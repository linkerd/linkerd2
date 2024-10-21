use std::{sync::Arc, vec};

use crate::{
    defaults::DefaultPolicy,
    outbound::index::{Index, SharedIndex},
    ClusterInfo,
};
use k8s_openapi::chrono::Utc;
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::IpNet;
use linkerd_policy_controller_k8s_api::{self as k8s, policy};
use tokio::time;

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
