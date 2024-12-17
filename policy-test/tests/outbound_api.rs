use futures::StreamExt;
use k8s_gateway_api::{self as gateway};
use linkerd_policy_controller_k8s_api::{self as k8s, policy, ResourceExt};
use linkerd_policy_test::{
    assert_resource_meta, assert_status_accepted, await_route_status, create, grpc,
    outbound_api::{
        assert_backend_matches_reference, assert_route_is_default, assert_singleton,
        retry_watch_outbound_policy,
    },
    test_route::{TestParent, TestRoute},
    with_temp_ns,
};

#[tokio::test(flavor = "current_thread")]
async fn parent_does_not_exist() {
    async fn test<P: TestParent + Send>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Some IP address in the cluster networks which we assume is not
            // used.
            let ip = "10.8.255.255";

            let mut policy_api = grpc::OutboundPolicyClient::port_forwarded(&client).await;
            let rsp: Result<tonic::Streaming<grpc::outbound::OutboundPolicy>, tonic::Status> =
                policy_api.watch_ip(&ns, ip, port).await;

            assert!(rsp.is_err());
            assert_eq!(rsp.err().unwrap().code(), tonic::Code::NotFound);
        })
        .await
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn parent_with_no_routes() {
    async fn test<P: TestParent, R: TestRoute>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            // Create a parent with no routes.
            // let parent = P::create_parent(&client.clone(), &ns).await;
            let parent = create(&client, P::make_parent(&ns)).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a default route.
            gateway::HttpRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HttpRoute>(route, &parent.obj_ref(), port);
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::HttpRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn http_route_with_no_rules() {
    async fn test<P: TestParent, R: TestRoute>() {
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let parent = create(&client, P::make_parent(&ns)).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a default route.
            gateway::HttpRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HttpRoute>(route, &parent.obj_ref(), port);
            });

            let route = R::create_route(&client, ns.clone(), vec![parent.obj_ref()], vec![]).await;
            let status = await_route_status(&client, &route).await;
            assert_status_accepted(status);

            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a route with no rules.
            R::routes(&config, |routes| {
                let outbound_route = assert_singleton(routes);
                let rules = &R::rules_first_available(outbound_route);
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
                assert!(rules.is_empty());
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::HttpRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn http_routes_without_backends() {
    async fn test<P: TestParent, R: TestRoute>() {
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let port = 4191;
            let parent = create(&client, P::make_parent(&ns)).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a default route.
            gateway::HttpRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HttpRoute>(route, &parent.obj_ref(), port);
            });

            // Create a route with one rule with no backends.
            let route =
                R::create_route(&client, ns.clone(), vec![parent.obj_ref()], vec![vec![]]).await;
            let status = await_route_status(&client, &route).await;
            assert_status_accepted(status);

            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a route with the logical backend.
            R::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                let rules = &R::rules_first_available(outbound_route);
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
                let backends = assert_singleton(rules);
                let backend = R::backend(*assert_singleton(backends));
                assert_backend_matches_reference(backend, &parent.obj_ref(), port);
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::HttpRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn routes_with_backend() {
    async fn test<P: TestParent, R: TestRoute>() {
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let port = 4191;
            let parent = create(&client, P::make_parent(&ns)).await;

            // Create a backend
            let backend_port = 8888;
            let backend = P::make_backend(&ns);
            create(&client, backend.clone()).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a default route.
            gateway::HttpRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HttpRoute>(route, &parent.obj_ref(), port);
            });

            let dt = Default::default();
            let route = R::create_route(
                &client,
                ns,
                vec![parent.obj_ref()],
                vec![vec![gateway::BackendRef {
                    weight: None,
                    inner: gateway::BackendObjectReference {
                        group: Some(P::group(&dt).to_string()),
                        kind: Some(P::kind(&dt).to_string()),
                        name: backend.name_unchecked(),
                        namespace: backend.namespace(),
                        port: Some(backend_port),
                    },
                }]],
            );
            create(&client, route.clone()).await;
            let status = await_route_status(&client, &route).await;
            assert_status_accepted(status);

            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a route with a backend with no filters.
            R::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                let rules = &R::rules_random_available(outbound_route);
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
                let backends = assert_singleton(rules);

                let filters = R::backend_filters(*assert_singleton(backends));
                assert!(filters.is_empty());

                let outbound_backend = R::backend(*assert_singleton(backends));
                assert_backend_matches_reference(
                    outbound_backend,
                    &backend.obj_ref(),
                    backend_port,
                );
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HttpRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GrpcRoute>().await;
    test::<k8s::Service, gateway::TlsRoute>().await;
    test::<k8s::Service, gateway::TcpRoute>().await;
    test::<policy::EgressNetwork, gateway::HttpRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::GrpcRoute>().await;
    test::<policy::EgressNetwork, gateway::TlsRoute>().await;
    test::<policy::EgressNetwork, gateway::TcpRoute>().await;
}
