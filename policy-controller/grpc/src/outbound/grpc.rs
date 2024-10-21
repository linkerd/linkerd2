use super::{convert_duration, default_balancer_config, default_queue_config};
use crate::routes::{
    convert_host_match, convert_request_header_modifier_filter, grpc::convert_match,
};
use linkerd2_proxy_api::{destination, grpc_route, http_route, meta, outbound};
use linkerd_policy_controller_core::{
    outbound::{
        Backend, Filter, GrpcRetryCondition, GrpcRoute, Kind, OutboundDiscoverTarget,
        OutboundRoute, OutboundRouteRule, RouteRetry, RouteTimeouts, TrafficPolicy,
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
    target: OutboundDiscoverTarget,
) -> outbound::proxy_protocol::Kind {
    let mut routes = routes
        .map(|(gknn, route)| {
            convert_outbound_route(
                gknn,
                route,
                default_backend.clone(),
                service_retry.clone(),
                service_timeouts.clone(),
                allow_l5d_request_headers,
                target.clone(),
            )
        })
        .collect::<Vec<_>>();

    if let Kind::EgressNetwork { traffic_policy, .. } = target.kind {
        routes.push(default_outbound_egress_route(
            default_backend,
            service_retry,
            service_timeouts,
            traffic_policy,
        ));
    }

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
    target: OutboundDiscoverTarget,
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
                    .map(|b| convert_backend(b, target.clone()))
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
    target: OutboundDiscoverTarget,
) -> outbound::grpc_route::WeightedRouteBackend {
    let original_dst_port = match target.kind {
        Kind::EgressNetwork { original_dst, .. } => Some(original_dst.port()),
        Kind::Service => None,
    };

    match backend {
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
        Backend::Service(svc) if svc.exists => {
            let filters = svc
                .filters
                .clone()
                .into_iter()
                .map(convert_to_filter)
                .collect();
            outbound::grpc_route::WeightedRouteBackend {
                weight: svc.weight,
                backend: Some(outbound::grpc_route::RouteBackend {
                    backend: Some(outbound::Backend {
                        metadata: Some(super::service_meta(svc.clone())),
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
        }
        Backend::Service(svc) => invalid_backend(
            svc.weight,
            format!("Service not found {}", svc.name),
            super::service_meta(svc),
        ),
        Backend::EgressNetwork(egress_net) if egress_net.exists => match target.kind {
            Kind::EgressNetwork { original_dst, .. } => {
                if target.name == egress_net.name && target.namespace == egress_net.namespace {
                    let filters = egress_net
                        .filters
                        .clone()
                        .into_iter()
                        .map(convert_to_filter)
                        .collect();

                    outbound::grpc_route::WeightedRouteBackend {
                        weight: egress_net.weight,
                        backend: Some(outbound::grpc_route::RouteBackend {
                            backend: Some(outbound::Backend {
                                metadata: Some(super::egress_net_meta(
                                    egress_net.clone(),
                                    original_dst_port,
                                )),
                                queue: Some(default_queue_config()),
                                kind: Some(outbound::backend::Kind::Forward(
                                    destination::WeightedAddr {
                                        addr: Some(original_dst.into()),
                                        weight: egress_net.weight,
                                        ..Default::default()
                                    },
                                )),
                            }),
                            filters,
                            ..Default::default()
                        }),
                    }
                } else {
                    let weight = egress_net.weight;
                    let message =  "Route with EgressNetwork backend needs to have the same EgressNetwork as a parent".to_string();
                    invalid_backend(
                        weight,
                        message,
                        super::egress_net_meta(egress_net, original_dst_port),
                    )
                }
            }
            Kind::Service => invalid_backend(
                egress_net.weight,
                "EgressNetwork backends attach to EgressNetwork parents only".to_string(),
                super::egress_net_meta(egress_net, original_dst_port),
            ),
        },
        Backend::EgressNetwork(egress_net) => invalid_backend(
            egress_net.weight,
            format!("EgressNetwork not found {}", egress_net.name),
            super::egress_net_meta(egress_net, original_dst_port),
        ),
        Backend::Invalid { weight, message } => invalid_backend(
            weight,
            message,
            meta::Metadata {
                kind: Some(meta::metadata::Kind::Default("invalid".to_string())),
            },
        ),
    }
}

fn invalid_backend(
    weight: u32,
    message: String,
    meta: meta::Metadata,
) -> outbound::grpc_route::WeightedRouteBackend {
    outbound::grpc_route::WeightedRouteBackend {
        weight,
        backend: Some(outbound::grpc_route::RouteBackend {
            backend: Some(outbound::Backend {
                metadata: Some(meta),
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
    }
}

pub(crate) fn default_outbound_egress_route(
    backend: outbound::Backend,
    service_retry: Option<RouteRetry<GrpcRetryCondition>>,
    service_timeouts: RouteTimeouts,
    traffic_policy: TrafficPolicy,
) -> outbound::GrpcRoute {
    #![allow(deprecated)]
    let (filters, name) = match traffic_policy {
        TrafficPolicy::Allow => (Vec::default(), "grpc-egress-allow"),
        TrafficPolicy::Deny => (
            vec![outbound::grpc_route::Filter {
                kind: Some(outbound::grpc_route::filter::Kind::FailureInjector(
                    grpc_route::GrpcFailureInjector {
                        code: 7,
                        message: "traffic not allowed".to_string(),
                        ratio: None,
                    },
                )),
            }],
            "grpc-egress-deny",
        ),
    };

    // This encoder sets deprecated timeouts for older proxies.
    let metadata = Some(meta::Metadata {
        kind: Some(meta::metadata::Kind::Default(name.to_string())),
    });
    let rules = vec![outbound::grpc_route::Rule {
        matches: vec![grpc_route::GrpcRouteMatch::default()],
        backends: Some(outbound::grpc_route::Distribution {
            kind: Some(outbound::grpc_route::distribution::Kind::FirstAvailable(
                outbound::grpc_route::distribution::FirstAvailable {
                    backends: vec![outbound::grpc_route::RouteBackend {
                        backend: Some(backend),
                        ..Default::default()
                    }],
                },
            )),
        }),
        request_timeout: service_timeouts
            .request
            .and_then(|d| convert_duration("request timeout", d)),
        timeouts: Some(http_route::Timeouts {
            request: service_timeouts
                .request
                .and_then(|d| convert_duration("stream timeout", d)),
            idle: service_timeouts
                .idle
                .and_then(|d| convert_duration("idle timeout", d)),
            response: service_timeouts
                .response
                .and_then(|d| convert_duration("response timeout", d)),
        }),
        retry: service_retry.map(|r| outbound::grpc_route::Retry {
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
                        GrpcRetryCondition::DeadlineExceeded => cond.deadine_exceeded = true,
                        GrpcRetryCondition::Internal => cond.internal = true,
                        GrpcRetryCondition::ResourceExhausted => cond.resource_exhausted = true,
                        GrpcRetryCondition::Unavailable => cond.unavailable = true,
                    };
                    cond
                },
            )),
            timeout: r.timeout.and_then(|d| convert_duration("retry timeout", d)),
        }),
        filters,
        ..Default::default()
    }];
    outbound::GrpcRoute {
        metadata,
        rules,
        ..Default::default()
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
