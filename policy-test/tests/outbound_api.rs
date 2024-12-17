use futures::StreamExt;
use k8s_gateway_api::{self as gateway};
use linkerd_policy_controller_k8s_api::{self as k8s, policy};
use linkerd_policy_test::{
    assert_resource_meta, assert_status_accepted, await_route_status, create,
    create_cluster_scoped, delete_cluster_scoped, grpc,
    outbound_api::{
        assert_backend_matches_reference, assert_route_is_default, assert_singleton,
        retry_watch_outbound_policy,
    },
    test_route::{TestParent, TestRoute},
    with_temp_ns,
};
use maplit::{btreemap, convert_args};
use tracing::debug_span;

#[tokio::test(flavor = "current_thread")]
async fn parent_does_not_exist() {
    async fn test<P: TestParent + Send>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
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
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
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
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
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

            let route = create(
                &client,
                R::make_route(ns.clone(), vec![parent.obj_ref()], vec![]),
            )
            .await;
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
                let outbound_route = routes.first().expect("route must exist");
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
        let _span = debug_span!(
            "test",
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        )
        .entered();
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
            let route = create(
                &client,
                R::make_route(ns.clone(), vec![parent.obj_ref()], vec![vec![]]),
            )
            .await;
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
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
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
            gateway::HttpRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HttpRoute>(route, &parent.obj_ref(), port);
            });

            let route = create(
                &client,
                R::make_route(
                    ns,
                    vec![parent.obj_ref()],
                    vec![vec![backend.backend_ref(backend_port)]],
                ),
            )
            .await;
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

#[tokio::test(flavor = "current_thread")]
async fn service_with_routes_with_cross_namespace_backend() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
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

            let backend_ns_name = format!("{}-backend", ns);
            let backend_ns = create_cluster_scoped(
                &client,
                k8s::Namespace {
                    metadata: k8s::ObjectMeta {
                        name: Some(backend_ns_name.clone()),
                        labels: Some(convert_args!(btreemap!(
                            "linkerd-policy-test" => std::thread::current().name().unwrap_or(""),
                        ))),
                        ..Default::default()
                    },
                    ..Default::default()
                },
            )
            .await;

            // Create a cross namespace backend
            let backend_port = 8888;
            let backend = match P::make_backend(&backend_ns_name) {
                Some(b) => create(&client, b).await,
                None => parent.clone(),
            };
            let route = create(
                &client,
                R::make_route(
                    ns,
                    vec![parent.obj_ref()],
                    vec![vec![backend.backend_ref(backend_port)]],
                ),
            )
            .await;
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

            delete_cluster_scoped(&client, backend_ns).await
        })
        .await
    }

    test::<k8s::Service, gateway::HttpRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GrpcRoute>().await;
    test::<k8s::Service, gateway::TlsRoute>().await;
    test::<k8s::Service, gateway::TcpRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn routes_with_invalid_backend() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
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

            let backend_port = 8888;
            let mut backend = match P::make_backend(&ns) {
                Some(b) => create(&client, b).await,
                None => parent.clone(),
            };
            backend.meta_mut().name = Some("invalid".to_string());
            let route = create(
                &client,
                R::make_route(
                    ns,
                    vec![parent.obj_ref()],
                    vec![vec![backend.backend_ref(backend_port)]],
                ),
            )
            .await;
            let status = await_route_status(&client, &route).await;
            assert_status_accepted(status);

            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a route with a backend with a failure filter.
            R::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                let rules = &R::rules_random_available(outbound_route);
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
                let backends = assert_singleton(rules);

                let filters = R::backend_filters(*assert_singleton(backends));
                let filter = assert_singleton(&filters);
                assert!(R::is_failure_filter(filter));

                let outbound_backend = R::backend(*assert_singleton(backends));
                assert_backend_matches_reference(
                    outbound_backend,
                    &backend.obj_ref(),
                    backend_port,
                );
            });
        })
        .await
    }

    test::<k8s::Service, gateway::HttpRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GrpcRoute>().await;
    //test::<k8s::Service, gateway::TlsRoute>().await; // TODO: No filters returned?
    // test::<k8s::Service, gateway::TcpRoute>().await; // TODO: No filters returned?
    test::<policy::EgressNetwork, gateway::HttpRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::GrpcRoute>().await;
    // test::<policy::EgressNetwork, gateway::TlsRoute>().await; // TODO: No filters returned?
    // test::<policy::EgressNetwork, gateway::TcpRoute>().await; // TODO: No filters returned?
}
