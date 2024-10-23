use super::{default_balancer_config, default_queue_config};
use crate::routes::convert_sni_match;
use linkerd2_proxy_api::{destination, meta, outbound};
use linkerd_policy_controller_core::{
    outbound::{Backend, ResourceOutboundPolicy, TlsRoute, TrafficPolicy},
    routes::GroupKindNamespaceName,
};
use std::net::SocketAddr;

pub(crate) fn protocol(
    default_backend: outbound::Backend,
    routes: impl Iterator<Item = (GroupKindNamespaceName, TlsRoute)>,
    policy: &ResourceOutboundPolicy,
) -> outbound::proxy_protocol::Kind {
    let mut routes = routes
        .map(|(gknn, route)| convert_outbound_route(gknn, route, default_backend.clone(), policy))
        .collect::<Vec<_>>();

    if let ResourceOutboundPolicy::Egress { traffic_policy, .. } = policy {
        routes.push(default_outbound_egress_route(
            default_backend,
            traffic_policy,
        ));
    }

    outbound::proxy_protocol::Kind::Tls(outbound::proxy_protocol::Tls { routes })
}

fn convert_outbound_route(
    gknn: GroupKindNamespaceName,
    TlsRoute {
        hostnames,
        rule,
        creation_timestamp: _,
    }: TlsRoute,
    backend: outbound::Backend,
    policy: &ResourceOutboundPolicy,
) -> outbound::TlsRoute {
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

    let snis = hostnames.into_iter().map(convert_sni_match).collect();

    let backends = rule
        .backends
        .into_iter()
        .map(|b| convert_backend(b, policy))
        .collect::<Vec<_>>();

    let dist = if backends.is_empty() {
        outbound::tls_route::distribution::Kind::FirstAvailable(
            outbound::tls_route::distribution::FirstAvailable {
                backends: vec![outbound::tls_route::RouteBackend {
                    backend: Some(backend.clone()),
                }],
            },
        )
    } else {
        outbound::tls_route::distribution::Kind::RandomAvailable(
            outbound::tls_route::distribution::RandomAvailable { backends },
        )
    };

    let rules = vec![outbound::tls_route::Rule {
        backends: Some(outbound::tls_route::Distribution { kind: Some(dist) }),
    }];

    outbound::TlsRoute {
        metadata,
        snis,
        rules,
        error: None,
    }
}

fn convert_backend(
    backend: Backend,
    policy: &ResourceOutboundPolicy,
) -> outbound::tls_route::WeightedRouteBackend {
    let original_dst_port = match policy {
        ResourceOutboundPolicy::Egress { original_dst, .. } => Some(original_dst.port()),
        ResourceOutboundPolicy::Service { .. } => None,
    };

    match backend {
        Backend::Addr(addr) => {
            let socket_addr = SocketAddr::new(addr.addr, addr.port.get());
            outbound::tls_route::WeightedRouteBackend {
                weight: addr.weight,
                backend: Some(outbound::tls_route::RouteBackend {
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
                }),
                error: None,
            }
        }
        Backend::Service(svc) if svc.exists => outbound::tls_route::WeightedRouteBackend {
            weight: svc.weight,
            backend: Some(outbound::tls_route::RouteBackend {
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
            }),
            error: None,
        },
        Backend::Service(svc) => invalid_backend(
            svc.weight,
            format!("Service not found {}", svc.name),
            super::service_meta(svc),
        ),
        Backend::EgressNetwork(egress_net) if egress_net.exists => match policy {
            ResourceOutboundPolicy::Egress {
                original_dst,
                policy,
                ..
            } => {
                if policy.name == egress_net.name && policy.namespace == egress_net.namespace {
                    outbound::tls_route::WeightedRouteBackend {
                        weight: egress_net.weight,
                        backend: Some(outbound::tls_route::RouteBackend {
                            backend: Some(outbound::Backend {
                                metadata: Some(super::egress_net_meta(
                                    egress_net.clone(),
                                    original_dst_port,
                                )),
                                queue: Some(default_queue_config()),
                                kind: Some(outbound::backend::Kind::Forward(
                                    destination::WeightedAddr {
                                        addr: Some((*original_dst).into()),
                                        weight: egress_net.weight,
                                        ..Default::default()
                                    },
                                )),
                            }),
                        }),
                        error: None,
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
            ResourceOutboundPolicy::Service { .. } => invalid_backend(
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
) -> outbound::tls_route::WeightedRouteBackend {
    outbound::tls_route::WeightedRouteBackend {
        weight,
        backend: Some(outbound::tls_route::RouteBackend {
            backend: Some(outbound::Backend {
                metadata: Some(meta),
                queue: Some(default_queue_config()),
                kind: None,
            }),
        }),
        error: Some(outbound::BackendError { message }),
    }
}

pub(crate) fn default_outbound_egress_route(
    backend: outbound::Backend,
    traffic_policy: &TrafficPolicy,
) -> outbound::TlsRoute {
    #![allow(deprecated)]
    let (error, name) = match traffic_policy {
        TrafficPolicy::Allow => (None, "tls-egress-allow"),
        TrafficPolicy::Deny => (
            Some(outbound::RouteError {
                message: "traffic not allowed".to_string(),
            }),
            "tls-egress-deny",
        ),
    };

    // This encoder sets deprecated timeouts for older proxies.
    let metadata = Some(meta::Metadata {
        kind: Some(meta::metadata::Kind::Default(name.to_string())),
    });
    let rules = vec![outbound::tls_route::Rule {
        backends: Some(outbound::tls_route::Distribution {
            kind: Some(outbound::tls_route::distribution::Kind::FirstAvailable(
                outbound::tls_route::distribution::FirstAvailable {
                    backends: vec![outbound::tls_route::RouteBackend {
                        backend: Some(backend),
                    }],
                },
            )),
        }),
    }];
    outbound::TlsRoute {
        metadata,
        rules,
        error,
        ..Default::default()
    }
}
