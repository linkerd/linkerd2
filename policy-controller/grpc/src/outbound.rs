use crate::{http_route, workload};
use futures::prelude::*;
use linkerd2_proxy_api::{
    self as api, destination,
    meta::{metadata, Metadata},
    outbound::{
        self,
        outbound_policies_server::{OutboundPolicies, OutboundPoliciesServer},
    },
};
use linkerd_policy_controller_core::{
    http_route::GroupKindNamespaceName,
    outbound::{
        Backend, DiscoverOutboundPolicy, Filter, HttpRoute, HttpRouteRule, OutboundDiscoverTarget,
        OutboundPolicy, OutboundPolicyStream,
    },
};
use std::{net::SocketAddr, num::NonZeroU16, str::FromStr, sync::Arc, time};

#[derive(Clone, Debug)]
pub struct OutboundPolicyServer<T> {
    index: T,
    // Used to parse named addresses into <svc>.<ns>.svc.<cluster-domain>.
    cluster_domain: Arc<str>,
    drain: drain::Watch,
}

impl<T> OutboundPolicyServer<T>
where
    T: DiscoverOutboundPolicy<OutboundDiscoverTarget> + Send + Sync + 'static,
{
    pub fn new(discover: T, cluster_domain: impl Into<Arc<str>>, drain: drain::Watch) -> Self {
        Self {
            index: discover,
            cluster_domain: cluster_domain.into(),
            drain,
        }
    }

    pub fn svc(self) -> OutboundPoliciesServer<Self> {
        OutboundPoliciesServer::new(self)
    }

    fn lookup(&self, spec: outbound::TrafficSpec) -> Result<OutboundDiscoverTarget, tonic::Status> {
        let target = spec
            .target
            .ok_or_else(|| tonic::Status::invalid_argument("target is required"))?;
        let source_namespace = workload::Workload::from_str(&spec.source_workload)?.namespace;
        let target = match target {
            outbound::traffic_spec::Target::Addr(target) => target,
            outbound::traffic_spec::Target::Authority(auth) => {
                return self.lookup_authority(&auth).map(
                    |(service_namespace, service_name, service_port)| OutboundDiscoverTarget {
                        service_name,
                        service_namespace,
                        service_port,
                        source_namespace,
                    },
                )
            }
        };

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
                tonic::Status::invalid_argument(format!("failed to parse target addr: {error}"))
            })?;

        self.index
            .lookup_ip(addr, port, source_namespace)
            .ok_or_else(|| tonic::Status::not_found("No such service"))
    }

    fn lookup_authority(
        &self,
        authority: &str,
    ) -> Result<(String, String, NonZeroU16), tonic::Status> {
        let auth = authority
            .parse::<http::uri::Authority>()
            .map_err(|_| tonic::Status::invalid_argument("invalid authority"))?;

        let mut host = auth.host();
        if host.is_empty() {
            return Err(tonic::Status::invalid_argument(
                "authority must have a host",
            ));
        }

        host = host
            .trim_end_matches('.')
            .trim_end_matches(&*self.cluster_domain);

        let mut parts = host.split('.');
        let invalid = {
            let domain = &self.cluster_domain;
            move || {
                tonic::Status::not_found(format!(
                    "authority must be of the form <name>.<namespace>.svc.{domain}",
                ))
            }
        };
        let name = parts.next().ok_or_else(invalid)?;
        let namespace = parts.next().ok_or_else(invalid)?;
        if parts.next() != Some("svc") {
            return Err(invalid());
        };

        let port = auth
            .port_u16()
            .and_then(|p| NonZeroU16::try_from(p).ok())
            .unwrap_or_else(|| 80.try_into().unwrap());

        Ok((namespace.to_string(), name.to_string(), port))
    }
}

#[async_trait::async_trait]
impl<T> OutboundPolicies for OutboundPolicyServer<T>
where
    T: DiscoverOutboundPolicy<OutboundDiscoverTarget> + Send + Sync + 'static,
{
    async fn get(
        &self,
        req: tonic::Request<outbound::TrafficSpec>,
    ) -> Result<tonic::Response<outbound::OutboundPolicy>, tonic::Status> {
        let service = self.lookup(req.into_inner())?;

        let policy = self
            .index
            .get_outbound_policy(service)
            .await
            .map_err(|error| {
                tonic::Status::internal(format!("failed to get outbound policy: {error}"))
            })?;

        if let Some(policy) = policy {
            Ok(tonic::Response::new(to_service(policy)))
        } else {
            Err(tonic::Status::not_found("No such policy"))
        }
    }

    type WatchStream = BoxWatchStream;

    async fn watch(
        &self,
        req: tonic::Request<outbound::TrafficSpec>,
    ) -> Result<tonic::Response<BoxWatchStream>, tonic::Status> {
        let service = self.lookup(req.into_inner())?;
        let drain = self.drain.clone();

        let rx = self
            .index
            .watch_outbound_policy(service)
            .await
            .map_err(|e| tonic::Status::internal(format!("lookup failed: {e}")))?
            .ok_or_else(|| tonic::Status::not_found("unknown server"))?;
        Ok(tonic::Response::new(response_stream(drain, rx)))
    }
}

type BoxWatchStream = std::pin::Pin<
    Box<dyn Stream<Item = Result<outbound::OutboundPolicy, tonic::Status>> + Send + Sync>,
>;

fn response_stream(drain: drain::Watch, mut rx: OutboundPolicyStream) -> BoxWatchStream {
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

fn to_service(outbound: OutboundPolicy) -> outbound::OutboundPolicy {
    let backend = default_backend(&outbound);

    let kind = if outbound.opaque {
        linkerd2_proxy_api::outbound::proxy_protocol::Kind::Opaque(
            outbound::proxy_protocol::Opaque {
                routes: vec![default_outbound_opaq_route(backend)],
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
            by_ts.then_with(|| a_name.name.cmp(&b_name.name))
        });

        let mut http_routes: Vec<_> = http_routes
            .into_iter()
            .map(|(gknn, route)| convert_outbound_http_route(gknn, route, backend.clone()))
            .collect();

        if http_routes.is_empty() {
            http_routes = vec![default_outbound_http_route(backend.clone())];
        }

        let accrual =
            outbound
                .accrual
                .map(|accrual| linkerd2_proxy_api::outbound::FailureAccrual {
                    kind: Some(match accrual {
                        linkerd_policy_controller_core::outbound::FailureAccrual::Consecutive {
                            max_failures,
                            backoff,
                        } => outbound::failure_accrual::Kind::ConsecutiveFailures(
                            outbound::failure_accrual::ConsecutiveFailures {
                                max_failures,
                                backoff: Some(outbound::ExponentialBackoff {
                                    min_backoff: convert_duration(
                                        "min_backoff",
                                        backoff.min_penalty,
                                    ),
                                    max_backoff: convert_duration(
                                        "max_backoff",
                                        backoff.max_penalty,
                                    ),
                                    jitter_ratio: backoff.jitter,
                                }),
                            },
                        ),
                    }),
                });

        linkerd2_proxy_api::outbound::proxy_protocol::Kind::Detect(
            outbound::proxy_protocol::Detect {
                timeout: Some(
                    time::Duration::from_secs(10)
                        .try_into()
                        .expect("failed to convert detect timeout to protobuf"),
                ),
                opaque: Some(outbound::proxy_protocol::Opaque {
                    routes: vec![default_outbound_opaq_route(backend)],
                }),
                http1: Some(outbound::proxy_protocol::Http1 {
                    routes: http_routes.clone(),
                    failure_accrual: accrual.clone(),
                }),
                http2: Some(outbound::proxy_protocol::Http2 {
                    routes: http_routes,
                    failure_accrual: accrual,
                }),
            },
        )
    };

    let metadata = Metadata {
        kind: Some(metadata::Kind::Resource(api::meta::Resource {
            group: "core".to_string(),
            kind: "Service".to_string(),
            namespace: outbound.namespace,
            name: outbound.name,
            port: u16::from(outbound.port).into(),
            ..Default::default()
        })),
    };

    outbound::OutboundPolicy {
        metadata: Some(metadata),
        protocol: Some(outbound::ProxyProtocol { kind: Some(kind) }),
    }
}

fn convert_outbound_http_route(
    gknn: GroupKindNamespaceName,
    HttpRoute {
        hostnames,
        rules,
        creation_timestamp: _,
    }: HttpRoute,
    backend: outbound::Backend,
) -> outbound::HttpRoute {
    let metadata = Some(Metadata {
        kind: Some(metadata::Kind::Resource(api::meta::Resource {
            group: gknn.group.to_string(),
            kind: gknn.kind.to_string(),
            namespace: gknn.namespace.to_string(),
            name: gknn.name.to_string(),
            ..Default::default()
        })),
    });

    let hosts = hostnames
        .into_iter()
        .map(http_route::convert_host_match)
        .collect();

    let rules = rules
        .into_iter()
        .map(
            |HttpRouteRule {
                 matches,
                 backends,
                 request_timeout,
                 backend_request_timeout,
                 filters,
             }| {
                let backend_request_timeout = backend_request_timeout
                    .and_then(|d| convert_duration("backend request_timeout", d));
                let backends = backends
                    .into_iter()
                    .map(|backend| convert_http_backend(backend_request_timeout.clone(), backend))
                    .collect::<Vec<_>>();
                let dist = if backends.is_empty() {
                    outbound::http_route::distribution::Kind::FirstAvailable(
                        outbound::http_route::distribution::FirstAvailable {
                            backends: vec![outbound::http_route::RouteBackend {
                                backend: Some(backend.clone()),
                                filters: vec![],
                                request_timeout: backend_request_timeout,
                            }],
                        },
                    )
                } else {
                    outbound::http_route::distribution::Kind::RandomAvailable(
                        outbound::http_route::distribution::RandomAvailable { backends },
                    )
                };
                outbound::http_route::Rule {
                    matches: matches.into_iter().map(http_route::convert_match).collect(),
                    backends: Some(outbound::http_route::Distribution { kind: Some(dist) }),
                    filters: filters.into_iter().map(convert_filter).collect(),
                    request_timeout: request_timeout
                        .and_then(|d| convert_duration("request timeout", d)),
                }
            },
        )
        .collect();

    outbound::HttpRoute {
        metadata,
        hosts,
        rules,
    }
}

fn convert_http_backend(
    request_timeout: Option<prost_types::Duration>,
    backend: Backend,
) -> outbound::http_route::WeightedRouteBackend {
    match backend {
        Backend::Addr(addr) => {
            let socket_addr = SocketAddr::new(addr.addr, addr.port.get());
            outbound::http_route::WeightedRouteBackend {
                weight: addr.weight,
                backend: Some(outbound::http_route::RouteBackend {
                    backend: Some(outbound::Backend {
                        metadata: None,
                        queue: Some(default_queue_config()),
                        kind: Some(outbound::backend::Kind::Forward(
                            destination::WeightedAddr {
                                addr: Some(socket_addr.into()),
                                weight: addr.weight,
                                ..Default::default()
                            },
                        )),
                    }),
                    filters: Default::default(),
                    request_timeout,
                }),
            }
        }
        Backend::Service(svc) => {
            let filters = svc.filters.into_iter().map(convert_filter).collect();
            outbound::http_route::WeightedRouteBackend {
                weight: svc.weight,
                backend: Some(outbound::http_route::RouteBackend {
                    backend: Some(outbound::Backend {
                        metadata: Some(Metadata {
                            kind: Some(metadata::Kind::Resource(api::meta::Resource {
                                group: "core".to_string(),
                                kind: "Service".to_string(),
                                name: svc.name,
                                namespace: svc.namespace,
                                section: Default::default(),
                                port: u16::from(svc.port).into(),
                            })),
                        }),
                        queue: Some(default_queue_config()),
                        kind: Some(outbound::backend::Kind::Balancer(
                            outbound::backend::BalanceP2c {
                                discovery: Some(outbound::backend::EndpointDiscovery {
                                    kind: Some(outbound::backend::endpoint_discovery::Kind::Dst(
                                        outbound::backend::endpoint_discovery::DestinationGet {
                                            path: svc.authority,
                                        },
                                    )),
                                }),
                                load: Some(default_balancer_config()),
                            },
                        )),
                    }),
                    filters,
                    request_timeout,
                }),
            }
        }
        Backend::Invalid { weight, message } => outbound::http_route::WeightedRouteBackend {
            weight,
            backend: Some(outbound::http_route::RouteBackend {
                backend: Some(outbound::Backend {
                    metadata: Some(Metadata {
                        kind: Some(metadata::Kind::Default("invalid".to_string())),
                    }),
                    queue: Some(default_queue_config()),
                    kind: None,
                }),
                filters: vec![outbound::http_route::Filter {
                    kind: Some(outbound::http_route::filter::Kind::FailureInjector(
                        api::http_route::HttpFailureInjector {
                            status: 500,
                            message,
                            ratio: None,
                        },
                    )),
                }],
                request_timeout,
            }),
        },
    }
}

fn default_backend(outbound: &OutboundPolicy) -> outbound::Backend {
    outbound::Backend {
        metadata: Some(Metadata {
            kind: Some(metadata::Kind::Resource(api::meta::Resource {
                group: "core".to_string(),
                kind: "Service".to_string(),
                name: outbound.name.clone(),
                namespace: outbound.namespace.clone(),
                section: Default::default(),
                port: u16::from(outbound.port).into(),
            })),
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
    }
}

fn default_outbound_http_route(backend: outbound::Backend) -> outbound::HttpRoute {
    let metadata = Some(Metadata {
        kind: Some(metadata::Kind::Default("http".to_string())),
    });
    let rules = vec![outbound::http_route::Rule {
        matches: vec![api::http_route::HttpRouteMatch {
            path: Some(api::http_route::PathMatch {
                kind: Some(api::http_route::path_match::Kind::Prefix("/".to_string())),
            }),
            ..Default::default()
        }],
        backends: Some(outbound::http_route::Distribution {
            kind: Some(outbound::http_route::distribution::Kind::FirstAvailable(
                outbound::http_route::distribution::FirstAvailable {
                    backends: vec![outbound::http_route::RouteBackend {
                        backend: Some(backend),
                        filters: vec![],
                        request_timeout: None,
                    }],
                },
            )),
        }),
        filters: Default::default(),
        request_timeout: None,
    }];
    outbound::HttpRoute {
        metadata,
        rules,
        ..Default::default()
    }
}

fn default_outbound_opaq_route(backend: outbound::Backend) -> outbound::OpaqueRoute {
    let metadata = Some(Metadata {
        kind: Some(metadata::Kind::Default("opaq".to_string())),
    });
    let rules = vec![outbound::opaque_route::Rule {
        backends: Some(outbound::opaque_route::Distribution {
            kind: Some(outbound::opaque_route::distribution::Kind::FirstAvailable(
                outbound::opaque_route::distribution::FirstAvailable {
                    backends: vec![outbound::opaque_route::RouteBackend {
                        backend: Some(backend),
                    }],
                },
            )),
        }),
    }];
    outbound::OpaqueRoute { metadata, rules }
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

fn convert_duration(name: &'static str, duration: time::Duration) -> Option<prost_types::Duration> {
    duration
        .try_into()
        .map_err(|error| {
            tracing::error!(%error, "Invalid {name} duration");
        })
        .ok()
}

fn convert_filter(filter: Filter) -> outbound::http_route::Filter {
    use outbound::http_route::filter::Kind;

    outbound::http_route::Filter {
        kind: Some(match filter {
            Filter::RequestHeaderModifier(f) => {
                Kind::RequestHeaderModifier(http_route::convert_request_header_modifier_filter(f))
            }
            Filter::ResponseHeaderModifier(f) => {
                Kind::ResponseHeaderModifier(http_route::convert_response_header_modifier_filter(f))
            }
            Filter::RequestRedirect(f) => Kind::Redirect(http_route::convert_redirect_filter(f)),
        }),
    }
}
