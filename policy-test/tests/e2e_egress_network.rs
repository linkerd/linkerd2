use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{
    await_condition, create, create_ready_pod, curl, endpoints_ready, update, web, with_temp_ns,
    LinkerdInject,
};

#[tokio::test(flavor = "current_thread")]
async fn default_traffic_policy() {
    with_temp_ns(|client, ns| async move {
        let mut egress_net = create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("all-egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    networks: None,
                    traffic_policy: k8s::policy::TrafficPolicy::Allow,
                },
                status: None,
            },
        )
        .await;

        let curl = curl::Runner::init(&client, &ns).await;

        let allowed = curl
            .run(
                "curl-allowed",
                "http://httpbin.org/get",
                LinkerdInject::Enabled,
            )
            .await;
        let allowed_status = allowed.http_status_code().await;
        assert_eq!(allowed_status, 200, "request must be allowed");

        egress_net.spec.traffic_policy = k8s::policy::TrafficPolicy::Deny;
        update(&client, egress_net).await;

        let not_allowed = curl
            .run(
                "curl-not-allowed",
                "http://httpbin.org/get",
                LinkerdInject::Enabled,
            )
            .await;
        let not_allowed_status = not_allowed.http_status_code().await;
        assert_eq!(not_allowed_status, 403, "request must be blocked");
    })
    .await;
}
