use futures::StreamExt;
use kube::Resource;
use linkerd2_proxy_api::{self as api, outbound};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy};
use linkerd_policy_test::{
    assert_resource_meta, await_route_accepted, create,
    outbound_api::{assert_route_is_default, assert_singleton, retry_watch_outbound_policy},
    test_route::{TestParent, TestRoute},
    with_temp_ns,
};
use maplit::btreemap;

#[tokio::test(flavor = "current_thread")]
async fn grpc_route_with_filters_service() {
    async fn test<P: TestParent>() {
        with_temp_ns(|client, ns| async move {
            tracing::debug!(
                parent = %P::kind(&P::DynamicType::default()),
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
            gateway::HttpRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HttpRoute>(route, &parent.obj_ref(), port);
            });

            let mut route = gateway::GrpcRoute::make_route(
                ns,
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            for rule in route.spec.rules.iter_mut().flatten() {
                rule.filters = Some(vec![gateway::GrpcRouteFilter::RequestHeaderModifier {
                    request_header_modifier: k8s_gateway_api::HttpRequestHeaderFilter {
                        set: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "set".to_string(),
                            value: "set-value".to_string(),
                        }]),
                        add: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "add".to_string(),
                            value: "add-value".to_string(),
                        }]),
                        remove: Some(vec!["remove".to_string()]),
                    },
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
            gateway::GrpcRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::GrpcRoute::extract_meta(outbound_route)));
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
        with_temp_ns(|client, ns| async move {
            tracing::debug!(
                parent = %P::kind(&P::DynamicType::default()),
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
            gateway::HttpRoute::routes(&config, |routes| {
                let route = assert_singleton(routes);
                assert_route_is_default::<gateway::HttpRoute>(route, &parent.obj_ref(), port);
            });

            let mut route = gateway::GrpcRoute::make_route(
                ns,
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            for rule in route.spec.rules.iter_mut().flatten() {
                for backend in rule.backend_refs.iter_mut().flatten() {
                    backend.filters = Some(vec![gateway::GrpcRouteFilter::RequestHeaderModifier {
                        request_header_modifier: gateway::HttpRequestHeaderFilter {
                            set: Some(vec![k8s_gateway_api::HttpHeader {
                                name: "set".to_string(),
                                value: "set-value".to_string(),
                            }]),
                            add: Some(vec![k8s_gateway_api::HttpHeader {
                                name: "add".to_string(),
                                value: "add-value".to_string(),
                            }]),
                            remove: Some(vec!["remove".to_string()]),
                        },
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
            gateway::GrpcRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::GrpcRoute::extract_meta(outbound_route)));
                let rules = gateway::GrpcRoute::rules_random_available(outbound_route);
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
        with_temp_ns(|client, ns| async move {
            tracing::debug!(
                parent = %P::kind(&P::DynamicType::default()),
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

            let mut route = gateway::GrpcRoute::make_route(
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

            gateway::GrpcRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::GrpcRoute::extract_meta(outbound_route)));
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
        with_temp_ns(|client, ns| async move {
            tracing::debug!(
                parent = %P::kind(&P::DynamicType::default()),
            );
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

            let mut route = gateway::GrpcRoute::make_route(
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

            gateway::GrpcRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::GrpcRoute::extract_meta(outbound_route)));
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

// #[tokio::test(flavor = "current_thread")]
// async fn egress_net_grpc_route_reattachment() {
//     with_temp_ns(|client, ns| async move {
//         // Create a egress network
//         let egress = create_egress_network(&client, &ns, "my-egress").await;
//         let status = await_egress_net_status(&client, &ns, "my-egress").await;
//         assert_status_accepted(status.conditions);

//         grpc_route_reattachment(Resource::EgressNetwork(egress), &client, &ns).await;
//     })
//     .await;
// }

// /* Helpers */
// struct GrpcRouteBuilder(k8s_gateway_api::GrpcRoute);

// fn mk_grpc_route(ns: &str, name: &str, parent: &Resource, port: Option<u16>) -> GrpcRouteBuilder {
//     GrpcRouteBuilder(k8s_gateway_api::GrpcRoute {
//         metadata: kube::api::ObjectMeta {
//             namespace: Some(ns.to_string()),
//             name: Some(name.to_string()),
//             ..Default::default()
//         },
//         spec: k8s_gateway_api::GrpcRouteSpec {
//             inner: k8s_gateway_api::CommonRouteSpec {
//                 parent_refs: Some(vec![k8s_gateway_api::ParentReference {
//                     group: Some(parent.group()),
//                     kind: Some(parent.kind()),
//                     namespace: Some(parent.namespace()),
//                     name: parent.name(),
//                     section_name: None,
//                     port,
//                 }]),
//             },
//             hostnames: None,
//             rules: Some(vec![k8s_gateway_api::GrpcRouteRule {
//                 matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
//                     method: Some(k8s_gateway_api::GrpcMethodMatch::Exact {
//                         method: Some("foo".to_string()),
//                         service: Some("my-gprc-service".to_string()),
//                     }),
//                     headers: None,
//                 }]),
//                 filters: None,
//                 backend_refs: None,
//             }]),
//         },
//         status: None,
//     })
// }

// impl GrpcRouteBuilder {
//     fn with_annotations(self, annotations: BTreeMap<String, String>) -> Self {
//         let mut route = self.0;
//         route.metadata.annotations = Some(annotations);
//         Self(route)
//     }

//     fn build(self) -> k8s_gateway_api::GrpcRoute {
//         self.0
//     }
// }

// async fn grpc_route_reattachment(parent: Resource, client: &kube::Client, ns: &str) {
//     let mut route = create(
//         client,
//         mk_grpc_route(ns, "foo-route", &parent, Some(4191)).build(),
//     )
//     .await;
//     await_grpc_route_status(client, ns, "foo-route").await;

//     let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;
//     let config = rx
//         .next()
//         .await
//         .expect("watch must not fail")
//         .expect("watch must return an initial config");
//     tracing::trace!(?config);

//     assert_resource_meta(&config.metadata, &parent, 4191);

//     {
//         // The route should be attached.
//         let routes = grpc_routes(&config);
//         let route = assert_route_attached(routes, &parent);
//         assert_name_eq(route.metadata.as_ref().unwrap(), "foo-route");
//     }

//     route
//         .spec
//         .inner
//         .parent_refs
//         .as_mut()
//         .unwrap()
//         .first_mut()
//         .unwrap()
//         .name = "other".to_string();
//     update(client, route.clone()).await;

//     let config = rx
//         .next()
//         .await
//         .expect("watch must not fail")
//         .expect("watch must return an updated config");
//     tracing::trace!(?config);

//     assert_resource_meta(&config.metadata, &parent, 4191);

//     // The grpc route should be unattached and the default (http) route
//     // should be present.
//     detect_http_routes(&config, |routes| {
//         let route = assert_singleton(routes);
//         assert_route_is_default(route, &parent, 4191);
//     });

//     route
//         .spec
//         .inner
//         .parent_refs
//         .as_mut()
//         .unwrap()
//         .first_mut()
//         .unwrap()
//         .name = parent.name();
//     update(client, route).await;

//     let config = rx
//         .next()
//         .await
//         .expect("watch must not fail")
//         .expect("watch must return an updated config");
//     tracing::trace!(?config);

//     assert_resource_meta(&config.metadata, &parent, 4191);

//     // The route should be attached again.
//     {
//         // The route should be attached.
//         let routes = grpc_routes(&config);
//         let route = assert_route_attached(routes, &parent);
//         assert_name_eq(route.metadata.as_ref().unwrap(), "foo-route");
//     }
// }

// async fn grpc_route_retries_and_timeouts(parent: Resource, client: &kube::Client, ns: &str) {
//     let _route = create(
//         client,
//         mk_grpc_route(ns, "foo-route", &parent, Some(4191))
//             .with_annotations(
//                 vec![
//                     ("retry.linkerd.io/grpc".to_string(), "internal".to_string()),
//                     ("timeout.linkerd.io/response".to_string(), "10s".to_string()),
//                 ]
//                 .into_iter()
//                 .collect(),
//             )
//             .build(),
//     )
//     .await;
//     await_grpc_route_status(client, ns, "foo-route").await;

//     let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;
//     let config = rx
//         .next()
//         .await
//         .expect("watch must not fail")
//         .expect("watch must return an initial config");
//     tracing::trace!(?config);

//     let routes = grpc_routes(&config);
//     let route = assert_route_attached(routes, &parent);
//     let rule = assert_singleton(&route.rules);
//     let conditions = rule
//         .retry
//         .as_ref()
//         .expect("retry config expected")
//         .conditions
//         .as_ref()
//         .expect("retry conditions expected");
//     assert!(conditions.internal);
//     let timeout = rule
//         .timeouts
//         .as_ref()
//         .expect("timeouts expected")
//         .response
//         .as_ref()
//         .expect("response timeout expected");
//     assert_eq!(timeout.seconds, 10);
// }

// async fn parent_retries_and_timeouts(parent: Resource, client: &kube::Client, ns: &str) {
//     let _route = create(
//         client,
//         mk_grpc_route(ns, "foo-route", &parent, Some(4191))
//             .with_annotations(
//                 vec![
//                     // Route annotations override the timeout config specified
//                     // on the service.
//                     ("timeout.linkerd.io/request".to_string(), "5s".to_string()),
//                 ]
//                 .into_iter()
//                 .collect(),
//             )
//             .build(),
//     )
//     .await;
//     await_grpc_route_status(client, ns, "foo-route").await;

//     let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;
//     let config = rx
//         .next()
//         .await
//         .expect("watch must not fail")
//         .expect("watch must return an initial config");
//     tracing::trace!(?config);

//     let routes = grpc_routes(&config);
//     let route = assert_route_attached(routes, &parent);
//     let rule = assert_singleton(&route.rules);
//     let conditions = rule
//         .retry
//         .as_ref()
//         .expect("retry config expected")
//         .conditions
//         .as_ref()
//         .expect("retry conditions expected");
//     // Retry config inherited from the service.
//     assert!(conditions.internal);
//     let timeouts = rule.timeouts.as_ref().expect("timeouts expected");
//     // Parent timeout config overridden by route timeout config.
//     assert_eq!(timeouts.response, None);
//     let request_timeout = timeouts.request.as_ref().expect("request timeout expected");
//     assert_eq!(request_timeout.seconds, 5);
// }
