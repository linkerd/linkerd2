use super::{
    convert_duration, default_balancer_config, default_outbound_opaq_route, default_queue_config,
};
use crate::routes::{
    convert_host_match, convert_redirect_filter, convert_request_header_modifier_filter,
    convert_response_header_modifier_filter,
    http::{convert_failure_injector_filter, convert_match},
};
use linkerd2_proxy_api::{destination, http_route, meta, outbound};
use linkerd_policy_controller_core::{
    outbound::{
        Backend, Filter, HttpRetryCondition, HttpRoute, OutboundRouteRule, ParentInfo, RouteRetry,
        RouteTimeouts, TrafficPolicy,
    },
    routes::GroupKindNamespaceName,
};
use std::{net::SocketAddr, time};

#[allow(clippy::too_many_arguments)]
pub(crate) fn protocol(
    default_backend: outbound::Backend,
    routes: impl Iterator<Item = (GroupKindNamespaceName, HttpRoute)>,
    accrual: Option<outbound::FailureAccrual>,
    service_retry: Option<RouteRetry<HttpRetryCondition>>,
    service_timeouts: RouteTimeouts,
    allow_l5d_request_headers: bool,
    parent_info: &ParentInfo,
    original_dst: Option<SocketAddr>,
) -> outbound::proxy_protocol::Kind {
    let opaque_route = default_outbound_opaq_route(default_backend.clone(), parent_info);
    let mut routes = routes
        .map(|(gknn, route)| {
            convert_outbound_route(
                gknn,
                route,
                default_backend.clone(),
                service_retry.clone(),
                service_timeouts.clone(),
                allow_l5d_request_headers,
                parent_info,
                original_dst,
            )
        })
        .collect::<Vec<_>>();

    match parent_info {
        ParentInfo::Service { .. } => {
            if routes.is_empty() {
                routes.push(default_outbound_service_route(
                    default_backend,
                    service_retry.clone(),
                    service_timeouts.clone(),
                ));
            }
        }
        ParentInfo::EgressNetwork { traffic_policy, .. } => {
            routes.push(default_outbound_egress_route(
                default_backend,
                service_retry.clone(),
                service_timeouts.clone(),
                traffic_policy,
            ));
        }
    }

    outbound::proxy_protocol::Kind::Detect(outbound::proxy_protocol::Detect {
        timeout: Some(
            time::Duration::from_secs(10)
                .try_into()
                .expect("failed to convert detect timeout to protobuf"),
        ),

        opaque: Some(outbound::proxy_protocol::Opaque {
            routes: vec![opaque_route],
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

#[allow(clippy::too_many_arguments)]
pub(crate) fn http1_only_protocol(
    default_backend: outbound::Backend,
    routes: impl Iterator<Item = (GroupKindNamespaceName, HttpRoute)>,
    accrual: Option<outbound::FailureAccrual>,
    service_retry: Option<RouteRetry<HttpRetryCondition>>,
    service_timeouts: RouteTimeouts,
    allow_l5d_request_headers: bool,
    parent_info: &ParentInfo,
    original_dst: Option<SocketAddr>,
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
                parent_info,
                original_dst,
            )
        })
        .collect::<Vec<_>>();

    match parent_info {
        ParentInfo::Service { .. } => {
            if routes.is_empty() {
                routes.push(default_outbound_service_route(
                    default_backend,
                    service_retry.clone(),
                    service_timeouts.clone(),
                ));
            }
        }
        ParentInfo::EgressNetwork { traffic_policy, .. } => {
            routes.push(default_outbound_egress_route(
                default_backend,
                service_retry.clone(),
                service_timeouts.clone(),
                traffic_policy,
            ));
        }
    }

    outbound::proxy_protocol::Kind::Http1(outbound::proxy_protocol::Http1 {
        routes: routes.clone(),
        failure_accrual: accrual.clone(),
    })
}

#[allow(clippy::too_many_arguments)]
pub(crate) fn http2_only_protocol(
    default_backend: outbound::Backend,
    routes: impl Iterator<Item = (GroupKindNamespaceName, HttpRoute)>,
    accrual: Option<outbound::FailureAccrual>,
    service_retry: Option<RouteRetry<HttpRetryCondition>>,
    service_timeouts: RouteTimeouts,
    allow_l5d_request_headers: bool,
    parent_info: &ParentInfo,
    original_dst: Option<SocketAddr>,
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
                parent_info,
                original_dst,
            )
        })
        .collect::<Vec<_>>();

    match parent_info {
        ParentInfo::Service { .. } => {
            if routes.is_empty() {
                routes.push(default_outbound_service_route(
                    default_backend,
                    service_retry.clone(),
                    service_timeouts.clone(),
                ));
            }
        }
        ParentInfo::EgressNetwork { traffic_policy, .. } => {
            routes.push(default_outbound_egress_route(
                default_backend,
                service_retry.clone(),
                service_timeouts.clone(),
                traffic_policy,
            ));
        }
    }

    outbound::proxy_protocol::Kind::Http2(outbound::proxy_protocol::Http2 {
        routes: routes.clone(),
        failure_accrual: accrual.clone(),
    })
}

#[allow(clippy::too_many_arguments)]
fn convert_outbound_route(
    gknn: GroupKindNamespaceName,
    HttpRoute {
        hostnames,
        rules,
        creation_timestamp: _,
    }: HttpRoute,
    backend: outbound::Backend,
    service_retry: Option<RouteRetry<HttpRetryCondition>>,
    service_timeouts: RouteTimeouts,
    allow_l5d_request_headers: bool,
    parent_info: &ParentInfo,
    original_dst: Option<SocketAddr>,
) -> outbound::HttpRoute {
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
                    .map(|b| convert_backend(b, parent_info, original_dst))
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
                if timeouts == Default::default() {
                    timeouts = service_timeouts.clone();
                }
                if retry.is_none() {
                    retry = service_retry.clone();
                }
                // This encoder sets deprecated timeouts for older proxies.
                #[allow(deprecated)]
                outbound::http_route::Rule {
                    matches: matches.into_iter().map(convert_match).collect(),
                    backends: Some(outbound::http_route::Distribution { kind: Some(dist) }),
                    filters: filters.into_iter().map(convert_to_filter).collect(),
                    request_timeout: timeouts
                        .request
                        .and_then(|d| convert_duration("request timeout", d)),
                    timeouts: Some(convert_timeouts(timeouts)),
                    retry: retry.map(convert_retry),
                    allow_l5d_request_headers,
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

fn convert_backend(
    backend: Backend,
    parent_info: &ParentInfo,
    original_dst: Option<SocketAddr>,
) -> outbound::http_route::WeightedRouteBackend {
    let original_dst_port = original_dst.map(|o| o.port());
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
        Backend::Service(svc) if svc.exists => {
            let filters = svc
                .filters
                .clone()
                .into_iter()
                .map(convert_to_filter)
                .collect();
            outbound::http_route::WeightedRouteBackend {
                weight: svc.weight,
                backend: Some(outbound::http_route::RouteBackend {
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
        Backend::EgressNetwork(egress_net) if egress_net.exists => {
            match (parent_info, original_dst) {
                (
                    ParentInfo::EgressNetwork {
                        name, namespace, ..
                    },
                    Some(original_dst),
                ) => {
                    if *name == egress_net.name && *namespace == egress_net.namespace {
                        let filters = egress_net
                            .filters
                            .clone()
                            .into_iter()
                            .map(convert_to_filter)
                            .collect();

                        outbound::http_route::WeightedRouteBackend {
                            weight: egress_net.weight,
                            backend: Some(outbound::http_route::RouteBackend {
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
                (ParentInfo::EgressNetwork { .. }, None) => invalid_backend(
                    egress_net.weight,
                    "EgressNetwork can be resolved from an ip:port combo only".to_string(),
                    super::egress_net_meta(egress_net, original_dst_port),
                ),
                (ParentInfo::Service { .. }, _) => invalid_backend(
                    egress_net.weight,
                    "EgressNetwork backends attach to EgressNetwork parents only".to_string(),
                    super::egress_net_meta(egress_net, original_dst_port),
                ),
            }
        }
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
) -> outbound::http_route::WeightedRouteBackend {
    outbound::http_route::WeightedRouteBackend {
        weight,
        backend: Some(outbound::http_route::RouteBackend {
            backend: Some(outbound::Backend {
                metadata: Some(meta),
                queue: Some(default_queue_config()),
                kind: None,
            }),
            filters: vec![outbound::http_route::Filter {
                kind: Some(outbound::http_route::filter::Kind::FailureInjector(
                    http_route::HttpFailureInjector {
                        status: 500,
                        message,
                        ratio: None,
                    },
                )),
            }],
            ..Default::default()
        }),
    }
}

pub(crate) fn default_outbound_service_route(
    backend: outbound::Backend,
    service_retry: Option<RouteRetry<HttpRetryCondition>>,
    service_timeouts: RouteTimeouts,
) -> outbound::HttpRoute {
    // This encoder sets deprecated timeouts for older proxies.
    #![allow(deprecated)]
    let metadata = Some(meta::Metadata {
        kind: Some(meta::metadata::Kind::Default("http".to_string())),
    });
    let rules = vec![outbound::http_route::Rule {
        matches: vec![http_route::HttpRouteMatch {
            path: Some(http_route::PathMatch {
                kind: Some(http_route::path_match::Kind::Prefix("/".to_string())),
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
        retry: service_retry.map(convert_retry),
        request_timeout: service_timeouts
            .request
            .and_then(|d| convert_duration("request timeout", d)),
        timeouts: Some(convert_timeouts(service_timeouts)),
        ..Default::default()
    }];
    outbound::HttpRoute {
        metadata,
        rules,
        ..Default::default()
    }
}

pub(crate) fn default_outbound_egress_route(
    backend: outbound::Backend,
    service_retry: Option<RouteRetry<HttpRetryCondition>>,
    service_timeouts: RouteTimeouts,
    traffic_policy: &TrafficPolicy,
) -> outbound::HttpRoute {
    #![allow(deprecated)]
    let (filters, name) = match traffic_policy {
        TrafficPolicy::Allow => (Vec::default(), "http-egress-allow"),
        TrafficPolicy::Deny => (
            vec![outbound::http_route::Filter {
                kind: Some(outbound::http_route::filter::Kind::FailureInjector(
                    http_route::HttpFailureInjector {
                        status: 403,
                        message: "traffic not allowed".to_string(),
                        ratio: None,
                    },
                )),
            }],
            "http-egress-deny",
        ),
    };

    // This encoder sets deprecated timeouts for older proxies.
    let metadata = Some(meta::Metadata {
        kind: Some(meta::metadata::Kind::Default(name.to_string())),
    });
    let rules = vec![outbound::http_route::Rule {
        matches: vec![http_route::HttpRouteMatch {
            path: Some(http_route::PathMatch {
                kind: Some(http_route::path_match::Kind::Prefix("/".to_string())),
            }),
            ..Default::default()
        }],
        backends: Some(outbound::http_route::Distribution {
            kind: Some(outbound::http_route::distribution::Kind::FirstAvailable(
                outbound::http_route::distribution::FirstAvailable {
                    backends: vec![outbound::http_route::RouteBackend {
                        backend: Some(backend),
                        ..Default::default()
                    }],
                },
            )),
        }),
        retry: service_retry.map(convert_retry),
        request_timeout: service_timeouts
            .request
            .and_then(|d| convert_duration("request timeout", d)),
        timeouts: Some(convert_timeouts(service_timeouts)),
        filters,
        ..Default::default()
    }];
    outbound::HttpRoute {
        metadata,
        rules,
        ..Default::default()
    }
}

fn convert_to_filter(filter: Filter) -> outbound::http_route::Filter {
    use outbound::http_route::filter::Kind;

    outbound::http_route::Filter {
        kind: Some(match filter {
            Filter::RequestHeaderModifier(f) => {
                Kind::RequestHeaderModifier(convert_request_header_modifier_filter(f))
            }
            Filter::ResponseHeaderModifier(f) => {
                Kind::ResponseHeaderModifier(convert_response_header_modifier_filter(f))
            }
            Filter::RequestRedirect(f) => Kind::Redirect(convert_redirect_filter(f)),
            Filter::FailureInjector(f) => Kind::FailureInjector(convert_failure_injector_filter(f)),
        }),
    }
}

fn convert_retry(r: RouteRetry<HttpRetryCondition>) -> outbound::http_route::Retry {
    outbound::http_route::Retry {
        max_retries: r.limit.into(),
        max_request_bytes: 64 * 1024,
        backoff: Some(outbound::ExponentialBackoff {
            min_backoff: Some(time::Duration::from_millis(25).try_into().unwrap()),
            max_backoff: Some(time::Duration::from_millis(250).try_into().unwrap()),
            jitter_ratio: 1.0,
        }),
        conditions: Some(r.conditions.iter().flatten().fold(
            outbound::http_route::retry::Conditions::default(),
            |mut cond, c| {
                cond.status_ranges
                    .push(outbound::http_route::retry::conditions::StatusRange {
                        start: c.status_min,
                        end: c.status_max,
                    });
                cond
            },
        )),
        timeout: r.timeout.and_then(|d| convert_duration("retry timeout", d)),
    }
}

fn convert_timeouts(timeouts: RouteTimeouts) -> http_route::Timeouts {
    http_route::Timeouts {
        request: timeouts
            .request
            .and_then(|d| convert_duration("request timeout", d)),
        idle: timeouts
            .idle
            .and_then(|d| convert_duration("idle timeout", d)),
        response: timeouts
            .response
            .and_then(|d| convert_duration("response timeout", d)),
    }
}
