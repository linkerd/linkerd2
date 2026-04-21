#![cfg(feature = "gateway-api-experimental")]

use futures::StreamExt;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy};
use linkerd_policy_test::{
    assert_resource_meta, await_route_accepted, create,
    outbound_api::{assert_route_is_default, assert_singleton, retry_watch_outbound_policy},
    test_route::{TestParent, TestRoute},
    with_temp_ns,
};

#[tokio::test(flavor = "current_thread")]
async fn multiple_tcp_routes() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let port = 4191;
            let parent = create(&client, P::make_parent(&ns)).await;

            // Create a backend
            let backend_port = 8888;
            let backend = match P::make_backend(&ns) {
                Some(b) => create(&client, b).await,
                None => parent.clone(),
            };

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a default route.
            gateway::HTTPRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });

            // Routes should be returned in sorted order by creation timestamp then
            // name. To ensure that this test isn't timing dependant, routes should
            // be created in alphabetical order.
            let mut route_a = R::make_route(
                ns.clone(),
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            route_a.meta_mut().name = Some("a-route".to_string());
            let route_a = create(&client, route_a).await;
            await_route_accepted(&client, &route_a).await;

            // First route update.
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let mut route_b = R::make_route(
                ns.clone(),
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            route_b.meta_mut().name = Some("b-route".to_string());
            let route_b = create(&client, route_b).await;
            await_route_accepted(&client, &route_b).await;

            // Second route update.
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            R::routes(&config, |routes| {
                // Only the first TCPRoute should be returned in the config.
                assert!(route_a.meta_eq(R::extract_meta(&routes[0])));
                assert_eq!(routes.len(), 1);
            });
        })
        .await
    }

    test::<k8s::Service, gateway::TCPRoute>().await;
    test::<policy::EgressNetwork, gateway::TCPRoute>().await;
}
