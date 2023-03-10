#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

mod http_route;

use api::{
    destination,
    net::{ip_address::Ip, IPv6, IpAddress},
};
use futures::prelude::*;
use linkerd2_proxy_api::{
    self as api,
    inbound::{
        self as proto,
        inbound_server_policies_server::{InboundServerPolicies, InboundServerPoliciesServer},
    },
    meta::{metadata, Metadata},
    net::TcpAddress,
    outbound::{
        self,
        outbound_policies_server::{OutboundPolicies, OutboundPoliciesServer},
    },
};
use linkerd_policy_controller_core::{
    http_route::{
        Backend, InboundFilter, InboundHttpRoute, InboundHttpRouteRule, OutboundHttpRouteRule,
    },
    AuthorizationRef, ClientAuthentication, ClientAuthorization, DiscoverInboundServer,
    DiscoverOutboundPolicy, IdentityMatch, InboundHttpRouteRef, InboundServer, InboundServerStream,
    IpNet, NetworkMatch, OutboundHttpRoute, OutboundPolicy, OutboundPolicyStream, ProxyProtocol,
    ServerRef,
};
use maplit::*;
use std::{net::IpAddr, num::NonZeroU16, sync::Arc, time};
use tracing::trace;

#[derive(Clone, Debug)]
pub struct InboundPolicyServer<T> {
    discover: T,
    drain: drain::Watch,
    cluster_networks: Arc<[IpNet]>,
}

#[derive(Clone, Debug)]
pub struct OutboundPolicyServer<T> {
    index: T,
    drain: drain::Watch,
}

// === impl InboundPolicyServer ===

impl<T> InboundPolicyServer<T>
where
    T: DiscoverInboundServer<(String, String, NonZeroU16)> + Send + Sync + 'static,
{
    pub fn new(discover: T, cluster_networks: Vec<IpNet>, drain: drain::Watch) -> Self {
        Self {
            discover,
            drain,
            cluster_networks: cluster_networks.into(),
        }
    }

    pub fn svc(self) -> InboundServerPoliciesServer<Self> {
        InboundServerPoliciesServer::new(self)
    }

    fn check_target(
        &self,
        proto::PortSpec { workload, port }: proto::PortSpec,
    ) -> Result<(String, String, NonZeroU16), tonic::Status> {
        // Parse a workload name in the form namespace:name.
        let (ns, name) = match workload.split_once(':') {
            None => {
                return Err(tonic::Status::invalid_argument(format!(
                    "Invalid workload: {}",
                    workload
                )));
            }
            Some((ns, pod)) if ns.is_empty() || pod.is_empty() => {
                return Err(tonic::Status::invalid_argument(format!(
                    "Invalid workload: {}",
                    workload
                )));
            }
            Some((ns, pod)) => (ns, pod),
        };

        // Ensure that the port is in the valid range.
        let port = u16::try_from(port)
            .and_then(NonZeroU16::try_from)
            .map_err(|_| tonic::Status::invalid_argument(format!("Invalid port: {port}")))?;

        Ok((ns.to_string(), name.to_string(), port))
    }
}

#[async_trait::async_trait]
impl<T> InboundServerPolicies for InboundPolicyServer<T>
where
    T: DiscoverInboundServer<(String, String, NonZeroU16)> + Send + Sync + 'static,
{
    async fn get_port(
        &self,
        req: tonic::Request<proto::PortSpec>,
    ) -> Result<tonic::Response<proto::Server>, tonic::Status> {
        let target = self.check_target(req.into_inner())?;

        // Lookup the configuration for an inbound port. If the pod hasn't (yet)
        // been indexed, return a Not Found error.
        let s = self
            .discover
            .get_inbound_server(target)
            .await
            .map_err(|e| tonic::Status::internal(format!("lookup failed: {}", e)))?
            .ok_or_else(|| tonic::Status::not_found("unknown server"))?;

        Ok(tonic::Response::new(to_server(&s, &*self.cluster_networks)))
    }

    type WatchPortStream = BoxWatchStream;

    async fn watch_port(
        &self,
        req: tonic::Request<proto::PortSpec>,
    ) -> Result<tonic::Response<BoxWatchStream>, tonic::Status> {
        let target = self.check_target(req.into_inner())?;
        let drain = self.drain.clone();
        let rx = self
            .discover
            .watch_inbound_server(target)
            .await
            .map_err(|e| tonic::Status::internal(format!("lookup failed: {}", e)))?
            .ok_or_else(|| tonic::Status::not_found("unknown server"))?;
        Ok(tonic::Response::new(response_stream(
            drain,
            rx,
            self.cluster_networks.clone(),
        )))
    }
}

impl<T> OutboundPolicyServer<T>
where
    T: DiscoverOutboundPolicy<(String, String, NonZeroU16)> + Send + Sync + 'static,
{
    pub fn new(discover: T, drain: drain::Watch) -> Self {
        Self {
            index: discover,
            drain,
        }
    }

    pub fn svc(self) -> OutboundPoliciesServer<Self> {
        OutboundPoliciesServer::new(self)
    }

    fn service_lookup(
        &self,
        target: TcpAddress,
    ) -> Result<(String, String, NonZeroU16), tonic::Status> {
        let port = target
            .port
            .try_into()
            .map_err(|_| tonic::Status::invalid_argument("port outside valid range"))?;
        let port = NonZeroU16::new(port)
            .ok_or_else(|| tonic::Status::invalid_argument("port cannot be zero"))?;

        let addr = target
            .ip
            .ok_or_else(|| tonic::Status::invalid_argument("traffic target must have an IP"))?
            .try_into()
            .map_err(|error| {
                tonic::Status::invalid_argument(format!("failed to parse target addr: {}", error))
            })?;

        self.index
            .service_lookup(addr, port)
            .ok_or_else(|| tonic::Status::not_found("No such service"))
    }
}

#[async_trait::async_trait]
impl<T> OutboundPolicies for OutboundPolicyServer<T>
where
    T: DiscoverOutboundPolicy<(String, String, NonZeroU16)> + Send + Sync + 'static,
{
    async fn get(
        &self,
        req: tonic::Request<outbound::TrafficSpec>,
    ) -> Result<tonic::Response<outbound::OutboundPolicy>, tonic::Status> {
        let traffic_spec = req.into_inner();

        let target = traffic_spec
            .target
            .ok_or_else(|| tonic::Status::invalid_argument("target is required"))?;
        let target = match target {
            outbound::traffic_spec::Target::Addr(target) => target,
            outbound::traffic_spec::Target::Authority(_) => {
                return Err(tonic::Status::unimplemented(
                    "getting policy by authority is not supported",
                ))
            }
        };
        let service = self.service_lookup(target)?;

        let policy = self
            .index
            .get_outbound_policy(service)
            .await
            .map_err(|error| {
                tonic::Status::internal(format!("failed to get outbound policy: {}", error))
            })?;

        if let Some(policy) = policy {
            Ok(tonic::Response::new(to_service(policy)))
        } else {
            Err(tonic::Status::not_found("No such policy"))
        }
    }

    type WatchStream = BoxWatchServiceStream;

    async fn watch(
        &self,
        req: tonic::Request<outbound::TrafficSpec>,
    ) -> Result<tonic::Response<BoxWatchServiceStream>, tonic::Status> {
        let traffic_spec = req.into_inner();

        let target = traffic_spec
            .target
            .ok_or_else(|| tonic::Status::invalid_argument("target is required"))?;
        let target = match target {
            outbound::traffic_spec::Target::Addr(target) => target,
            outbound::traffic_spec::Target::Authority(_) => {
                return Err(tonic::Status::unimplemented(
                    "getting policy by authority is not supported",
                ))
            }
        };
        let service = self.service_lookup(target)?;
        let drain = self.drain.clone();

        let rx = self
            .index
            .watch_outbound_policy(service)
            .await
            .map_err(|e| tonic::Status::internal(format!("lookup failed: {}", e)))?
            .ok_or_else(|| tonic::Status::not_found("unknown server"))?;
        Ok(tonic::Response::new(outbound_policy_stream(drain, rx)))
    }
}

type BoxWatchStream =
    std::pin::Pin<Box<dyn Stream<Item = Result<proto::Server, tonic::Status>> + Send + Sync>>;
type BoxWatchServiceStream = std::pin::Pin<
    Box<dyn Stream<Item = Result<outbound::OutboundPolicy, tonic::Status>> + Send + Sync>,
>;

fn response_stream(
    drain: drain::Watch,
    mut rx: InboundServerStream,
    cluster_networks: Arc<[IpNet]>,
) -> BoxWatchStream {
    Box::pin(async_stream::try_stream! {
        tokio::pin! {
            let shutdown = drain.signaled();
        }

        loop {
            tokio::select! {
                // When the port is updated with a new server, update the server watch.
                res = rx.next() => match res {
                    Some(s) => {
                        yield to_server(&s, &*cluster_networks);
                    }
                    None => return,
                },

                // If the server starts shutting down, close the stream so that it doesn't hold the
                // server open.
                _ = (&mut shutdown) => {
                    return;
                }
            }
        }
    })
}

fn outbound_policy_stream(
    drain: drain::Watch,
    mut rx: OutboundPolicyStream,
) -> BoxWatchServiceStream {
    Box::pin(async_stream::try_stream! {
        tokio::pin! {
            let shutdown = drain.signaled();
        }

        loop {
            tokio::select! {
                // When the port is updated with a new server, update the server watch.
                res = rx.next() => match res {
                    Some(policy) => {
                        yield to_service(policy);
                    }
                    None => return,
                },

                // If the server starts shutting down, close the stream so that it doesn't hold the
                // server open.
                _ = (&mut shutdown) => {
                    return;
                }
            }
        }
    })
}

fn to_server(srv: &InboundServer, cluster_networks: &[IpNet]) -> proto::Server {
    // Convert the protocol object into a protobuf response.
    let protocol = proto::ProxyProtocol {
        kind: match srv.protocol {
            ProxyProtocol::Detect { timeout } => Some(proto::proxy_protocol::Kind::Detect(
                proto::proxy_protocol::Detect {
                    timeout: timeout.try_into().map_err(|error| tracing::warn!(%error, "failed to convert protocol detect timeout to protobuf")).ok(),
                    http_routes: to_http_route_list(&srv.http_routes, cluster_networks),
                },
            )),
            ProxyProtocol::Http1 => Some(proto::proxy_protocol::Kind::Http1(
                proto::proxy_protocol::Http1 {
                    routes: to_http_route_list(&srv.http_routes, cluster_networks),
                },
            )),
            ProxyProtocol::Http2 => Some(proto::proxy_protocol::Kind::Http2(
                proto::proxy_protocol::Http2 {
                    routes: to_http_route_list(&srv.http_routes, cluster_networks),
                },
            )),
            ProxyProtocol::Grpc => Some(proto::proxy_protocol::Kind::Grpc(
                proto::proxy_protocol::Grpc::default(),
            )),
            ProxyProtocol::Opaque => Some(proto::proxy_protocol::Kind::Opaque(
                proto::proxy_protocol::Opaque {},
            )),
            ProxyProtocol::Tls => Some(proto::proxy_protocol::Kind::Tls(
                proto::proxy_protocol::Tls {},
            )),
        },
    };
    trace!(?protocol);

    let authorizations = srv
        .authorizations
        .iter()
        .map(|(n, c)| to_authz(n, c, cluster_networks))
        .collect();
    trace!(?authorizations);

    let labels = match &srv.reference {
        ServerRef::Default(name) => convert_args!(hashmap!(
            "group" => "",
            "kind" => "default",
            "name" => *name,
        )),
        ServerRef::Server(name) => convert_args!(hashmap!(
            "group" => "policy.linkerd.io",
            "kind" => "server",
            "name" => name,
        )),
    };
    trace!(?labels);

    proto::Server {
        protocol: Some(protocol),
        authorizations,
        labels,
        ..Default::default()
    }
}

fn to_authz(
    reference: &AuthorizationRef,
    ClientAuthorization {
        networks,
        authentication,
    }: &ClientAuthorization,
    cluster_networks: &[IpNet],
) -> proto::Authz {
    let meta = Metadata {
        kind: Some(match reference {
            AuthorizationRef::Default(name) => metadata::Kind::Default(name.to_string()),
            AuthorizationRef::AuthorizationPolicy(name) => {
                metadata::Kind::Resource(api::meta::Resource {
                    group: "policy.linkerd.io".to_string(),
                    kind: "authorizationpolicy".to_string(),
                    name: name.to_string(),
                    ..Default::default()
                })
            }
            AuthorizationRef::ServerAuthorization(name) => {
                metadata::Kind::Resource(api::meta::Resource {
                    group: "policy.linkerd.io".to_string(),
                    kind: "serverauthorization".to_string(),
                    name: name.clone(),
                    ..Default::default()
                })
            }
        }),
    };

    // TODO labels are deprecated, but we want to continue to support them for older proxies. This
    // can be removed in 2.13.
    let labels = match reference {
        AuthorizationRef::Default(name) => convert_args!(hashmap!(
            "group" => "",
            "kind" => "default",
            "name" => *name,
        )),
        AuthorizationRef::ServerAuthorization(name) => convert_args!(hashmap!(
            "group" => "policy.linkerd.io",
            "kind" => "serverauthorization",
            "name" => name,
        )),
        AuthorizationRef::AuthorizationPolicy(name) => convert_args!(hashmap!(
            "group" => "policy.linkerd.io",
            "kind" => "authorizationpolicy",
            "name" => name,
        )),
    };

    let networks = if networks.is_empty() {
        cluster_networks
            .iter()
            .map(|n| proto::Network {
                net: Some((*n).into()),
                except: vec![],
            })
            .collect::<Vec<_>>()
    } else {
        networks
            .iter()
            .map(|NetworkMatch { net, except }| proto::Network {
                net: Some((*net).into()),
                except: except.iter().cloned().map(Into::into).collect(),
            })
            .collect()
    };

    let authn = match authentication {
        ClientAuthentication::Unauthenticated => proto::Authn {
            permit: Some(proto::authn::Permit::Unauthenticated(
                proto::authn::PermitUnauthenticated {},
            )),
        },

        ClientAuthentication::TlsUnauthenticated => proto::Authn {
            permit: Some(proto::authn::Permit::MeshTls(proto::authn::PermitMeshTls {
                clients: Some(proto::authn::permit_mesh_tls::Clients::Unauthenticated(
                    proto::authn::PermitUnauthenticated {},
                )),
            })),
        },

        // Authenticated connections must have TLS and apply to all
        // networks.
        ClientAuthentication::TlsAuthenticated(identities) => {
            let suffixes = identities
                .iter()
                .filter_map(|i| match i {
                    IdentityMatch::Suffix(s) => Some(proto::IdentitySuffix { parts: s.to_vec() }),
                    _ => None,
                })
                .collect();

            let identities = identities
                .iter()
                .filter_map(|i| match i {
                    IdentityMatch::Exact(n) => Some(proto::Identity {
                        name: n.to_string(),
                    }),
                    _ => None,
                })
                .collect();

            proto::Authn {
                permit: Some(proto::authn::Permit::MeshTls(proto::authn::PermitMeshTls {
                    clients: Some(proto::authn::permit_mesh_tls::Clients::Identities(
                        proto::authn::permit_mesh_tls::PermitClientIdentities {
                            identities,
                            suffixes,
                        },
                    )),
                })),
            }
        }
    };

    proto::Authz {
        metadata: Some(meta),
        labels,
        networks,
        authentication: Some(authn),
    }
}

fn to_http_route_list<'r>(
    routes: impl IntoIterator<Item = (&'r InboundHttpRouteRef, &'r InboundHttpRoute)>,
    cluster_networks: &[IpNet],
) -> Vec<proto::HttpRoute> {
    // Per the Gateway API spec:
    //
    // > If ties still exist across multiple Routes, matching precedence MUST be
    // > determined in order of the following criteria, continuing on ties:
    // >
    // >    The oldest Route based on creation timestamp.
    // >    The Route appearing first in alphabetical order by
    // >   "{namespace}/{name}".
    //
    // Note that we don't need to include the route's namespace in this
    // comparison, because all these routes will exist in the same
    // namespace.
    let mut route_list = routes.into_iter().collect::<Vec<_>>();
    route_list.sort_by(|(a_ref, a), (b_ref, b)| {
        let by_ts = match (&a.creation_timestamp, &b.creation_timestamp) {
            (Some(a_ts), Some(b_ts)) => a_ts.cmp(b_ts),
            (None, None) => std::cmp::Ordering::Equal,
            // Routes with timestamps are preferred over routes without.
            (Some(_), None) => return std::cmp::Ordering::Less,
            (None, Some(_)) => return std::cmp::Ordering::Greater,
        };
        by_ts.then_with(|| a_ref.cmp(b_ref))
    });

    route_list
        .into_iter()
        .map(|(route_ref, route)| to_http_route(route_ref, route.clone(), cluster_networks))
        .collect()
}

fn to_http_route(
    reference: &InboundHttpRouteRef,
    InboundHttpRoute {
        hostnames,
        rules,
        authorizations,
        creation_timestamp: _,
    }: InboundHttpRoute,
    cluster_networks: &[IpNet],
) -> proto::HttpRoute {
    let metadata = Metadata {
        kind: Some(match reference {
            InboundHttpRouteRef::Default(name) => metadata::Kind::Default(name.to_string()),
            InboundHttpRouteRef::Linkerd(name) => metadata::Kind::Resource(api::meta::Resource {
                group: "policy.linkerd.io".to_string(),
                kind: "HTTPRoute".to_string(),
                name: name.to_string(),
                ..Default::default()
            }),
        }),
    };

    let hosts = hostnames
        .into_iter()
        .map(http_route::convert_host_match)
        .collect();

    let rules = rules
        .into_iter()
        .map(
            |InboundHttpRouteRule { matches, filters }| proto::http_route::Rule {
                matches: matches.into_iter().map(http_route::convert_match).collect(),
                filters: filters.into_iter().map(convert_filter).collect(),
            },
        )
        .collect();

    let authorizations = authorizations
        .iter()
        .map(|(n, c)| to_authz(n, c, cluster_networks))
        .collect();

    proto::HttpRoute {
        metadata: Some(metadata),
        hosts,
        rules,
        authorizations,
    }
}

fn convert_filter(filter: InboundFilter) -> proto::http_route::Filter {
    use proto::http_route::filter::Kind;

    proto::http_route::Filter {
        kind: Some(match filter {
            InboundFilter::FailureInjector(f) => {
                Kind::FailureInjector(http_route::convert_failure_injector_filter(f))
            }
            InboundFilter::RequestHeaderModifier(f) => {
                Kind::RequestHeaderModifier(http_route::convert_header_modifier_filter(f))
            }
            InboundFilter::RequestRedirect(f) => {
                Kind::Redirect(http_route::convert_redirect_filter(f))
            }
        }),
    }
}

fn to_service(outbound: OutboundPolicy) -> outbound::OutboundPolicy {
    let backend = default_http_backend(&outbound);

    let kind = if outbound.opaque {
        linkerd2_proxy_api::outbound::proxy_protocol::Kind::Opaque(
            outbound::proxy_protocol::Opaque {
                routes: Default::default(),
            },
        )
    } else {
        let mut http_routes = outbound.http_routes.into_iter().collect::<Vec<_>>();
        http_routes.sort_by(|(a_name, a_route), (b_name, b_route)| {
            let by_ts = match (&a_route.creation_timestamp, &b_route.creation_timestamp) {
                (Some(a_ts), Some(b_ts)) => a_ts.cmp(b_ts),
                (None, None) => std::cmp::Ordering::Equal,
                // Routes with timestamps are preferred over routes without.
                (Some(_), None) => return std::cmp::Ordering::Less,
                (None, Some(_)) => return std::cmp::Ordering::Greater,
            };
            by_ts.then_with(|| a_name.cmp(b_name))
        });

        let mut http_routes: Vec<_> = http_routes
            .into_iter()
            .map(|(name, route)| {
                convert_outbound_http_route(
                    outbound.namespace.clone(),
                    name,
                    route,
                    backend.clone(),
                )
            })
            .collect();

        if http_routes.is_empty() {
            http_routes = vec![default_outbound_http_route(backend)];
        }

        linkerd2_proxy_api::outbound::proxy_protocol::Kind::Detect(
            outbound::proxy_protocol::Detect {
                timeout: Some(
                    time::Duration::from_secs(10)
                        .try_into()
                        .expect("failed to convert detect timeout to protobuf"),
                ),
                opaque: Some(outbound::proxy_protocol::Opaque {
                    routes: Default::default(),
                }),
                http1: Some(outbound::proxy_protocol::Http1 {
                    routes: http_routes.clone(),
                }),
                http2: Some(outbound::proxy_protocol::Http2 {
                    routes: http_routes,
                }),
            },
        )
    };

    outbound::OutboundPolicy {
        protocol: Some(outbound::ProxyProtocol { kind: Some(kind) }),
    }
}

fn convert_outbound_http_route(
    namespace: String,
    name: String,
    OutboundHttpRoute {
        hostnames,
        rules,
        creation_timestamp: _,
    }: OutboundHttpRoute,
    default_backend: outbound::http_route::WeightedRouteBackend,
) -> outbound::HttpRoute {
    let metadata = Some(Metadata {
        kind: Some(metadata::Kind::Resource(api::meta::Resource {
            group: "policy.linkerd.io".to_string(),
            kind: "HTTPRoute".to_string(),
            namespace,
            name,
            ..Default::default()
        })),
    });

    let hosts = hostnames
        .into_iter()
        .map(http_route::convert_host_match)
        .collect();

    let rules = rules
        .into_iter()
        .map(|OutboundHttpRouteRule { matches, backends }| {
            let mut backends = backends
                .into_iter()
                .map(convert_http_backend)
                .collect::<Vec<_>>();
            if backends.is_empty() {
                backends = vec![default_backend.clone()];
            }
            outbound::http_route::Rule {
                matches: matches.into_iter().map(http_route::convert_match).collect(),
                backends: Some(outbound::http_route::Distribution {
                    kind: Some(outbound::http_route::distribution::Kind::RandomAvailable(
                        outbound::http_route::distribution::RandomAvailable { backends },
                    )),
                }),
                filters: Default::default(),
            }
        })
        .collect();

    outbound::HttpRoute {
        metadata,
        hosts,
        rules,
    }
}

fn convert_http_backend(backend: Backend) -> outbound::http_route::WeightedRouteBackend {
    match backend {
        Backend::Addr(addr) => outbound::http_route::WeightedRouteBackend {
            weight: addr.weight,
            backend: Some(outbound::http_route::RouteBackend {
                backend: Some(outbound::Backend {
                    metadata: None,
                    queue: Some(default_queue_config()),
                    kind: Some(outbound::backend::Kind::Forward(
                        destination::WeightedAddr {
                            addr: Some(convert_tcp_address(addr.addr, addr.port)),
                            weight: addr.weight,
                            ..Default::default()
                        },
                    )),
                }),
                filters: Default::default(),
            }),
        },
        Backend::Dst(dst) => outbound::http_route::WeightedRouteBackend {
            weight: dst.weight,
            backend: Some(outbound::http_route::RouteBackend {
                backend: Some(outbound::Backend {
                    metadata: None,
                    queue: Some(default_queue_config()),
                    kind: Some(outbound::backend::Kind::Balancer(
                        outbound::backend::BalanceP2c {
                            discovery: Some(outbound::backend::EndpointDiscovery {
                                kind: Some(outbound::backend::endpoint_discovery::Kind::Dst(
                                    outbound::backend::endpoint_discovery::DestinationGet {
                                        path: dst.authority,
                                    },
                                )),
                            }),
                            load: Some(default_balancer_config()),
                        },
                    )),
                }),
                filters: Default::default(),
            }),
        },
        Backend::InvalidDst { weight, message } => outbound::http_route::WeightedRouteBackend {
            weight,
            backend: Some(outbound::http_route::RouteBackend {
                backend: None,
                filters: vec![outbound::http_route::Filter {
                    kind: Some(outbound::http_route::filter::Kind::FailureInjector(
                        api::http_route::HttpFailureInjector {
                            status: 500,
                            message,
                            ratio: None,
                        },
                    )),
                }],
            }),
        },
    }
}

fn default_http_backend(outbound: &OutboundPolicy) -> outbound::http_route::WeightedRouteBackend {
    outbound::http_route::WeightedRouteBackend {
        weight: 1,
        backend: Some(outbound::http_route::RouteBackend {
            backend: Some(outbound::Backend {
                metadata: Some(Metadata {
                    kind: Some(metadata::Kind::Default("default".to_string())),
                }),
                queue: Some(default_queue_config()),
                kind: Some(outbound::backend::Kind::Balancer(
                    outbound::backend::BalanceP2c {
                        discovery: Some(outbound::backend::EndpointDiscovery {
                            kind: Some(outbound::backend::endpoint_discovery::Kind::Dst(
                                outbound::backend::endpoint_discovery::DestinationGet {
                                    path: outbound.authority.clone(),
                                },
                            )),
                        }),
                        load: Some(default_balancer_config()),
                    },
                )),
            }),
            filters: Default::default(),
        }),
    }
}

fn default_outbound_http_route(
    backend: outbound::http_route::WeightedRouteBackend,
) -> outbound::HttpRoute {
    let metadata = Some(Metadata {
        kind: Some(metadata::Kind::Default("default".to_string())),
    });

    let rules = vec![outbound::http_route::Rule {
        matches: vec![api::http_route::HttpRouteMatch {
            path: Some(api::http_route::PathMatch {
                kind: Some(api::http_route::path_match::Kind::Prefix("/".to_string())),
            }),
            ..Default::default()
        }],
        backends: Some(outbound::http_route::Distribution {
            kind: Some(outbound::http_route::distribution::Kind::RandomAvailable(
                outbound::http_route::distribution::RandomAvailable {
                    backends: vec![backend],
                },
            )),
        }),
        filters: Default::default(),
    }];
    outbound::HttpRoute {
        metadata,
        rules,
        ..Default::default()
    }
}

fn default_balancer_config() -> outbound::backend::balance_p2c::Load {
    outbound::backend::balance_p2c::Load::PeakEwma(outbound::backend::balance_p2c::PeakEwma {
        default_rtt: Some(
            time::Duration::from_millis(30)
                .try_into()
                .expect("failed to convert ewma default_rtt to protobuf"),
        ),
        decay: Some(
            time::Duration::from_secs(10)
                .try_into()
                .expect("failed to convert ewma decay to protobuf"),
        ),
    })
}

fn default_queue_config() -> outbound::Queue {
    outbound::Queue {
        capacity: 100,
        failfast_timeout: Some(
            time::Duration::from_secs(3)
                .try_into()
                .expect("failed to convert failfast_timeout to protobuf"),
        ),
    }
}

// TODO(ver) this conversion should be made in the api crate.
fn convert_tcp_address(ip_addr: IpAddr, port: NonZeroU16) -> TcpAddress {
    let ip = match ip_addr {
        IpAddr::V4(ipv4) => Ip::Ipv4(ipv4.into()),
        IpAddr::V6(ipv6) => {
            let first = [
                ipv6.octets()[0],
                ipv6.octets()[1],
                ipv6.octets()[2],
                ipv6.octets()[3],
                ipv6.octets()[4],
                ipv6.octets()[5],
                ipv6.octets()[6],
                ipv6.octets()[7],
            ];
            let last = [
                ipv6.octets()[8],
                ipv6.octets()[9],
                ipv6.octets()[10],
                ipv6.octets()[11],
                ipv6.octets()[12],
                ipv6.octets()[13],
                ipv6.octets()[14],
                ipv6.octets()[15],
            ];
            Ip::Ipv6(IPv6 {
                first: u64::from_be_bytes(first),
                last: u64::from_be_bytes(last),
            })
        }
    };
    TcpAddress {
        ip: Some(IpAddress { ip: Some(ip) }),
        port: port.get().into(),
    }
}
