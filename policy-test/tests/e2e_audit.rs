use kube::{Client, ResourceExt};
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{create, create_ready_pod, curl, web, with_temp_ns, LinkerdInject};

#[tokio::test(flavor = "current_thread")]
async fn server_audit() {
    with_temp_ns(|client, ns| async move {
        // Create a server with no policy that should block traffic to the associated pod
        let srv = create(&client, web::server(&ns, None)).await;

        // Create the web pod and wait for it to be ready.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        // All requests should fail
        let curl = curl::Runner::init(&client, &ns).await;
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-injected", "http://web", LinkerdInject::Enabled),
            curl.run("curl-uninjected", "http://web", LinkerdInject::Disabled),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_ne!(injected_status, 0, "injected curl must fail");
        assert_ne!(uninjected_status, 0, "uninjected curl must fail");

        // Patch the server with accessPolicy audit
        let patch = serde_json::json!({
            "spec": {
                "accessPolicy": "audit",
            }
        });
        let patch = k8s::Patch::Merge(patch);
        let api = k8s::Api::<k8s::policy::Server>::namespaced(client.clone(), &ns);
        api.patch(&srv.name_unchecked(), &k8s::PatchParams::default(), &patch)
            .await
            .expect("failed to patch server");

        // All requests should succeed
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-audit-injected", "http://web", LinkerdInject::Enabled),
            curl.run(
                "curl-audit-uninjected",
                "http://web",
                LinkerdInject::Disabled
            ),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_eq!(injected_status, 0, "injected curl must contact web");
        assert_eq!(uninjected_status, 0, "uninjected curl must contact web");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn ns_audit() {
    with_temp_ns(|client, ns| async move {
        change_access_policy(client.clone(), &ns, "cluster-authenticated").await;

        // Create the web pod and wait for it to be ready.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        // Unmeshed requests should fail
        let curl = curl::Runner::init(&client, &ns).await;
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-injected", "http://web", LinkerdInject::Enabled),
            curl.run("curl-uninjected", "http://web", LinkerdInject::Disabled),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_eq!(injected_status, 0, "injected curl must contact web");
        assert_ne!(uninjected_status, 0, "uninjected curl must fail");

        change_access_policy(client.clone(), &ns, "audit").await;

        // Recreate pod for it to pick the new default policy
        let api = kube::Api::<k8s::api::core::v1::Pod>::namespaced(client.clone(), &ns);
        kube::runtime::wait::delete::delete_and_finalize(
            api,
            "web",
            &kube::api::DeleteParams::foreground(),
        )
        .await
        .expect("web pod must be deleted");

        create_ready_pod(&client, web::pod(&ns)).await;

        // All requests should work
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-audit-injected", "http://web", LinkerdInject::Enabled),
            curl.run(
                "curl-audit-uninjected",
                "http://web",
                LinkerdInject::Disabled
            ),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_eq!(injected_status, 0, "injected curl must contact web");
        assert_eq!(uninjected_status, 0, "uninject curl must contact web");
    })
    .await;
}

async fn change_access_policy(client: Client, ns: &str, policy: &str) {
    let api = k8s::Api::<k8s::Namespace>::all(client.clone());
    let patch = serde_json::json!({
        "metadata": {
            "annotations": {
                "config.linkerd.io/default-inbound-policy": policy,
            }
        }
    });
    let patch = k8s::Patch::Merge(patch);
    api.patch(ns, &k8s::PatchParams::default(), &patch)
        .await
        .expect("failed to patch namespace");
}
