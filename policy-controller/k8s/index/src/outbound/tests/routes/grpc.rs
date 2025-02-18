use kube::Resource;
use linkerd_policy_controller_core::{
    outbound::{Backend, Kind, ResourceTarget, WeightedEgressNetwork, WeightedService},
    routes::GroupKindNamespaceName,
    POLICY_CONTROLLER_NAME,
};
use linkerd_policy_controller_k8s_api::{gateway, Time};
use tracing::Level;

use super::super::*;

#[test]
fn backend_service() {
    tracing_subscriber::fmt()
        .with_max_level(Level::TRACE)
        .try_init()
        .ok();

    let test = TestConfig::default();
    // Create apex service.
    let apex = mk_service("ns", "apex", 8080);
    test.index.write().apply(apex);

    // Create httproute.
    let route = mk_route(
        "ns",
        "route",
        8080,
        "apex",
        "backend",
        super::BackendKind::Service,
    );
    test.index.write().apply(route);

    let mut rx = test
        .index
        .write()
        .outbound_policy_rx(ResourceTarget {
            name: "apex".to_string(),
            namespace: "ns".to_string(),
            port: 8080.try_into().unwrap(),
            source_namespace: "ns".to_string(),
            kind: Kind::Service,
        })
        .expect("apex.ns should exist");

    {
        let policy = rx.borrow_and_update();
        let backend = policy
            .grpc_routes
            .get(&GroupKindNamespaceName {
                group: gateway::GRPCRoute::group(&()),
                kind: gateway::GRPCRoute::kind(&()),
                namespace: "ns".into(),
                name: "route".into(),
            })
            .expect("route should exist")
            .rules
            .first()
            .expect("rule should exist")
            .backends
            .first()
            .expect("backend should exist");

        let exists = match backend {
            Backend::Service(WeightedService { exists, .. }) => exists,
            _ => panic!("backend should be a service"),
        };

        // Backend should not exist.
        assert!(!exists);
    }

    // Create backend service.
    let backend = mk_service("ns", "backend", 8080);
    test.index.write().apply(backend);
    assert!(rx.has_changed().unwrap());

    {
        let policy = rx.borrow_and_update();
        let backend = policy
            .grpc_routes
            .get(&GroupKindNamespaceName {
                group: gateway::GRPCRoute::group(&()),
                kind: gateway::GRPCRoute::kind(&()),
                namespace: "ns".into(),
                name: "route".into(),
            })
            .expect("route should exist")
            .rules
            .first()
            .expect("rule should exist")
            .backends
            .first()
            .expect("backend should exist");

        let exists = match backend {
            Backend::Service(WeightedService { exists, .. }) => exists,
            _ => panic!("backend should be a service"),
        };

        // Backend should exist.
        assert!(exists);
    }
}

#[test]
fn backend_egress_network() {
    tracing_subscriber::fmt()
        .with_max_level(Level::TRACE)
        .try_init()
        .ok();

    let test = TestConfig::default();
    // Create apex service.
    let apex = mk_egress_network("ns", "apex");
    test.index.write().apply(apex);

    // Create httproute.
    let route = mk_route(
        "ns",
        "route",
        8080,
        "apex",
        "apex",
        super::BackendKind::Egress,
    );
    test.index.write().apply(route);

    let mut rx = test
        .index
        .write()
        .outbound_policy_rx(ResourceTarget {
            name: "apex".to_string(),
            namespace: "ns".to_string(),
            port: 8080.try_into().unwrap(),
            source_namespace: "ns".to_string(),
            kind: Kind::EgressNetwork("192.168.0.1:8080".parse().unwrap()),
        })
        .expect("apex.ns should exist");

    {
        let policy = rx.borrow_and_update();
        let backend = policy
            .grpc_routes
            .get(&GroupKindNamespaceName {
                group: gateway::GRPCRoute::group(&()),
                kind: gateway::GRPCRoute::kind(&()),
                namespace: "ns".into(),
                name: "route".into(),
            })
            .expect("route should exist")
            .rules
            .first()
            .expect("rule should exist")
            .backends
            .first()
            .expect("backend should exist");

        let exists = match backend {
            Backend::Invalid { .. } => &false,
            Backend::EgressNetwork(WeightedEgressNetwork { exists, .. }) => exists,
            _ => panic!("backend should be an egress network, but got {backend:?}"),
        };

        // Backend should exist.
        assert!(exists);
    }
}

fn mk_route(
    ns: impl ToString,
    name: impl ToString,
    port: u16,
    parent: impl ToString,
    backend_name: impl ToString,
    backend: super::BackendKind,
) -> gateway::GRPCRoute {
    let (group, kind) = match backend {
        super::BackendKind::Service => ("core".to_string(), "Service".to_string()),
        super::BackendKind::Egress => {
            ("policy.linkerd.io".to_string(), "EgressNetwork".to_string())
        }
    };

    gateway::GRPCRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            creation_timestamp: Some(Time(Utc::now())),
            ..Default::default()
        },
        spec: gateway::GRPCRouteSpec {
            parent_refs: Some(vec![gateway::GRPCRouteParentRefs {
                group: Some(group.clone()),
                kind: Some(kind.clone()),
                namespace: Some(ns.to_string()),
                name: parent.to_string(),
                section_name: None,
                port: Some(port.into()),
            }]),
            hostnames: None,
            rules: Some(vec![gateway::GRPCRouteRules {
                name: None,
                matches: Some(vec![gateway::GRPCRouteRulesMatches {
                    headers: None,
                    method: Some(gateway::GRPCRouteRulesMatchesMethod {
                        method: Some("Test".to_string()),
                        service: Some("io.linkerd.Testing".to_string()),
                        r#type: Some(gateway::GRPCRouteRulesMatchesMethodType::Exact),
                    }),
                }]),
                filters: None,
                backend_refs: Some(vec![gateway::GRPCRouteRulesBackendRefs {
                    filters: None,
                    weight: None,
                    group: Some(group.clone()),
                    kind: Some(kind.clone()),
                    namespace: Some(ns.to_string()),
                    name: backend_name.to_string(),
                    port: Some(port.into()),
                }]),
                session_persistence: None,
            }]),
        },
        status: Some(gateway::GRPCRouteStatus {
            parents: vec![gateway::GRPCRouteStatusParents {
                parent_ref: gateway::GRPCRouteStatusParentsParentRef {
                    group: Some(group.clone()),
                    kind: Some(kind.clone()),
                    namespace: Some(ns.to_string()),
                    name: parent.to_string(),
                    section_name: None,
                    port: Some(port.into()),
                },
                controller_name: POLICY_CONTROLLER_NAME.to_string(),
                conditions: Some(vec![k8s::Condition {
                    last_transition_time: Time(chrono::DateTime::<Utc>::MIN_UTC),
                    message: "".to_string(),
                    observed_generation: None,
                    reason: "Accepted".to_string(),
                    status: "True".to_string(),
                    type_: "Accepted".to_string(),
                }]),
            }],
        }),
    }
}
