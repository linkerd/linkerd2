use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{
    assert_status_accepted, await_condition, await_egress_net_status, await_gateway_route_status,
    await_tcp_route_status, await_tls_route_status, create, create_ready_pod, curl,
    endpoints_ready, web, with_temp_ns, LinkerdInject,
};

#[tokio::test(flavor = "current_thread")]
async fn default_traffic_policy_http_allow() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Allow,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        let curl = curl::Runner::init(&client, &ns).await;
        let allowed = curl
            .run(
                "curl-allowed",
                "http://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let allowed_status = allowed.http_status_code().await;
        assert_eq!(allowed_status, 200, "traffic should be allowed");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn default_traffic_policy_http_deny() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Deny,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        let curl = curl::Runner::init(&client, &ns).await;
        let not_allowed = curl
            .run(
                "curl-not-allowed",
                "http://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let not_allowed_status = not_allowed.http_status_code().await;
        assert_eq!(not_allowed_status, 403, "traffic should be blocked");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn default_traffic_policy_opaque_allow() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Allow,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        let curl = curl::Runner::init(&client, &ns).await;
        let allowed = curl
            .run(
                "curl-allowed",
                "https://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let allowed_status = allowed.http_status_code().await;
        assert_eq!(allowed_status, 200, "traffic should be allowed");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn default_traffic_policy_opaque_deny() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Deny,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        let curl = curl::Runner::init(&client, &ns).await;
        let not_allowed = curl
            .run(
                "curl-not-allowed",
                "https://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let not_allowed_exit_code = not_allowed.exit_code().await;
        assert_ne!(not_allowed_exit_code, 0, "traffic should be blocked");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn explicit_allow_http_route() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Deny,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        let curl = curl::Runner::init(&client, &ns).await;
        let not_allowed_get = curl
            .run(
                "curl-not-allowed-get",
                "http://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let not_allowed_get_status = not_allowed_get.http_status_code().await;
        assert_eq!(not_allowed_get_status, 403, "traffic should be blocked");

        // Now create an http route that will allow explicit hostname and explicit path
        create(
            &client,
            k8s::gateway::HttpRoute {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("http-route".to_string()),
                    ..Default::default()
                },
                spec: k8s::gateway::HttpRouteSpec {
                    inner: k8s::gateway::CommonRouteSpec {
                        parent_refs: Some(vec![k8s::policy::httproute::ParentReference {
                            namespace: None,
                            name: "egress".to_string(),
                            port: Some(80),
                            group: Some("policy.linkerd.io".to_string()),
                            kind: Some("EgressNetwork".to_string()),
                            section_name: None,
                        }]),
                    },
                    hostnames: None,
                    rules: Some(vec![k8s::gateway::HttpRouteRule {
                        matches: Some(vec![k8s::policy::httproute::HttpRouteMatch {
                            path: Some(k8s::policy::httproute::HttpPathMatch::Exact {
                                value: "/get".to_string(),
                            }),
                            ..Default::default()
                        }]),
                        backend_refs: None,
                        filters: None,
                    }]),
                },
                status: None,
            },
        )
        .await;
        await_gateway_route_status(&client, &ns, "http-route").await;

        // traffic should be allowed for /get request
        let allowed_get = curl
            .run(
                "curl-allowed-get",
                "http://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let allowed_get_status = allowed_get.http_status_code().await;
        assert_eq!(allowed_get_status, 200, "traffic should be allowed");

        // traffic should not be allowed for /ip request
        let not_allowed_ip = curl
            .run(
                "curl-not-allowed-ip",
                "http://postman-echo.com/ip",
                LinkerdInject::Enabled,
            )
            .await;

        let not_allowed_ip_status = not_allowed_ip.http_status_code().await;
        assert_eq!(not_allowed_ip_status, 403, "traffic should not be allowed");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn explicit_allow_tls_route() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Deny,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        let curl = curl::Runner::init(&client, &ns).await;
        let not_allowed_external = curl
            .run(
                "not-allowed-external",
                "https://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let not_allowed_external_exit_code = not_allowed_external.exit_code().await;
        assert_ne!(
            not_allowed_external_exit_code, 0,
            "traffic should be blocked"
        );

        // Now create a tls route that will allow explicit hostname and explicit path
        create(
            &client,
            k8s_gateway_api::TlsRoute {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("tls-route".to_string()),
                    ..Default::default()
                },
                spec: k8s_gateway_api::TlsRouteSpec {
                    inner: k8s_gateway_api::CommonRouteSpec {
                        parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                            namespace: None,
                            name: "egress".to_string(),
                            port: Some(443),
                            group: Some("policy.linkerd.io".to_string()),
                            kind: Some("EgressNetwork".to_string()),
                            section_name: None,
                        }]),
                    },
                    hostnames: Some(vec!["postman-echo.com".to_string()]),
                    rules: vec![k8s_gateway_api::TlsRouteRule {
                        backend_refs: vec![k8s_gateway_api::BackendRef {
                            weight: None,
                            inner: k8s_gateway_api::BackendObjectReference {
                                namespace: None,
                                name: "egress".to_string(),
                                port: Some(443),
                                group: Some("policy.linkerd.io".to_string()),
                                kind: Some("EgressNetwork".to_string()),
                            },
                        }],
                    }],
                },
                status: None,
            },
        )
        .await;
        await_tls_route_status(&client, &ns, "tls-route").await;

        // External traffic should be allowed.
        let allowed_external = curl
            .run(
                "allowed-external",
                "https://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let allowed_external_status = allowed_external.http_status_code().await;
        assert_eq!(allowed_external_status, 200, "traffic should be allowed");

        // traffic should not be allowed for google.com
        let not_allowed_google = curl
            .run(
                "curl-not-allowed-google",
                "https://google.com/",
                LinkerdInject::Enabled,
            )
            .await;

        let not_allowed_google_exit_code = not_allowed_google.exit_code().await;
        assert_ne!(
            not_allowed_google_exit_code, 0,
            "traffic should not be allowed"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn explicit_allow_tcp_route() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Deny,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        let curl = curl::Runner::init(&client, &ns).await;
        let not_allowed_external = curl
            .run(
                "not-allowed-external",
                "https://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let not_allowed_external_exit_code = not_allowed_external.exit_code().await;
        assert_ne!(
            not_allowed_external_exit_code, 0,
            "traffic should be blocked"
        );

        // Now create a tcp route that will allow explicit hostname and explicit path
        create(
            &client,
            k8s_gateway_api::TcpRoute {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("tcp-route".to_string()),
                    ..Default::default()
                },
                spec: k8s_gateway_api::TcpRouteSpec {
                    inner: k8s_gateway_api::CommonRouteSpec {
                        parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                            namespace: None,
                            name: "egress".to_string(),
                            port: Some(443),
                            group: Some("policy.linkerd.io".to_string()),
                            kind: Some("EgressNetwork".to_string()),
                            section_name: None,
                        }]),
                    },
                    rules: vec![k8s_gateway_api::TcpRouteRule {
                        backend_refs: vec![k8s_gateway_api::BackendRef {
                            weight: None,
                            inner: k8s_gateway_api::BackendObjectReference {
                                namespace: None,
                                name: "egress".to_string(),
                                port: Some(443),
                                group: Some("policy.linkerd.io".to_string()),
                                kind: Some("EgressNetwork".to_string()),
                            },
                        }],
                    }],
                },
                status: None,
            },
        )
        .await;
        await_tcp_route_status(&client, &ns, "tcp-route").await;

        // External traffic should be allowed on 443.
        let allowed_external = curl
            .run(
                "allowed-external",
                "https://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let allowed_external_status = allowed_external.http_status_code().await;
        assert_eq!(allowed_external_status, 200, "traffic should be allowed");

        // External traffic should not be allowed on 80.
        let not_allowed_google = curl
            .run(
                "curl-not-allowed-google",
                "http://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let not_allowed_google_status = not_allowed_google.http_status_code().await;
        assert_eq!(
            not_allowed_google_status, 403,
            "traffic should not be allowed"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn routing_back_to_cluster_http_route() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Allow,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        // Create the web pod and wait for it to be ready.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        // Now create an http route that will route requests
        // back to the cluster if the request path is /get
        // and will let the rest go through
        create(
            &client,
            k8s::gateway::HttpRoute {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("http-route".to_string()),
                    ..Default::default()
                },
                spec: k8s::gateway::HttpRouteSpec {
                    inner: k8s_gateway_api::CommonRouteSpec {
                        parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                            namespace: None,
                            name: "egress".to_string(),
                            port: Some(80),
                            group: Some("policy.linkerd.io".to_string()),
                            kind: Some("EgressNetwork".to_string()),
                            section_name: None,
                        }]),
                    },
                    hostnames: Some(vec!["postman-echo.com".to_string()]),
                    rules: Some(vec![k8s::gateway::HttpRouteRule {
                        matches: Some(vec![k8s_gateway_api::HttpRouteMatch {
                            path: Some(k8s_gateway_api::HttpPathMatch::Exact {
                                value: "/get".to_string(),
                            }),
                            ..Default::default()
                        }]),
                        backend_refs: Some(vec![k8s_gateway_api::HttpBackendRef {
                            backend_ref: Some(k8s_gateway_api::BackendRef {
                                weight: None,
                                inner: k8s_gateway_api::BackendObjectReference {
                                    namespace: Some(ns.clone()),
                                    name: "web".to_string(),
                                    port: Some(80),
                                    group: None,
                                    kind: None,
                                },
                            }),
                            filters: None,
                        }]),
                        filters: None,
                    }]),
                },
                status: None,
            },
        )
        .await;
        await_gateway_route_status(&client, &ns, "http-route").await;

        let curl = curl::Runner::init(&client, &ns).await;
        let (in_cluster, out_of_cluster) = tokio::join!(
            curl.run(
                "curl-in-cluster",
                "http://postman-echo.com/get",
                LinkerdInject::Enabled
            ),
            curl.run(
                "curl-out-of-cluster",
                "http://postman-echo.com/ip",
                LinkerdInject::Enabled
            ),
        );

        let (in_cluster_status, out_of_cluster_status) = tokio::join!(
            in_cluster.http_status_code(),
            out_of_cluster.http_status_code(),
        );

        assert_eq!(in_cluster_status, 204); // in-cluster service returns 204
        assert_eq!(out_of_cluster_status, 200); // external service returns 200
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn routing_back_to_cluster_tls_route() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Allow,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        // Create the web pod and wait for it to be ready.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        // Now create an tls route that will route requests
        // to an in-cluster service based on SNI
        create(
            &client,
            k8s_gateway_api::TlsRoute {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("tls-route".to_string()),
                    ..Default::default()
                },
                spec: k8s_gateway_api::TlsRouteSpec {
                    inner: k8s_gateway_api::CommonRouteSpec {
                        parent_refs: Some(vec![k8s::policy::httproute::ParentReference {
                            namespace: None,
                            name: "egress".to_string(),
                            port: Some(443),
                            group: Some("policy.linkerd.io".to_string()),
                            kind: Some("EgressNetwork".to_string()),
                            section_name: None,
                        }]),
                    },
                    hostnames: Some(vec!["postman-echo.com".to_string()]),
                    rules: vec![k8s_gateway_api::TlsRouteRule {
                        backend_refs: vec![k8s::gateway::BackendRef {
                            weight: None,
                            inner: k8s_gateway_api::BackendObjectReference {
                                namespace: Some(ns.clone()),
                                name: "web".to_string(),
                                port: Some(80),
                                group: None,
                                kind: None,
                            },
                        }],
                    }],
                },
                status: None,
            },
        )
        .await;
        await_tls_route_status(&client, &ns, "tls-route").await;

        let curl = curl::Runner::init(&client, &ns).await;
        let (in_cluster, out_of_cluster) = tokio::join!(
            curl.run(
                "curl-in-cluster",
                "https://postman-echo.com/get",
                LinkerdInject::Enabled
            ),
            curl.run(
                "curl-out-of-cluster",
                "https://google.com/not-there",
                LinkerdInject::Enabled
            ),
        );

        let (in_cluster_exit_code, out_of_cluster_status) =
            tokio::join!(in_cluster.exit_code(), out_of_cluster.http_status_code(),);

        assert_ne!(in_cluster_exit_code, 0); // in-cluster service fails because it does not expect TLS
        assert_eq!(out_of_cluster_status, 404); // external service returns 404 as this path does not exist
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn routing_back_to_cluster_tcp_route() {
    with_temp_ns(|client, ns| async move {
        create(
            &client,
            k8s::policy::EgressNetwork {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("egress".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::EgressNetworkSpec {
                    traffic_policy: k8s::policy::TrafficPolicy::Allow,
                    networks: None,
                },
                status: None,
            },
        )
        .await;
        let status = await_egress_net_status(&client, &ns, "egress").await;
        assert_status_accepted(status.conditions);

        // Create the web pod and wait for it to be ready.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        // Now create an tls route that will route requests
        // to an in-cluster service based on SNI
        create(
            &client,
            k8s_gateway_api::TcpRoute {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("tcp-route".to_string()),
                    ..Default::default()
                },
                spec: k8s_gateway_api::TcpRouteSpec {
                    inner: k8s_gateway_api::CommonRouteSpec {
                        parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                            namespace: None,
                            name: "egress".to_string(),
                            port: Some(80),
                            group: Some("policy.linkerd.io".to_string()),
                            kind: Some("EgressNetwork".to_string()),
                            section_name: None,
                        }]),
                    },
                    rules: vec![k8s_gateway_api::TcpRouteRule {
                        backend_refs: vec![k8s::gateway::BackendRef {
                            weight: None,
                            inner: k8s_gateway_api::BackendObjectReference {
                                namespace: Some(ns.clone()),
                                name: "web".to_string(),
                                port: Some(80),
                                group: None,
                                kind: None,
                            },
                        }],
                    }],
                },
                status: None,
            },
        )
        .await;
        await_tcp_route_status(&client, &ns, "tcp-route").await;

        let curl = curl::Runner::init(&client, &ns).await;
        let in_cluster = curl
            .run(
                "curl-in-cluster",
                "http://postman-echo.com/get",
                LinkerdInject::Enabled,
            )
            .await;

        let in_cluster_status = in_cluster.http_status_code().await;

        assert_eq!(in_cluster_status, 204); // in-cluster service returns 204
    })
    .await;
}
