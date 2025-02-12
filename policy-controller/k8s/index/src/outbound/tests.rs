use std::{sync::Arc, vec};

use crate::{
    defaults::DefaultPolicy,
    outbound::index::{Index, SharedIndex},
    ClusterInfo,
};
use k8s_openapi::chrono::Utc;
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{outbound, IpNet};
use linkerd_policy_controller_core::{
    outbound::{Kind, ResourceTarget},
    POLICY_CONTROLLER_NAME,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s, gateway,
    policy::{self, EgressNetwork},
};
use tokio::time;
use tracing::Level;

mod routes;

struct TestConfig {
    index: SharedIndex,
}

pub fn mk_service(ns: impl ToString, name: impl ToString, port: i32) -> k8s::Service {
    k8s::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: Some(k8s::api::core::v1::ServiceSpec {
            ports: Some(vec![k8s::api::core::v1::ServicePort {
                port,
                ..Default::default()
            }]),
            ..Default::default()
        }),
        ..Default::default()
    }
}

pub fn mk_egress_network(ns: impl ToString, name: impl ToString) -> policy::EgressNetwork {
    policy::EgressNetwork {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: policy::EgressNetworkSpec {
            traffic_policy: policy::TrafficPolicy::Allow,
            networks: None,
        },
        status: Some(policy::EgressNetworkStatus {
            conditions: vec![k8s::Condition {
                last_transition_time: k8s::Time(Utc::now()),
                message: "".to_string(),
                observed_generation: None,
                reason: "Accepted".to_string(),
                status: "True".to_string(),
                type_: "Accepted".to_string(),
            }],
        }),
    }
}

impl TestConfig {
    fn from_default_policy(default_policy: DefaultPolicy) -> Self {
        Self::from_default_policy_with_probes(default_policy, vec![])
    }

    fn from_default_policy_with_probes(
        default_policy: DefaultPolicy,
        probe_networks: Vec<IpNet>,
    ) -> Self {
        let cluster_net = "192.0.2.0/24".parse().unwrap();
        let detect_timeout = time::Duration::from_secs(1);
        let cluster = ClusterInfo {
            networks: vec![cluster_net],
            control_plane_ns: "linkerd".to_string(),
            identity_domain: "cluster.example.com".into(),
            dns_domain: "cluster.example.com".into(),
            default_policy,
            default_detect_timeout: detect_timeout,
            default_opaque_ports: Default::default(),
            probe_networks,
            global_egress_network_namespace: Arc::new("linkerd-egress".to_string()),
        };
        let index = Index::shared(Arc::new(cluster));
        Self { index }
    }
}

impl Default for TestConfig {
    fn default() -> TestConfig {
        Self::from_default_policy(DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: true,
        })
    }
}

#[test]
fn switch_to_another_egress_network_parent() {
    tracing_subscriber::fmt()
        .with_max_level(Level::TRACE)
        .try_init()
        .ok();

    let test = TestConfig::default();
    // Create network b.
    let network_b = mk_egress_network("ns", "b");
    test.index.write().apply(network_b);

    let (ns, name) = test
        .index
        .write()
        .lookup_egress_network("192.168.0.1".parse().unwrap(), "ns".to_string())
        .expect("should resolve");

    assert_eq!(ns, "ns".to_string());
    assert_eq!(name, "b".to_string());

    let mut rx_b = test
        .index
        .write()
        .outbound_policy_rx(ResourceTarget {
            name,
            namespace: ns.clone(),
            port: 8080.try_into().unwrap(),
            source_namespace: ns,
            kind: Kind::EgressNetwork("192.168.0.1:8080".parse().unwrap()),
        })
        .expect("b.ns should exist");

    // first resolution is for network B
    let policy_b = rx_b.borrow_and_update();
    assert_eq!(policy_b.parent_namespace(), "ns");
    assert_eq!(policy_b.parent_name(), "b");
    drop(policy_b);

    // Create network a.
    let network_a = mk_egress_network("ns", "a");
    test.index.write().apply(network_a);

    // watch should be dropped at this point
    assert!(rx_b.has_changed().is_err());

    // now a new resolution should resolve network a

    let (ns, name) = test
        .index
        .write()
        .lookup_egress_network("192.168.0.1".parse().unwrap(), "ns".to_string())
        .expect("should resolve");

    let mut rx_a = test
        .index
        .write()
        .outbound_policy_rx(ResourceTarget {
            name,
            namespace: ns.clone(),
            port: 8080.try_into().unwrap(),
            source_namespace: ns,
            kind: Kind::EgressNetwork("192.168.0.1:8080".parse().unwrap()),
        })
        .expect("a.ns should exist");

    // second resolution is for network A
    let policy_b = rx_a.borrow_and_update();
    assert_eq!(policy_b.parent_namespace(), "ns");
    assert_eq!(policy_b.parent_name(), "a");
}

#[test]
fn fallback_rx_closed_when_egress_net_created() {
    tracing_subscriber::fmt()
        .with_max_level(Level::TRACE)
        .try_init()
        .ok();

    let test = TestConfig::default();

    let fallback_rx = test.index.read().fallback_policy_rx();
    assert!(fallback_rx.has_changed().is_ok());

    // Create network.
    let network = mk_egress_network("ns", "egress-net");
    test.index.write().apply(network);

    assert!(fallback_rx.has_changed().is_err());
}

#[test]
fn fallback_rx_closed_when_egress_net_deleted() {
    tracing_subscriber::fmt()
        .with_max_level(Level::TRACE)
        .try_init()
        .ok();

    let test = TestConfig::default();

    // Create network.
    let network = mk_egress_network("ns", "egress-net");
    test.index.write().apply(network);

    let fallback_rx = test.index.read().fallback_policy_rx();
    assert!(fallback_rx.has_changed().is_ok());

    <Index as kubert::index::IndexNamespacedResource<EgressNetwork>>::delete(
        &mut test.index.write(),
        "ns".into(),
        "egress-net".into(),
    );

    assert!(fallback_rx.has_changed().is_err());
}

#[test]
fn update_backend_on_route_with_no_port() {
    tracing_subscriber::fmt()
        .with_max_level(Level::TRACE)
        .try_init()
        .ok();

    let test = TestConfig::default();
    let ns = "ns";

    let parent = k8s::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("parent-svc".to_string()),
            ..Default::default()
        },
        spec: Some(k8s::ServiceSpec {
            cluster_ip: Some("1.1.1.1".to_string()),
            cluster_ips: Some(vec!["1.1.1.1".to_string()]),
            type_: Some("ClusterIP".to_string()),
            ..Default::default()
        }),
        status: None,
    };

    test.index.write().apply(parent);

    let parent_ref = gateway::ParentReference {
        name: "parent-svc".to_string(),
        namespace: Some(ns.to_string()),
        kind: Some("Service".to_string()),
        group: Some("core".to_string()),
        section_name: None,
        port: None, // Route is attached to parent without specifying port.
    };

    let route = gateway::HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("foo-route".to_string()),
            ..Default::default()
        },
        spec: gateway::HttpRouteSpec {
            inner: gateway::CommonRouteSpec {
                parent_refs: Some(vec![parent_ref.clone()]),
            },
            hostnames: None,
            rules: Some(vec![gateway::HttpRouteRule {
                matches: None,
                filters: None,
                // Reference to a backend that doesn't exist yet.
                backend_refs: Some(vec![gateway::HttpBackendRef {
                    backend_ref: Some(gateway::BackendRef {
                        weight: Some(1),
                        inner: gateway::BackendObjectReference {
                            group: Some("core".to_string()),
                            kind: Some("Service".to_string()),
                            name: "backend-svc".to_string(),
                            namespace: Some(ns.to_string()),
                            port: Some(8080),
                        },
                    }),
                    filters: None,
                }]),
            }]),
        },
        status: Some(gateway::HttpRouteStatus {
            inner: gateway::RouteStatus {
                parents: vec![gateway::RouteParentStatus {
                    parent_ref,
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: vec![k8s::Condition {
                        last_transition_time: k8s::Time(Utc::now()),
                        message: "".to_string(),
                        observed_generation: None,
                        reason: "Accepted".to_string(),
                        status: "True".to_string(),
                        type_: "Accepted".to_string(),
                    }],
                }],
            },
        }),
    };

    test.index.write().apply(route);

    // Create the backend.
    let backend = k8s::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("backend-svc".to_string()),
            ..Default::default()
        },
        spec: Some(k8s::ServiceSpec {
            cluster_ip: Some("1.1.1.2".to_string()),
            cluster_ips: Some(vec!["1.1.1.2".to_string()]),
            type_: Some("ClusterIP".to_string()),
            ..Default::default()
        }),
        status: None,
    };
    test.index.write().apply(backend);

    let mut rx = test
        .index
        .write()
        .outbound_policy_rx(ResourceTarget {
            name: "parent-svc".to_string(),
            namespace: ns.to_string(),
            port: 8080.try_into().unwrap(),
            source_namespace: ns.to_string(),
            kind: Kind::Service,
        })
        .expect("parent-svc should exist");

    // Backend should be valid.
    let policy = rx.borrow_and_update();
    assert_eq!(policy.parent_namespace(), "ns");
    assert_eq!(policy.parent_name(), "parent-svc");
    let (gknn, outbound_route) = policy.http_routes.iter().next().unwrap();
    assert_eq!(gknn.name, "foo-route");
    assert_eq!(gknn.namespace, ns);
    let rule = outbound_route.rules.first().unwrap();
    let backend = rule.backends.first().unwrap();
    if let outbound::Backend::Service(outbound::WeightedService {
        weight: _,
        authority: _,
        name,
        namespace,
        port: _,
        filters: _,
        exists,
    }) = backend
    {
        assert_eq!(name, "backend-svc");
        assert_eq!(namespace, ns);
        assert!(exists);
    } else {
        panic!("expected outbound::Backend::Service");
    }
    drop(policy);
}
