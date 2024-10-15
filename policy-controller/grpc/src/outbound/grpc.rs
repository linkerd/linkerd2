use super::{convert_duration, default_balancer_config, default_queue_config};
use crate::routes::{
    convert_host_match, convert_request_header_modifier_filter, grpc::convert_match,
};
use linkerd2_proxy_api::{destination, grpc_route, http_route, meta, outbound};
use linkerd_policy_controller_core::{
    outbound::{
        Backend, Filter, GrpcRetryCondition, GrpcRoute, OutboundRoute, OutboundRouteRule,
        RouteRetry, RouteTimeouts,
    },
    routes::{FailureInjectorFilter, GroupKindNamespaceName},
};
use std::{net::SocketAddr, time};

pub(crate) fn protocol(
    default_backend: outbound::Backend,
    routes: impl Iterator<Item = (GroupKindNamespaceName, GrpcRoute)>,
    failure_accrual: Option<outbound::FailureAccrual>,
    service_retry: Option<RouteRetry<GrpcRetryCondition>>,
    service_timeouts: RouteTimeouts,
    allow_l5d_request_headers: bool,
    original_dst: Option<SocketAddr>,
) -> outbound::proxy_protocol::Kind {
    let routes = routes
        .map(|(gknn, route)| {
            convert_outbound_route(
                gknn,
                route,
                default_backend.clone(),
                service_retry.clone(),
                service_timeouts.clone(),
                allow_l5d_request_headers,
                original_dst,
            )
        })
        .collect::<Vec<_>>();
    outbound::proxy_protocol::Kind::Grpc(outbound::proxy_protocol::Grpc {
        routes,
        failure_accrual,
    })
}

fn convert_outbound_route(
    gknn: GroupKindNamespaceName,
    OutboundRoute {
        hostnames,
        rules,
        creation_timestamp: _,
    }: GrpcRoute,
    backend: outbound::Backend,
    service_retry: Option<RouteRetry<GrpcRetryCondition>>,
    service_timeouts: RouteTimeouts,
    allow_l5d_request_headers: bool,
    original_dst: Option<SocketAddr>,
) -> outbound::GrpcRoute {
    // This encoder sets deprecated timeouts for older proxies.
    #![allow(deprecated)]

    let metadata = Some(meta::Metadata {
        kind: Some(meta::metadata::Kind::Resource(meta::Resource {
            group: gknn.group.to_string(),
            kind: gknn.kind.to_string(),
            namespace: gknn.namespace.to_string(),
            name: gknn.name.to_string(),
            ..Default::default()
        })),
    });

    let hosts = hostnames.into_iter().map(convert_host_match).collect();

    let rules = rules
        .into_iter()
        .map(
            |OutboundRouteRule {
                 matches,
                 backends,
                 mut retry,
                 mut timeouts,
                 filters,
             }| {
                let backends = backends
                    .into_iter()
                    .map(|b| convert_backend(b, original_dst))
                    .collect::<Vec<_>>();
                let dist = if backends.is_empty() {
                    outbound::grpc_route::distribution::Kind::FirstAvailable(
                        outbound::grpc_route::distribution::FirstAvailable {
                            backends: vec![outbound::grpc_route::RouteBackend {
                                backend: Some(backend.clone()),
                                filters: vec![],
                                ..Default::default()
                            }],
                        },
                    )
                } else {
                    outbound::grpc_route::distribution::Kind::RandomAvailable(
                        outbound::grpc_route::distribution::RandomAvailable { backends },
                    )
                };
                if timeouts == Default::default() {
                    timeouts = service_timeouts.clone();
                }
                if retry.is_none() {
                    retry = service_retry.clone();
                }
                outbound::grpc_route::Rule {
                    matches: matches.into_iter().map(convert_match).collect(),
                    backends: Some(outbound::grpc_route::Distribution { kind: Some(dist) }),
                    filters: filters.into_iter().map(convert_to_filter).collect(),
                    request_timeout: timeouts
                        .request
                        .and_then(|d| convert_duration("request timeout", d)),
                    timeouts: Some(http_route::Timeouts {
                        request: timeouts
                            .request
                            .and_then(|d| convert_duration("stream timeout", d)),
                        idle: timeouts
                            .idle
                            .and_then(|d| convert_duration("idle timeout", d)),
                        response: timeouts
                            .response
                            .and_then(|d| convert_duration("response timeout", d)),
                    }),
                    retry: retry.map(|r| outbound::grpc_route::Retry {
                        max_retries: r.limit.into(),
                        max_request_bytes: 64 * 1024,
                        backoff: Some(outbound::ExponentialBackoff {
                            min_backoff: Some(time::Duration::from_millis(25).try_into().unwrap()),
                            max_backoff: Some(time::Duration::from_millis(250).try_into().unwrap()),
                            jitter_ratio: 1.0,
                        }),
                        conditions: Some(r.conditions.iter().flatten().fold(
                            outbound::grpc_route::retry::Conditions::default(),
                            |mut cond, c| {
                                match c {
                                    GrpcRetryCondition::Cancelled => cond.cancelled = true,
                                    GrpcRetryCondition::DeadlineExceeded => {
                                        cond.deadine_exceeded = true
                                    }
                                    GrpcRetryCondition::Internal => cond.internal = true,
                                    GrpcRetryCondition::ResourceExhausted => {
                                        cond.resource_exhausted = true
                                    }
                                    GrpcRetryCondition::Unavailable => cond.unavailable = true,
                                };
                                cond
                            },
                        )),
                        timeout: r.timeout.and_then(|d| convert_duration("retry timeout", d)),
                    }),
                    allow_l5d_request_headers,
                }
            },
        )
        .collect();

    outbound::GrpcRoute {
        metadata,
        hosts,
        rules,
    }
}

fn convert_backend(
    backend: Backend,
    original_dst: Option<SocketAddr>,
) -> outbound::grpc_route::WeightedRouteBackend {
    match backend {
        Backend::Forward => {
            if let Some(original_dst) = original_dst {
                outbound::grpc_route::WeightedRouteBackend {
                    weight: 1,
                    backend: Some(outbound::grpc_route::RouteBackend {
                        backend: Some(outbound::Backend {
                            metadata: None,
                            queue: Some(default_queue_config()),
                            kind: Some(outbound::backend::Kind::Forward(
                                destination::WeightedAddr {
                                    addr: Some(original_dst.into()),
                                    weight: 1,
                                    ..Default::default()
                                },
                            )),
                        }),
                        filters: Default::default(),
                        ..Default::default()
                    }),
                }
            } else {
                outbound::grpc_route::WeightedRouteBackend {
                    weight: 1,
                    backend: Some(outbound::grpc_route::RouteBackend {
                        backend: Some(outbound::Backend {
                            metadata: Some(meta::Metadata {
                                kind: Some(meta::metadata::Kind::Default("invalid".to_string())),
                            }),
                            queue: Some(default_queue_config()),
                            kind: None,
                        }),
                        filters: vec![outbound::grpc_route::Filter {
                            kind: Some(outbound::grpc_route::filter::Kind::FailureInjector(
                                grpc_route::GrpcFailureInjector {
                                    code: 500,
                                    message: "Forward backend needs an original_dst".to_string(),
                                    ratio: None,
                                },
                            )),
                        }],
                        ..Default::default()
                    }),
                }
            }
        }
        Backend::Addr(addr) => {
            let socket_addr = SocketAddr::new(addr.addr, addr.port.get());
            outbound::grpc_route::WeightedRouteBackend {
                weight: addr.weight,
                backend: Some(outbound::grpc_route::RouteBackend {
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
                let filters = svc.filters.into_iter().map(convert_to_filter).collect();
                outbound::grpc_route::WeightedRouteBackend {
                    weight: svc.weight,
                    backend: Some(outbound::grpc_route::RouteBackend {
                        backend: Some(outbound::Backend {
                            metadata: Some(meta::Metadata {
                                kind: Some(meta::metadata::Kind::Resource(meta::Resource {
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
                outbound::grpc_route::WeightedRouteBackend {
                    weight: svc.weight,
                    backend: Some(outbound::grpc_route::RouteBackend {
                        backend: Some(outbound::Backend {
                            metadata: Some(meta::Metadata {
                                kind: Some(meta::metadata::Kind::Default("invalid".to_string())),
                            }),
                            queue: Some(default_queue_config()),
                            kind: None,
                        }),
                        filters: vec![outbound::grpc_route::Filter {
                            kind: Some(outbound::grpc_route::filter::Kind::FailureInjector(
                                grpc_route::GrpcFailureInjector {
                                    code: 500,
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
        Backend::Invalid { weight, message } => outbound::grpc_route::WeightedRouteBackend {
            weight,
            backend: Some(outbound::grpc_route::RouteBackend {
                backend: Some(outbound::Backend {
                    metadata: Some(meta::Metadata {
                        kind: Some(meta::metadata::Kind::Default("invalid".to_string())),
                    }),
                    queue: Some(default_queue_config()),
                    kind: None,
                }),
                filters: vec![outbound::grpc_route::Filter {
                    kind: Some(outbound::grpc_route::filter::Kind::FailureInjector(
                        grpc_route::GrpcFailureInjector {
                            code: 500,
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

fn convert_to_filter(filter: Filter) -> outbound::grpc_route::Filter {
    use outbound::grpc_route::filter::Kind as GrpcFilterKind;

    outbound::grpc_route::Filter {
        kind: match filter {
            Filter::FailureInjector(FailureInjectorFilter {
                status,
                message,
                ratio,
            }) => Some(GrpcFilterKind::FailureInjector(
                grpc_route::GrpcFailureInjector {
                    code: u32::from(status.as_u16()),
                    message,
                    ratio: Some(http_route::Ratio {
                        numerator: ratio.numerator,
                        denominator: ratio.denominator,
                    }),
                },
            )),
            Filter::RequestHeaderModifier(filter) => Some(GrpcFilterKind::RequestHeaderModifier(
                convert_request_header_modifier_filter(filter),
            )),
            Filter::RequestRedirect(filter) => {
                tracing::warn!(filter = ?filter, "declining to convert invalid filter type for GrpcRoute");
                None
            }
            Filter::ResponseHeaderModifier(filter) => {
                tracing::warn!(filter = ?filter, "declining to convert invalid filter type for GrpcRoute");
                None
            }
        },
    }
}
