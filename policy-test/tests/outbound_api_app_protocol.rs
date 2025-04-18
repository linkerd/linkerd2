use futures::StreamExt;
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_controller_k8s_api::gateway;
use linkerd_policy_test::{
    assert_resource_meta, await_route_accepted, create,
    outbound_api::{
        assert_route_is_default, assert_singleton, grpc_routes, http1_routes, http2_routes,
        retry_watch_outbound_policy,
    },
    test_route::{TestParent, TestRoute},
    with_temp_ns,
};

#[cfg(feature = "gateway-api-experimental")]
#[tokio::test(flavor = "current_thread")]
async fn opaque_parent() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            let parent = create(
                &client,
                P::make_parent_with_protocol(&ns, Some("linkerd.io/opaque".to_string())),
            )
            .await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let routes = linkerd_policy_test::outbound_api::tcp_routes(&config);
            let route = assert_singleton(routes);
            assert_route_is_default::<gateway::TCPRoute>(route, &parent.obj_ref(), port);
        })
        .await;
    }

    test::<k8s::Service>().await;
}

#[cfg(feature = "gateway-api-experimental")]
#[tokio::test(flavor = "current_thread")]
async fn unknown_app_protocol_parent() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            let parent = create(
                &client,
                P::make_parent_with_protocol(&ns, Some("XMPP".to_string())),
            )
            .await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let routes = linkerd_policy_test::outbound_api::tcp_routes(&config);
            let route = assert_singleton(routes);
            assert_route_is_default::<gateway::TCPRoute>(route, &parent.obj_ref(), port);
        })
        .await;
    }

    test::<k8s::Service>().await;
}

#[cfg(feature = "gateway-api-experimental")]
#[tokio::test(flavor = "current_thread")]
async fn opaque_parent_with_tcp_route() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            let parent = create(
                &client,
                P::make_parent_with_protocol(&ns, Some("linkerd.io/opaque".to_string())),
            )
            .await;

            let route = create(
                &client,
                gateway::TCPRoute::make_route(
                    ns.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![parent.backend_ref(port)]],
                ),
            )
            .await;
            await_route_accepted(&client, &route).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            gateway::TCPRoute::routes(&config, |routes| {
                // Only the first TCPRoute should be returned in the config.
                assert!(route.meta_eq(gateway::TCPRoute::extract_meta(&routes[0])));
                assert_eq!(routes.len(), 1);
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn http1_parent() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            let parent = create(
                &client,
                P::make_parent_with_protocol(&ns, Some("http".to_string())),
            )
            .await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let routes = http1_routes(&config);
            let route = assert_singleton(routes);
            assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
        })
        .await;
    }

    test::<k8s::Service>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn http1_parent_with_http_route() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            let parent = create(
                &client,
                P::make_parent_with_protocol(&ns, Some("http".to_string())),
            )
            .await;

            let route = create(
                &client,
                gateway::HTTPRoute::make_route(
                    ns.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![parent.backend_ref(port)]],
                ),
            )
            .await;
            await_route_accepted(&client, &route).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let routes = http1_routes(&config);
            let outbound_route = assert_singleton(routes);
            assert!(route.meta_eq(gateway::HTTPRoute::extract_meta(outbound_route)));
        })
        .await;
    }

    test::<k8s::Service>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn http2_parent() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            let parent = create(
                &client,
                P::make_parent_with_protocol(&ns, Some("kubernetes.io/h2c".to_string())),
            )
            .await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let routes = http2_routes(&config);
            let route = assert_singleton(routes);
            assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
        })
        .await;
    }

    test::<k8s::Service>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn http2_parent_with_http_route() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            let parent = create(
                &client,
                P::make_parent_with_protocol(&ns, Some("kubernetes.io/h2c".to_string())),
            )
            .await;

            let route = create(
                &client,
                gateway::HTTPRoute::make_route(
                    ns.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![parent.backend_ref(port)]],
                ),
            )
            .await;
            await_route_accepted(&client, &route).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let routes = http2_routes(&config);
            let outbound_route = assert_singleton(routes);
            assert!(route.meta_eq(gateway::HTTPRoute::extract_meta(outbound_route)));
        })
        .await;
    }

    test::<k8s::Service>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn http2_parent_with_grpc_route() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            let parent = create(
                &client,
                P::make_parent_with_protocol(&ns, Some("kubernetes.io/h2c".to_string())),
            )
            .await;

            let route = create(
                &client,
                gateway::GRPCRoute::make_route(
                    ns.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![parent.backend_ref(port)]],
                ),
            )
            .await;
            await_route_accepted(&client, &route).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let routes = grpc_routes(&config);
            let outbound_route = assert_singleton(routes);
            assert!(route.meta_eq(gateway::GRPCRoute::extract_meta(outbound_route)));
        })
        .await;
    }

    test::<k8s::Service>().await;
}
