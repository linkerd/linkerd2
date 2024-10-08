use linkerd_policy_controller_k8s_api::{
    self as api,
    policy::unmeshed_network::{DefaultPolicy, UnmeshedNetwork, UnmeshedNetworkSpec},
};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| UnmeshedNetwork {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: UnmeshedNetworkSpec {
            traffic_policy: DefaultPolicy::AllowUnknown,
            networks: vec![
                "10.1.0.0/24".parse().unwrap(),
                "10.1.1.0/24".parse().unwrap(),
            ],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_empty_networks() {
    admission::rejects(|ns| UnmeshedNetwork {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: UnmeshedNetworkSpec {
            traffic_policy: DefaultPolicy::AllowUnknown,
            networks: Default::default(),
        },
    })
    .await;
}
