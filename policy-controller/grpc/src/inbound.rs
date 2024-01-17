use crate::{http_route, workload::Workload};
use futures::prelude::*;
use linkerd2_proxy_api::{
    self as api,
    inbound::{
        self as proto,
        inbound_server_policies_server::{InboundServerPolicies, InboundServerPoliciesServer},
    },
    meta::{metadata, Metadata},
};
use linkerd_policy_controller_core::{
    inbound::{
        AuthorizationRef, ClientAuthentication, ClientAuthorization, DiscoverInboundServer, Filter,
        HttpRoute, HttpRouteRef, HttpRouteRule, InboundServer, InboundServerStream, ProxyProtocol,
        ServerRef,
    },
    IdentityMatch, IpNet, NetworkMatch,
};
use maplit::*;
use std::{num::NonZeroU16, str::FromStr, sync::Arc};
use tracing::trace;

#[derive(Clone, Debug)]
pub struct InboundPolicyServer<T> {
    discover: T,
    drain: drain::Watch,
    cluster_networks: Arc<[IpNet]>,
}

// === impl InboundPolicyServer ===

impl<T> InboundPolicyServer<T>
where
    T: DiscoverInboundServer<(Workload, NonZeroU16)> + Send + Sync + 'static,
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
    ) -> Result<(Workload, NonZeroU16), tonic::Status> {
        let workload = Workload::from_str(&workload)?;
        // Ensure that the port is in the valid range.
        let port = u16::try_from(port)
            .and_then(NonZeroU16::try_from)
            .map_err(|_| tonic::Status::invalid_argument(format!("Invalid port: {port}")))?;

        Ok((workload, port))
    }
}

#[async_trait::async_trait]
impl<T> InboundServerPolicies for InboundPolicyServer<T>
where
    T: DiscoverInboundServer<(Workload, NonZeroU16)> + Send + Sync + 'static,
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

        Ok(tonic::Response::new(to_server(&s, &self.cluster_networks)))
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

type BoxWatchStream =
    std::pin::Pin<Box<dyn Stream<Item = Result<proto::Server, tonic::Status>> + Send + Sync>>;

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
                        yield to_server(&s, &cluster_networks);
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
    routes: impl IntoIterator<Item = (&'r HttpRouteRef, &'r HttpRoute)>,
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
    reference: &HttpRouteRef,
    HttpRoute {
        hostnames,
        rules,
        authorizations,
        creation_timestamp: _,
    }: HttpRoute,
    cluster_networks: &[IpNet],
) -> proto::HttpRoute {
    let metadata = Metadata {
        kind: Some(match reference {
            HttpRouteRef::Default(name) => metadata::Kind::Default(name.to_string()),
            HttpRouteRef::Linkerd(gkn) => metadata::Kind::Resource(api::meta::Resource {
                group: gkn.group.to_string(),
                kind: gkn.kind.to_string(),
                name: gkn.name.to_string(),
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
            |HttpRouteRule { matches, filters }| proto::http_route::Rule {
                matches: matches.into_iter().map(http_route::convert_match).collect(),
                filters: filters.into_iter().filter_map(convert_filter).collect(),
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

fn convert_filter(filter: Filter) -> Option<proto::http_route::Filter> {
    use proto::http_route::filter::Kind;

    let kind = match filter {
        Filter::FailureInjector(f) => Some(Kind::FailureInjector(
            http_route::convert_failure_injector_filter(f),
        )),
        Filter::RequestHeaderModifier(f) => Some(Kind::RequestHeaderModifier(
            http_route::convert_request_header_modifier_filter(f),
        )),
        Filter::ResponseHeaderModifier(_) => None,
        Filter::RequestRedirect(f) => Some(Kind::Redirect(http_route::convert_redirect_filter(f))),
    };

    kind.map(|kind| proto::http_route::Filter { kind: Some(kind) })
}
