use crate::{routes, workload};
use futures::prelude::*;
use itertools::Itertools;
use linkerd2_proxy_api::{
    self as api, destination,
    meta::{metadata, Metadata},
    outbound::{
        self,
        outbound_policies_server::{OutboundPolicies, OutboundPoliciesServer},
    },
};
use linkerd_policy_controller_core::{
    outbound::{
        Backend, DiscoverOutboundPolicy, Filter, HttpRetryConditions, OutboundDiscoverTarget,
        OutboundPolicy, OutboundPolicyStream, OutboundRoute, OutboundRouteCollection,
        OutboundRouteRule,
    },
    routes::{GroupKindNamespaceName, HttpRouteMatch},
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
                _ = &mut shutdown => {
                    return;
                }
            }
        }
    })
}

fn to_service(outbound: OutboundPolicy) -> outbound::OutboundPolicy {
    let backend = default_backend(&outbound);

    let kind = if outbound.opaque {
        outbound::proxy_protocol::Kind::Opaque(outbound::proxy_protocol::Opaque {
            routes: vec![default_outbound_opaq_route(backend)],
        })
    } else {
        let accrual = outbound.accrual.map(|accrual| outbound::FailureAccrual {
            kind: Some(match accrual {
                linkerd_policy_controller_core::outbound::FailureAccrual::Consecutive {
                    max_failures,
                    backoff,
                } => outbound::failure_accrual::Kind::ConsecutiveFailures(
                    outbound::failure_accrual::ConsecutiveFailures {
                        max_failures,
                        backoff: Some(outbound::ExponentialBackoff {
                            min_backoff: convert_duration("min_backoff", backoff.min_penalty),
                            max_backoff: convert_duration("max_backoff", backoff.max_penalty),
                            jitter_ratio: backoff.jitter,
                        }),
                    },
                ),
            }),
        });

        match outbound.routes {
            OutboundRouteCollection::Empty => {
                let routes = vec![default_outbound_http_route(backend.clone())];

                outbound::proxy_protocol::Kind::Detect(outbound::proxy_protocol::Detect {
                    timeout: Some(
                        time::Duration::from_secs(10)
                            .try_into()
                            .expect("failed to convert detect timeout to protobuf"),
                    ),
                    opaque: Some(outbound::proxy_protocol::Opaque {
                        routes: vec![default_outbound_opaq_route(backend)],
                    }),
                    http1: Some(outbound::proxy_protocol::Http1 {
                        routes: routes.clone(),
                        failure_accrual: accrual.clone(),
                    }),
                    http2: Some(outbound::proxy_protocol::Http2 {
                        routes,
                        failure_accrual: accrual,
                    }),
                })
            }
            OutboundRouteCollection::Http(routes) => {
                let routes = routes
                    .into_iter()
                    .sorted_by(timestamp_then_name)
                    .map(|(gknn, route)| convert_outbound_http_route(gknn, route, backend.clone()))
                    .collect::<Vec<_>>();

                outbound::proxy_protocol::Kind::Detect(outbound::proxy_protocol::Detect {
                    timeout: Some(
                        time::Duration::from_secs(10)
                            .try_into()
                            .expect("failed to convert detect timeout to protobuf"),
                    ),
                    opaque: Some(outbound::proxy_protocol::Opaque {
                        routes: vec![default_outbound_opaq_route(backend)],
                    }),
                    http1: Some(outbound::proxy_protocol::Http1 {
                        routes: routes.clone(),
                        failure_accrual: accrual.clone(),
                    }),
                    http2: Some(outbound::proxy_protocol::Http2 {
                        routes,
                        failure_accrual: accrual,
                    }),
                })
            }
        }
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

fn timestamp_then_name<MatchType>(
    (left_id, left_route): &(GroupKindNamespaceName, OutboundRoute<MatchType>),
    (right_id, right_route): &(GroupKindNamespaceName, OutboundRoute<MatchType>),
) -> std::cmp::Ordering {
    let by_ts = match (
        &left_route.creation_timestamp,
        &right_route.creation_timestamp,
    ) {
        (Some(left_ts), Some(right_ts)) => left_ts.cmp(right_ts),
        (None, None) => std::cmp::Ordering::Equal,
        // Routes with timestamps are preferred over routes without.
        (Some(_), None) => return std::cmp::Ordering::Less,
        (None, Some(_)) => return std::cmp::Ordering::Greater,
    };

    by_ts.then_with(|| left_id.name.cmp(&right_id.name))
}

fn convert_outbound_http_route(
    gknn: GroupKindNamespaceName,
    OutboundRoute {
        hostnames,
        rules,
        creation_timestamp: _,
    }: OutboundRoute<HttpRouteMatch>,
    backend: outbound::Backend,
) -> outbound::HttpRoute {
    // This encoder sets deprecated timeouts for older proxies.
    #![allow(deprecated)]

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
        .map(routes::convert_host_match)
        .collect();

    let rules = rules
        .into_iter()
        .map(
            |OutboundRouteRule {
                 matches,
                 backends,
                 retry,
                 timeouts,
                 filters,
             }| {
                let backends = backends
                    .into_iter()
                    .map(convert_http_backend)
                    .collect::<Vec<_>>();
                let dist = if backends.is_empty() {
                    outbound::http_route::distribution::Kind::FirstAvailable(
                        outbound::http_route::distribution::FirstAvailable {
                            backends: vec![outbound::http_route::RouteBackend {
                                backend: Some(backend.clone()),
                                filters: vec![],
                                ..Default::default()
                            }],
                        },
                    )
                } else {
                    outbound::http_route::distribution::Kind::RandomAvailable(
                        outbound::http_route::distribution::RandomAvailable { backends },
                    )
                };
                outbound::http_route::Rule {
                    matches: matches
                        .into_iter()
                        .map(routes::http::convert_match)
                        .collect(),
                    backends: Some(outbound::http_route::Distribution { kind: Some(dist) }),
                    filters: filters.into_iter().map(convert_to_http_filter).collect(),
                    request_timeout: timeouts
                        .request
                        .and_then(|d| convert_duration("request timeout", d)),
                    timeouts: Some(api::http_route::Timeouts {
                        stream: timeouts
                            .request
                            .and_then(|d| convert_duration("stream timeout", d)),
                        idle: timeouts
                            .idle
                            .and_then(|d| convert_duration("idle timeout", d)),
                        response: timeouts
                            .response
                            .and_then(|d| convert_duration("response timeout", d)),
                    }),
                    retry: retry.map(|r| outbound::http_route::Retry {
                        max_retries: r.limit.into(),
                        max_request_bytes: 64 * 1024,
                        backoff: Some(outbound::ExponentialBackoff {
                            min_backoff: Some(time::Duration::from_millis(25).try_into().unwrap()),
                            max_backoff: Some(time::Duration::from_millis(250).try_into().unwrap()),
                            jitter_ratio: 1.0,
                        }),
                        conditions: r
                            .conditions
                            .map(|c| outbound::http_route::retry::Conditions {
                                status_ranges: match c {
                                    HttpRetryConditions::ServerError => {
                                        vec![outbound::http_route::retry::conditions::StatusRange {
                                            start: 500,
                                            end: 599,
                                        }]
                                    }
                                    HttpRetryConditions::GatewayError => {
                                        vec![outbound::http_route::retry::conditions::StatusRange {
                                            start: 502,
                                            end: 504,
                                        }]
                                    }
                                },
                            }),
                        timeout: r.timeout.and_then(|d| convert_duration("retry timeout", d)),
                    }),
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

fn convert_http_backend(backend: Backend) -> outbound::http_route::WeightedRouteBackend {
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
                    ..Default::default()
                }),
            }
        }
        Backend::Service(svc) => {
            if svc.exists {
                let filters = svc
                    .filters
                    .into_iter()
                    .map(convert_to_http_filter)
                    .collect();
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
                        ..Default::default()
                    }),
                }
            } else {
                outbound::http_route::WeightedRouteBackend {
                    weight: svc.weight,
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
                                    message: format!("Service not found {}", svc.name),
                                    ratio: None,
                                },
                            )),
                        }],
                        ..Default::default()
                    }),
                }
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
                ..Default::default()
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
                        ..Default::default()
                    }],
                },
            )),
        }),
        ..Default::default()
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

fn convert_to_http_filter(filter: Filter) -> outbound::http_route::Filter {
    use outbound::http_route::filter::Kind;

    outbound::http_route::Filter {
        kind: Some(match filter {
            Filter::RequestHeaderModifier(f) => {
                Kind::RequestHeaderModifier(routes::convert_request_header_modifier_filter(f))
            }
            Filter::ResponseHeaderModifier(f) => {
                Kind::ResponseHeaderModifier(routes::convert_response_header_modifier_filter(f))
            }
            Filter::RequestRedirect(f) => Kind::Redirect(routes::convert_redirect_filter(f)),
            Filter::FailureInjector(f) => {
                Kind::FailureInjector(routes::http::convert_failure_injector_filter(f))
            }
        }),
    }
}
