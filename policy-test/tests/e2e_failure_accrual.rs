use kube::ResourceExt;
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{LocalTargetRef, NamespacedTargetRef},
};
use linkerd_policy_test::{create, create_ready_pod, curl, web, with_temp_ns, LinkerdInject};

#[tokio::test(flavor = "current_thread")]
async fn consecutive_failures() {
    with_temp_ns(|client, ns| async move {
        // Create a web service with two pods, one of which always returns 204
        // No Content, and the other of which always returns 500 Internal Server
        // Error;
        let good_pod = {
            let mut pod = web::pod(&ns);
            pod.metadata.name = Some("web-good".to_string());
            pod
        };
        let bad_pod = {
            let mut pod = web::pod(&ns);
            pod.metadata.name = Some("web-bad".to_string());
            pod.spec = Some(k8s::PodSpec {
                containers: vec![k8s::api::core::v1::Container {
                    // Always return 500s.
                    args: Some(vec!["--status".to_string(), "500".to_string()]),
                    ..web::hokay_container()
                }],
                ..Default::default()
            });
            pod
        };
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, good_pod),
            // create_ready_pod(&client, bad_pod),
        );

        let curl = curl::Runner::init(&client, &ns)
            .await
            .run_execable("curl", LinkerdInject::Enabled)
            .await;
        let status = curl
            .curl("http://web")
            .await
            .expect("curl command should succeed");
        tracing::info!(?status);
        assert_eq!(status, "204")
    })
    .await;
}
