use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{create, create_ready_pod, curl, web, with_temp_ns, LinkerdInject};

#[tokio::test(flavor = "current_thread")]
async fn path_based_routing() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::HttpRoute {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("web-route".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::HttpRouteSpec {
                    inner: k8s::policy::httproute::CommonRouteSpec {
                        parent_refs: Some(vec![k8s::policy::httproute::ParentReference {
                            namespace: None,
                            name: "web".to_string(),
                            port: Some(80),
                            group: Some("core".to_string()),
                            kind: Some("Service".to_string()),
                            section_name: None,
                        }]),
                    },
                    hostnames: None,
                    rules: Some(vec![
                        rule("/valid".to_string(), "web".to_string()),
                        rule("/invalid".to_string(), "foobar".to_string()),
                    ]),
                },
                status: None,
            },
        )
        .await;

        // Create the web pod and wait for it to be ready.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        let curl = curl::Runner::init(&client, &ns).await;
        let (valid, invalid, notfound) = tokio::join!(
            curl.run("curl-valid", "http://web/valid", LinkerdInject::Enabled),
            curl.run("curl-invalid", "http://web/invalid", LinkerdInject::Enabled),
            curl.run(
                "curl-notfound",
                "http://web/notfound",
                LinkerdInject::Enabled
            ),
        );
        let (valid_status, invalid_status, notfound_status) = tokio::join!(
            valid.http_status_code(),
            invalid.http_status_code(),
            notfound.http_status_code()
        );
        assert_eq!(valid_status, 204, "request must be routed to valid backend");
        assert_eq!(invalid_status, 500, "invalid backend must produce 500");
        assert_eq!(
            notfound_status, 404,
            "request not matching any rule must produce 404"
        )
    })
    .await;
}

// === helpers ===

fn rule(path: String, backend: String) -> k8s::policy::httproute::HttpRouteRule {
    k8s::policy::httproute::HttpRouteRule {
        matches: Some(vec![k8s::policy::httproute::HttpRouteMatch {
            path: Some(k8s::policy::httproute::HttpPathMatch::Exact { value: path }),
            ..Default::default()
        }]),
        backend_refs: Some(vec![k8s::policy::httproute::HttpBackendRef {
            backend_ref: Some(k8s::gateway::BackendRef {
                weight: None,
                inner: k8s::gateway::BackendObjectReference {
                    group: None,
                    kind: None,
                    name: backend,
                    namespace: None,
                    port: Some(80),
                },
            }),
            filters: None,
        }]),
        filters: None,
        timeouts: None,
    }
}
