use std::{net::SocketAddr, time};

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
    outbound::{Backend, Filter, OutboundRoute, OutboundRouteRule},
    routes::{GroupKindNamespaceName, HttpRouteMatch},
};

pub(crate) fn protocol(
    default_backend: outbound::Backend,
    routes: impl Iterator<Item = (GroupKindNamespaceName, OutboundRoute<HttpRouteMatch>)>,
    accrual: Option<outbound::FailureAccrual>,
) -> outbound::proxy_protocol::Kind {
    let opaque_route = default_outbound_opaq_route(default_backend.clone());
    let mut routes = routes
        .map(|(gknn, route)| convert_outbound_route(gknn, route, default_backend.clone()))
        .collect::<Vec<_>>();
    if routes.is_empty() {
        routes.push(default_outbound_route(default_backend));
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

fn convert_outbound_route(
    gknn: GroupKindNamespaceName,
    OutboundRoute {
        hostnames,
        rules,
        creation_timestamp: _,
    }: OutboundRoute<HttpRouteMatch>,
    backend: outbound::Backend,
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
                 request_timeout,
                 backend_request_timeout,
                 filters,
             }| {
                let backend_request_timeout = backend_request_timeout
                    .and_then(|d| convert_duration("backend request_timeout", d));
                let backends = backends
                    .into_iter()
                    .map(|backend| convert_backend(backend_request_timeout.clone(), backend))
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
                    matches: matches.into_iter().map(convert_match).collect(),
                    backends: Some(outbound::http_route::Distribution { kind: Some(dist) }),
                    filters: filters.into_iter().map(convert_to_filter).collect(),
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

fn convert_backend(
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
            if svc.exists {
                let filters = svc.filters.into_iter().map(convert_to_filter).collect();
                outbound::http_route::WeightedRouteBackend {
                    weight: svc.weight,
                    backend: Some(outbound::http_route::RouteBackend {
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
                        request_timeout,
                    }),
                }
            } else {
                outbound::http_route::WeightedRouteBackend {
                    weight: svc.weight,
                    backend: Some(outbound::http_route::RouteBackend {
                        backend: Some(outbound::Backend {
                            metadata: Some(meta::Metadata {
                                kind: Some(meta::metadata::Kind::Default("invalid".to_string())),
                            }),
                            queue: Some(default_queue_config()),
                            kind: None,
                        }),
                        filters: vec![outbound::http_route::Filter {
                            kind: Some(outbound::http_route::filter::Kind::FailureInjector(
                                http_route::HttpFailureInjector {
                                    status: 500,
                                    message: format!("Service not found {}", svc.name),
                                    ratio: None,
                                },
                            )),
                        }],
                        request_timeout,
                    }),
                }
            }
        }
        Backend::Invalid { weight, message } => outbound::http_route::WeightedRouteBackend {
            weight,
            backend: Some(outbound::http_route::RouteBackend {
                backend: Some(outbound::Backend {
                    metadata: Some(meta::Metadata {
                        kind: Some(meta::metadata::Kind::Default("invalid".to_string())),
                    }),
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
                request_timeout,
            }),
        },
    }
}

pub(crate) fn default_outbound_route(backend: outbound::Backend) -> outbound::HttpRoute {
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
