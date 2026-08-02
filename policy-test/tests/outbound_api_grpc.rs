use futures::StreamExt;
use kube::Resource;
use linkerd2_proxy_api::{self as api, outbound};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy};
use linkerd_policy_test::{
    assert_resource_meta, await_route_accepted, create,
    outbound_api::{
        assert_default_penalty_estimator, assert_default_retry_after_cap, assert_route_is_default,
        assert_singleton, grpc_route_backend_load_bias_and_retry_after,
        retry_watch_outbound_policy,
    },
    test_route::{TestParent, TestRoute},
    with_temp_ns,
};
use maplit::btreemap;

#[tokio::test(flavor = "current_thread")]
async fn grpc_route_with_filters_service() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
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

            let mut route = gateway::GRPCRoute::make_route(
                ns,
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            for rule in route.spec.rules.iter_mut().flatten() {
                rule.filters = Some(vec![gateway::GRPCRouteRulesFilters {
                    request_header_modifier: Some(
                        gateway::GRPCRouteRulesFiltersRequestHeaderModifier {
                            set: Some(vec![
                                gateway::GRPCRouteRulesFiltersRequestHeaderModifierSet {
                                    name: "set".to_string(),
                                    value: "set-value".to_string(),
                                },
                            ]),
                            add: Some(vec![
                                gateway::GRPCRouteRulesFiltersRequestHeaderModifierAdd {
                                    name: "add".to_string(),
                                    value: "add-value".to_string(),
                                },
                            ]),
                            remove: Some(vec!["remove".to_string()]),
                        },
                    ),
                    ..Default::default()
                }]);
            }
            let route = create(&client, route).await;
            await_route_accepted(&client, &route).await;

            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a route with filters.
            gateway::GRPCRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::GRPCRoute::extract_meta(outbound_route)));
                let rule = assert_singleton(&outbound_route.rules);
                let filters = &rule.filters;
                assert_eq!(
                    *filters,
                    vec![outbound::grpc_route::Filter {
                        kind: Some(outbound::grpc_route::filter::Kind::RequestHeaderModifier(
                            api::http_route::RequestHeaderModifier {
                                add: Some(api::http_types::Headers {
                                    headers: vec![api::http_types::headers::Header {
                                        name: "add".to_string(),
                                        value: "add-value".into(),
                                    }]
                                }),
                                set: Some(api::http_types::Headers {
                                    headers: vec![api::http_types::headers::Header {
                                        name: "set".to_string(),
                                        value: "set-value".into(),
                                    }]
                                }),
                                remove: vec!["remove".to_string()],
                            }
                        ))
                    }]
                );
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn policy_grpc_route_with_backend_filters() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
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

            let mut route = gateway::GRPCRoute::make_route(
                ns,
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            for rule in route.spec.rules.iter_mut().flatten() {
                for backend in rule.backend_refs.iter_mut().flatten() {
                    backend.filters = Some(vec![gateway::GRPCRouteRulesBackendRefsFilters {
                        request_header_modifier: Some(
                            gateway::GRPCRouteRulesBackendRefsFiltersRequestHeaderModifier {
                                set: Some(vec![gateway::GRPCRouteRulesBackendRefsFiltersRequestHeaderModifierSet {
                                    name: "set".to_string(),
                                    value: "set-value".to_string(),
                                }]),
                                add: Some(vec![gateway::GRPCRouteRulesBackendRefsFiltersRequestHeaderModifierAdd {
                                    name: "add".to_string(),
                                    value: "add-value".to_string(),
                                }]),
                                remove: Some(vec!["remove".to_string()]),
                            },
                        ),
                        ..Default::default()
                    }]);
                }
            }
            let route = create(&client, route).await;
            await_route_accepted(&client, &route).await;

            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            // There should be a route with backend filters.
            gateway::GRPCRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::GRPCRoute::extract_meta(outbound_route)));
                let rules = gateway::GRPCRoute::rules_random_available(outbound_route);
                let rule = assert_singleton(&rules);
                let backend = assert_singleton(rule);
                assert_eq!(
                    backend.filters,
                    vec![outbound::grpc_route::Filter {
                        kind: Some(outbound::grpc_route::filter::Kind::RequestHeaderModifier(
                            api::http_route::RequestHeaderModifier {
                                add: Some(api::http_types::Headers {
                                    headers: vec![api::http_types::headers::Header {
                                        name: "add".to_string(),
                                        value: "add-value".into(),
                                    }]
                                }),
                                set: Some(api::http_types::Headers {
                                    headers: vec![api::http_types::headers::Header {
                                        name: "set".to_string(),
                                        value: "set-value".into(),
                                    }]
                                }),
                                remove: vec!["remove".to_string()],
                            }
                        ))
                    }]
                );
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn grpc_route_retries_and_timeouts() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
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

            let mut route = gateway::GRPCRoute::make_route(
                ns.clone(),
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            route.meta_mut().annotations = Some(btreemap! {
                "retry.linkerd.io/grpc".to_string() => "internal".to_string(),
                "timeout.linkerd.io/response".to_string() => "10s".to_string(),
            });
            let route = create(&client, route).await;
            await_route_accepted(&client, &route).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an updated config");
            tracing::trace!(?config);

            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            gateway::GRPCRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::GRPCRoute::extract_meta(outbound_route)));
                let rule = assert_singleton(&outbound_route.rules);
                let conditions = rule
                    .retry
                    .as_ref()
                    .expect("retry config expected")
                    .conditions
                    .as_ref()
                    .expect("retry conditions expected");
                assert!(conditions.internal);
                let timeout = rule
                    .timeouts
                    .as_ref()
                    .expect("timeouts expected")
                    .response
                    .as_ref()
                    .expect("response timeout expected");
                assert_eq!(timeout.seconds, 10);
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn parent_retries_and_timeouts() {
    async fn test<P: TestParent>() {
        tracing::debug!(
            parent = %P::kind(&P::DynamicType::default()),
        );
        with_temp_ns(|client, ns| async move {
            // Create a parent
            let mut parent = P::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "retry.linkerd.io/grpc".to_string() => "internal".to_string(),
                "timeout.linkerd.io/response".to_string() => "10s".to_string(),
            });
            let parent = create(&client, parent).await;
            let port = 4191;
            // Create a backend
            let backend_port = 8888;
            let backend = match P::make_backend(&ns) {
                Some(b) => create(&client, b).await,
                None => parent.clone(),
            };

            let mut route = gateway::GRPCRoute::make_route(
                ns.clone(),
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            route.meta_mut().annotations = Some(btreemap! {
                // Route annotations override the retry config specified on the parent.
                "timeout.linkerd.io/request".to_string() => "5s".to_string(),
            });
            let route = create(&client, route).await;
            await_route_accepted(&client, &route).await;

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);
            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            gateway::GRPCRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::GRPCRoute::extract_meta(outbound_route)));
                let rule = assert_singleton(&outbound_route.rules);

                // Retry config inherited from the service.
                let conditions = rule
                    .retry
                    .as_ref()
                    .expect("retry config expected")
                    .conditions
                    .as_ref()
                    .expect("retry conditions expected");
                assert!(conditions.internal);

                // Parent timeout config overridden by route timeout config.
                let timeouts = rule.timeouts.as_ref().expect("timeouts expected");
                assert_eq!(timeouts.response, None);
                let request_timeout = timeouts.request.as_ref().expect("request timeout expected");
                assert_eq!(request_timeout.seconds, 5);
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

// A service that defines gRPC routes sends traffic through its route backends,
// so the penalty estimator selected by the load-bias toggles must reach those
// route backend balancers rather than only the default backend.
#[tokio::test(flavor = "current_thread")]
async fn grpc_route_backend_inherits_load_bias() {
    async fn test<R: TestRoute<Route = outbound::GrpcRoute>>() {
        tracing::debug!(route = %R::kind(&R::DynamicType::default()));
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let backend_port = 8888;

            // The load-bias and Retry-After toggles are set on the parent Service.
            let mut parent = k8s::Service::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.linkerd.io/failure-accrual".to_string() => "consecutive".to_string(),
                "balancer.alpha.linkerd.io/penalize-failures".to_string() => "true".to_string(),
                "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after".to_string() => "true".to_string(),
            });
            let parent = create(&client, parent).await;

            let route = create(
                &client,
                R::make_route(
                    ns.clone(),
                    vec![parent.obj_ref()],
                    vec![vec![parent.backend_ref(backend_port)]],
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

            // The route's own Service backend balancer holds the penalty
            // estimator, not just the default backend.
            let (load_bias, retry_after) = grpc_route_backend_load_bias_and_retry_after(&config);
            let lb = load_bias.expect("route backend must carry the penalty estimator");
            assert_default_penalty_estimator(&lb);

            let ra = retry_after.expect("route backend must carry the Retry-After cap");
            assert_default_retry_after_cap(&ra);
        })
        .await;
    }

    test::<gateway::GRPCRoute>().await;
}

// Load bias is parent-scoped. An annotated parent lends its penalty estimator
// to a distinct, unannotated backend reached through one of its routes.
#[tokio::test(flavor = "current_thread")]
async fn grpc_penalize_failures_annotated_parent_unannotated_backend() {
    async fn test<R: TestRoute<Route = outbound::GrpcRoute>>() {
        tracing::debug!(route = %R::kind(&R::DynamicType::default()));
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let backend_port = 8888;

            let mut parent = k8s::Service::make_parent(&ns);
            parent.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/penalize-failures".to_string() => "true".to_string(),
            });
            let parent = create(&client, parent).await;

            let backend = create(
                &client,
                k8s::Service::make_backend(&ns).expect("Service must produce a backend"),
            )
            .await;

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

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);
            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let (load_bias, _) = grpc_route_backend_load_bias_and_retry_after(&config);
            let lb = load_bias.expect("backend must inherit the penalty estimator from the parent");
            assert_default_penalty_estimator(&lb);
        })
        .await;
    }

    test::<gateway::GRPCRoute>().await;
}

// The converse: a backend's own load-bias annotation has no effect under an
// unannotated parent, leaving the plain PeakEwma estimator in place.
#[tokio::test(flavor = "current_thread")]
async fn grpc_penalize_failures_unannotated_parent_annotated_backend() {
    async fn test<R: TestRoute<Route = outbound::GrpcRoute>>() {
        tracing::debug!(route = %R::kind(&R::DynamicType::default()));
        with_temp_ns(|client, ns| async move {
            let port = 4191;
            let backend_port = 8888;

            let parent = create(&client, k8s::Service::make_parent(&ns)).await;

            let mut backend =
                k8s::Service::make_backend(&ns).expect("Service must produce a backend");
            backend.meta_mut().annotations = Some(btreemap! {
                "balancer.alpha.linkerd.io/penalize-failures".to_string() => "true".to_string(),
            });
            let backend = create(&client, backend).await;

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

            let mut rx = retry_watch_outbound_policy(&client, &ns, parent.ip(), port).await;
            let config = rx
                .next()
                .await
                .expect("watch must not fail")
                .expect("watch must return an initial config");
            tracing::trace!(?config);
            assert_resource_meta(&config.metadata, parent.obj_ref(), port);

            let (load_bias, _) = grpc_route_backend_load_bias_and_retry_after(&config);
            assert!(
                load_bias.is_none(),
                "backend's own annotation must not select the penalty estimator under an unannotated parent"
            );
        })
        .await;
    }

    test::<gateway::GRPCRoute>().await;
}
