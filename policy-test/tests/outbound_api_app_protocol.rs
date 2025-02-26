use futures::StreamExt;
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_controller_k8s_api::gateway;
use linkerd_policy_test::{
    assert_resource_meta, create,
    outbound_api::{
        assert_route_is_default, assert_singleton, retry_watch_outbound_policy, tcp_routes,
    },
    test_route::TestParent,
    with_temp_ns,
};

#[tokio::test(flavor = "current_thread")]
async fn opaque_parent() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            // let parent = P::create_parent(&client.clone(), &ns).await;
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

            let routes = tcp_routes(&config);
            let route = assert_singleton(routes);
            assert_route_is_default::<gateway::TCPRoute>(route, &parent.obj_ref(), port);
        })
        .await;
    }

    test::<k8s::Service>().await;
}
