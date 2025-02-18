use futures::{FutureExt, StreamExt};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy};
use linkerd_policy_test::{
    assert_resource_meta, await_route_accepted, create, create_cluster_scoped,
    delete_cluster_scoped, grpc,
    outbound_api::{
        assert_backend_matches_reference, assert_route_is_default, assert_singleton,
        retry_watch_outbound_policy,
    },
    test_route::{TestParent, TestRoute},
    update, with_temp_ns,
};
use maplit::{btreemap, convert_args};

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
            gateway::HTTPRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<policy::EgressNetwork, gateway::HTTPRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn route_with_no_rules() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
        with_temp_ns(|client, ns| async move {
            tracing::debug!(
                parent = %P::kind(&P::DynamicType::default()),
                route = %R::kind(&R::DynamicType::default()),
            );
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
            gateway::HTTPRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });

            let route = create(
                &client,
                R::make_route(ns.clone(), vec![parent.obj_ref()], vec![]),
            )
            .await;
            await_route_accepted(&client, &route).await;

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

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::HTTPRoute>().await;
    test::<policy::EgressNetwork, gateway::GRPCRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn routes_without_backends() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default()),
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
            gateway::HTTPRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });

            // Create a route with one rule with no backends.
            let route = create(
                &client,
                R::make_route(ns.clone(), vec![parent.obj_ref()], vec![vec![]]),
            )
            .await;
            await_route_accepted(&client, &route).await;

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

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::HTTPRoute>().await;
    test::<policy::EgressNetwork, gateway::GRPCRoute>().await;
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
            gateway::HTTPRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
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
            await_route_accepted(&client, &route).await;

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

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<k8s::Service, gateway::TCPRoute>().await;
    test::<policy::EgressNetwork, gateway::HTTPRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::GRPCRoute>().await;
    test::<policy::EgressNetwork, gateway::TLSRoute>().await;
    test::<policy::EgressNetwork, gateway::TCPRoute>().await;
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
            gateway::HTTPRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
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
            await_route_accepted(&client, &route).await;

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

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<k8s::Service, gateway::TCPRoute>().await;
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
            gateway::HTTPRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
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
            await_route_accepted(&client, &route).await;

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

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<k8s::Service, gateway::TCPRoute>().await;
    test::<policy::EgressNetwork, gateway::HTTPRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::GRPCRoute>().await;
    test::<policy::EgressNetwork, gateway::TLSRoute>().await;
    test::<policy::EgressNetwork, gateway::TCPRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn multiple_routes() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
        with_temp_ns(|client, ns| async move {
            tracing::debug!(
                parent = %P::kind(&P::DynamicType::default()),
                route = %R::kind(&R::DynamicType::default()),
            );
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
                assert!(route_a.meta_eq(R::extract_meta(&routes[0])));
                assert!(route_b.meta_eq(R::extract_meta(&routes[1])));
            });
        })
        .await
    }

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<policy::EgressNetwork, gateway::HTTPRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::GRPCRoute>().await;
    test::<policy::EgressNetwork, gateway::TLSRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn opaque_service() {
    async fn test<P: TestParent + Send>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            tracing::debug!(
                parent = %P::kind(&P::DynamicType::default()),
            );
            // Create a parent
            let port = 4191;
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "config.linkerd.io/opaque-ports".to_string() => port.to_string(),
            });
            let parent = create(&client, parent).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // Proxy protocol should be opaque.
            match config.protocol.unwrap().kind.unwrap() {
                grpc::outbound::proxy_protocol::Kind::Opaque(_) => {}
                _ => panic!("proxy protocol must be Opaque"),
            };
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn route_with_no_port() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let parent = create(&client, P::make_parent(&ns)).await;
            // Create a backend
            let backend_port = 8888;
            let backend = match P::make_backend(&ns) {
                Some(b) => create(&client, b).await,
                None => parent.clone(),
            };

            let port_a = 4191;
            let port_b = 9999;

            let mut rx_a = retry_watch_outbound_policy(&client, &ns, parent.ip(), port_a).await;
            let config_a = rx_a
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config_a);

            let mut rx_b = retry_watch_outbound_policy(&client, &ns, parent.ip(), port_b).await;
            let config_b = rx_b
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config_b);

            // There should be a default route.
            gateway::HTTPRoute::routes(&config_a, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port_a);
            });
            gateway::HTTPRoute::routes(&config_b, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port_b);
            });

            // Create a route with no port in the parent_ref.
            let mut parent_ref = parent.obj_ref();
            parent_ref.port = None;
            let route = create(
                &client,
                R::make_route(
                    ns.clone(),
                    vec![parent_ref],
                    vec![vec![backend.backend_ref(backend_port)]],
                ),
            )
            .await;
            await_route_accepted(&client, &route).await;

            let config_a = rx_a
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config_a);
            assert_resource_meta(&config_a.metadata, parent.obj_ref(), port_a);

            let config_b = rx_b
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config_b);
            assert_resource_meta(&config_b.metadata, parent.obj_ref(), port_b);

            // The route should apply to both ports.
            R::routes(&config_a, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });
            R::routes(&config_b, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<k8s::Service, gateway::TCPRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn producer_route() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let parent = create(&client, P::make_parent(&ns)).await;
            let port = 4191;
            // Create a backend
            let backend_port = 8888;
            let backend = match P::make_backend(&ns) {
                Some(b) => create(&client, b).await,
                None => parent.clone(),
            };

            let mut producer_rx =
                retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let producer_config = producer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?producer_config);
            assert_resource_meta(&producer_config.metadata, parent.obj_ref(), port);

            let mut consumer_rx =
                retry_watch_outbound_policy(&client, "consumer_ns", parent.ip(), port).await;
            let consumer_config = consumer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?consumer_config);
            assert_resource_meta(&consumer_config.metadata, parent.obj_ref(), port);

            // There should be a default route.
            gateway::HTTPRoute::routes(&producer_config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });
            gateway::HTTPRoute::routes(&consumer_config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });

            // A route created in the same namespace as its parent service is called
            // a producer route. It should be returned in outbound policy requests
            // for that service from ALL namespaces.
            let route = create(
                &client,
                R::make_route(
                    ns.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![backend.backend_ref(backend_port)]],
                ),
            )
            .await;
            await_route_accepted(&client, &route).await;

            let producer_config = producer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?producer_config);
            assert_resource_meta(&producer_config.metadata, parent.obj_ref(), port);

            let consumer_config = consumer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?consumer_config);
            assert_resource_meta(&consumer_config.metadata, parent.obj_ref(), port);

            // The route should be returned in queries from the producer namespace.
            R::routes(&producer_config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });
            // The route should be returned in queries from a consumer namespace.
            R::routes(&consumer_config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<k8s::Service, gateway::TCPRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn pre_existing_producer_route() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default())
        );
        // We test the scenario where outbound policy watches are initiated after
        // a produce route already exists.
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let parent = create(&client, P::make_parent(&ns)).await;
            let port = 4191;
            // Create a backend
            let backend_port = 8888;
            let backend = match P::make_backend(&ns) {
                Some(b) => create(&client, b).await,
                None => parent.clone(),
            };

            // A route created in the same namespace as its parent service is called
            // a producer route. It should be returned in outbound policy requests
            // for that service from ALL namespaces.
            let route = create(
                &client,
                R::make_route(
                    ns.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![backend.backend_ref(backend_port)]],
                ),
            )
            .await;
            await_route_accepted(&client, &route).await;

            let mut producer_rx =
                retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let producer_config = producer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?producer_config);
            assert_resource_meta(&producer_config.metadata, parent.obj_ref(), port);

            let mut consumer_rx =
                retry_watch_outbound_policy(&client, "consumer_ns", parent.ip(), port).await;
            let consumer_config = consumer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?consumer_config);
            assert_resource_meta(&consumer_config.metadata, parent.obj_ref(), port);

            // The route should be returned in queries from the producer namespace.
            R::routes(&producer_config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });
            // The route should be returned in queries from a consumer namespace.
            R::routes(&consumer_config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<k8s::Service, gateway::TCPRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn consumer_route() {
    async fn test<P: TestParent, R: TestRoute>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
            route = %R::kind(&R::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let parent = create(&client, P::make_parent(&ns)).await;
            let port = 4191;
            // Create a backend
            let backend_port = 8888;
            let backend = match P::make_backend(&ns) {
                Some(b) => create(&client, b).await,
                None => parent.clone(),
            };

            let consumer_ns_name = format!("{}-consumer", ns);
            let consumer_ns = create_cluster_scoped(
                &client,
                k8s::Namespace {
                    metadata: k8s::ObjectMeta {
                        name: Some(consumer_ns_name.clone()),
                        labels: Some(convert_args!(btreemap!(
                            "linkerd-policy-test" => std::thread::current().name().unwrap_or(""),
                        ))),
                        ..Default::default()
                    },
                    ..Default::default()
                },
            )
            .await;

            let mut producer_rx =
                retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let producer_config = producer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?producer_config);
            assert_resource_meta(&producer_config.metadata, parent.obj_ref(), port);

            let mut consumer_rx =
                retry_watch_outbound_policy(&client, &consumer_ns_name, parent.ip(), port).await;
            let consumer_config = consumer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?consumer_config);
            assert_resource_meta(&consumer_config.metadata, parent.obj_ref(), port);

            let mut other_rx =
                retry_watch_outbound_policy(&client, "other_ns", parent.ip(), port).await;
            let other_config = other_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?other_config);
            assert_resource_meta(&other_config.metadata, parent.obj_ref(), port);

            // There should be a default route.
            gateway::HTTPRoute::routes(&producer_config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });
            gateway::HTTPRoute::routes(&consumer_config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });
            gateway::HTTPRoute::routes(&other_config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });

            // A route created in a different namespace as its parent service is
            // called a consumer route. It should be returned in outbound policy
            // requests for that service ONLY when the request comes from the
            // consumer namespace.
            let route = create(
                &client,
                R::make_route(
                    consumer_ns_name.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![backend.backend_ref(backend_port)]],
                ),
            )
            .await;
            await_route_accepted(&client, &route).await;

            // The route should NOT be returned in queries from the producer namespace.
            // There should be a default route.
            assert!(producer_rx.next().now_or_never().is_none());

            // The route should be returned in queries from the same consumer
            // namespace.
            let consumer_config = consumer_rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?consumer_config);
            assert_resource_meta(&consumer_config.metadata, parent.obj_ref(), port);

            R::routes(&consumer_config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });

            // The route should NOT be returned in queries from a different consumer
            // namespace.
            assert!(other_rx.next().now_or_never().is_none());

            delete_cluster_scoped(&client, consumer_ns).await;
        })
        .await;
    }

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<k8s::Service, gateway::TCPRoute>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn route_reattachment() {
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

            let mut route = create(
                &client,
                R::make_route(
                    ns.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![backend.backend_ref(backend_port)]],
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

            // The route should be attached.
            R::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });

            // Detatch route.
            route.set_parent_name("other".to_string());
            update(&client, route.clone()).await;

            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // The route should be unattached and the default route should be present.
            gateway::HTTPRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HTTPRoute>(route, &parent.obj_ref(), port);
            });

            // Reattach route.
            route.set_parent_name(parent.meta().name.clone().unwrap());
            update(&client, route.clone()).await;

            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // The route should be attached again.
            R::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(R::extract_meta(outbound_route)));
            });
        })
        .await;
    }

    test::<k8s::Service, gateway::HTTPRoute>().await;
    test::<k8s::Service, policy::HttpRoute>().await;
    test::<k8s::Service, gateway::GRPCRoute>().await;
    test::<k8s::Service, gateway::TLSRoute>().await;
    test::<k8s::Service, gateway::TCPRoute>().await;
    test::<policy::EgressNetwork, gateway::HTTPRoute>().await;
    test::<policy::EgressNetwork, policy::HttpRoute>().await;
    test::<policy::EgressNetwork, gateway::GRPCRoute>().await;
    test::<policy::EgressNetwork, gateway::TLSRoute>().await;
    test::<policy::EgressNetwork, gateway::TCPRoute>().await;
}
