use std::sync::Arc;

use crate::{
    defaults::DefaultPolicy,
    outbound::index::{Index, SharedIndex},
    ClusterInfo,
};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::IpNet;
use linkerd_policy_controller_k8s_api::{self as k8s};
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
