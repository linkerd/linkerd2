use super::{default_balancer_config, default_queue_config};
use linkerd2_proxy_api::{destination, meta, outbound};
use linkerd_policy_controller_core::{
    outbound::{Backend, ResourceOutboundPolicy, TcpRoute, TrafficPolicy},
    routes::GroupKindNamespaceName,
};
use std::net::SocketAddr;

pub(crate) fn protocol(
    default_backend: outbound::Backend,
    routes: impl Iterator<Item = (GroupKindNamespaceName, TcpRoute)>,
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

    outbound::proxy_protocol::Kind::Opaque(outbound::proxy_protocol::Opaque { routes })
}

fn convert_outbound_route(
    gknn: GroupKindNamespaceName,
    TcpRoute {
        rule,
        creation_timestamp: _,
    }: TcpRoute,
    backend: outbound::Backend,
    policy: &ResourceOutboundPolicy,
) -> outbound::OpaqueRoute {
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

    let backends = rule
        .backends
        .into_iter()
        .map(|b| convert_backend(b, policy))
        .collect::<Vec<_>>();

    let dist = if backends.is_empty() {
        outbound::opaque_route::distribution::Kind::FirstAvailable(
            outbound::opaque_route::distribution::FirstAvailable {
                backends: vec![outbound::opaque_route::RouteBackend {
                    backend: Some(backend.clone()),
                }],
            },
        )
    } else {
        outbound::opaque_route::distribution::Kind::RandomAvailable(
            outbound::opaque_route::distribution::RandomAvailable { backends },
        )
    };

    let rules = vec![outbound::opaque_route::Rule {
        backends: Some(outbound::opaque_route::Distribution { kind: Some(dist) }),
    }];

    outbound::OpaqueRoute {
        metadata,
        rules,
        error: None,
    }
}

fn convert_backend(
    backend: Backend,
    policy: &ResourceOutboundPolicy,
) -> outbound::opaque_route::WeightedRouteBackend {
    let original_dst_port = match policy {
        ResourceOutboundPolicy::Egress { original_dst, .. } => Some(original_dst.port()),
        ResourceOutboundPolicy::Service { .. } => None,
    };

    match backend {
        Backend::Addr(addr) => {
            let socket_addr = SocketAddr::new(addr.addr, addr.port.get());
            outbound::opaque_route::WeightedRouteBackend {
                weight: addr.weight,
                backend: Some(outbound::opaque_route::RouteBackend {
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
        Backend::Service(svc) if svc.exists => outbound::opaque_route::WeightedRouteBackend {
            weight: svc.weight,
            backend: Some(outbound::opaque_route::RouteBackend {
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
                    outbound::opaque_route::WeightedRouteBackend {
                        weight: egress_net.weight,
                        backend: Some(outbound::opaque_route::RouteBackend {
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
) -> outbound::opaque_route::WeightedRouteBackend {
    outbound::opaque_route::WeightedRouteBackend {
        weight,
        backend: Some(outbound::opaque_route::RouteBackend {
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
) -> outbound::OpaqueRoute {
    #![allow(deprecated)]
    let (error, name) = match traffic_policy {
        TrafficPolicy::Allow => (None, "tcp-egress-allow"),
        TrafficPolicy::Deny => (
            Some(outbound::RouteError {
                message: "traffic not allowed".to_string(),
            }),
            "tcp-egress-deny",
        ),
    };

    // This encoder sets deprecated timeouts for older proxies.
    let metadata = Some(meta::Metadata {
        kind: Some(meta::metadata::Kind::Default(name.to_string())),
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
    outbound::OpaqueRoute {
        metadata,
        rules,
        error,
    }
}
