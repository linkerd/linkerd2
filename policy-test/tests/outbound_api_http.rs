use futures::StreamExt;
use linkerd2_proxy_api::{self as api, outbound};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy};
use linkerd_policy_test::{
    assert_resource_meta, await_route_accepted, create,
    outbound_api::{assert_route_is_default, assert_singleton, retry_watch_outbound_policy},
    test_route::{TestParent, TestRoute},
    with_temp_ns,
};

#[tokio::test(flavor = "current_thread")]
async fn gateway_http_route_with_filters_service() {
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

            let mut route = gateway::HttpRoute::make_route(
                ns,
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            for rule in route.spec.rules.iter_mut().flatten() {
                rule.filters = Some(vec![
                    gateway::HttpRouteFilter::RequestHeaderModifier {
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
                    },
                    gateway::HttpRouteFilter::RequestRedirect {
                        request_redirect: k8s_gateway_api::HttpRequestRedirectFilter {
                            scheme: Some("http".to_string()),
                            hostname: Some("host".to_string()),
                            path: Some(k8s_gateway_api::HttpPathModifier::ReplacePrefixMatch {
                                replace_prefix_match: "/path".to_string(),
                            }),
                            port: Some(5555),
                            status_code: Some(302),
                        },
                    },
                ]);
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
            gateway::HttpRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::HttpRoute::extract_meta(outbound_route)));
                let rule = assert_singleton(&outbound_route.rules);
                let filters = &rule.filters;
                assert_eq!(
                    *filters,
                    vec![
                outbound::http_route::Filter {
                    kind: Some(
                        outbound::http_route::filter::Kind::RequestHeaderModifier(
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
                        )
                    )
                },
                outbound::http_route::Filter {
                    kind: Some(outbound::http_route::filter::Kind::Redirect(
                        api::http_route::RequestRedirect {
                            scheme: Some(api::http_types::Scheme {
                                r#type: Some(api::http_types::scheme::Type::Registered(
                                    api::http_types::scheme::Registered::Http.into(),
                                ))
                            }),
                            host: "host".to_string(),
                            path: Some(linkerd2_proxy_api::http_route::PathModifier {
                                replace: Some(
                                    linkerd2_proxy_api::http_route::path_modifier::Replace::Prefix(
                                        "/path".to_string()
                                    )
                                )
                            }),
                            port: 5555,
                            status: 302,
                        }
                    ))
                }
            ]
                );
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn policy_http_route_with_filters_service() {
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

            let mut route = policy::HttpRoute::make_route(
                ns,
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            for rule in route.spec.rules.iter_mut().flatten() {
                rule.filters = Some(vec![
                    policy::httproute::HttpRouteFilter::RequestHeaderModifier {
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
                    },
                    policy::httproute::HttpRouteFilter::RequestRedirect {
                        request_redirect: k8s_gateway_api::HttpRequestRedirectFilter {
                            scheme: Some("http".to_string()),
                            hostname: Some("host".to_string()),
                            path: Some(k8s_gateway_api::HttpPathModifier::ReplacePrefixMatch {
                                replace_prefix_match: "/path".to_string(),
                            }),
                            port: Some(5555),
                            status_code: Some(302),
                        },
                    },
                ]);
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
            policy::HttpRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(policy::HttpRoute::extract_meta(outbound_route)));
                let rule = assert_singleton(&outbound_route.rules);
                let filters = &rule.filters;
                assert_eq!(
                    *filters,
                    vec![
                outbound::http_route::Filter {
                    kind: Some(
                        outbound::http_route::filter::Kind::RequestHeaderModifier(
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
                        )
                    )
                },
                outbound::http_route::Filter {
                    kind: Some(outbound::http_route::filter::Kind::Redirect(
                        api::http_route::RequestRedirect {
                            scheme: Some(api::http_types::Scheme {
                                r#type: Some(api::http_types::scheme::Type::Registered(
                                    api::http_types::scheme::Registered::Http.into(),
                                ))
                            }),
                            host: "host".to_string(),
                            path: Some(linkerd2_proxy_api::http_route::PathModifier {
                                replace: Some(
                                    linkerd2_proxy_api::http_route::path_modifier::Replace::Prefix(
                                        "/path".to_string()
                                    )
                                )
                            }),
                            port: 5555,
                            status: 302,
                        }
                    ))
                }
            ]
                );
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn gateway_http_route_with_backend_filters() {
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

            let mut route = gateway::HttpRoute::make_route(
                ns,
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            for rule in route.spec.rules.iter_mut().flatten() {
                for backend in rule.backend_refs.iter_mut().flatten() {
                    backend.filters = Some(vec![
                        gateway::HttpRouteFilter::RequestHeaderModifier {
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
                        },
                        gateway::HttpRouteFilter::RequestRedirect {
                            request_redirect: k8s_gateway_api::HttpRequestRedirectFilter {
                                scheme: Some("http".to_string()),
                                hostname: Some("host".to_string()),
                                path: Some(k8s_gateway_api::HttpPathModifier::ReplacePrefixMatch {
                                    replace_prefix_match: "/path".to_string(),
                                }),
                                port: Some(5555),
                                status_code: Some(302),
                            },
                        },
                    ]);
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
            gateway::HttpRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(gateway::HttpRoute::extract_meta(outbound_route)));
                let rules = gateway::HttpRoute::rules_random_available(outbound_route);
                let rule = assert_singleton(&rules);
                let backend = assert_singleton(rule);
                assert_eq!(
                    backend.filters,
                    vec![
                outbound::http_route::Filter {
                    kind: Some(
                        outbound::http_route::filter::Kind::RequestHeaderModifier(
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
                        )
                    )
                },
                outbound::http_route::Filter {
                    kind: Some(outbound::http_route::filter::Kind::Redirect(
                        api::http_route::RequestRedirect {
                            scheme: Some(api::http_types::Scheme {
                                r#type: Some(api::http_types::scheme::Type::Registered(
                                    api::http_types::scheme::Registered::Http.into(),
                                ))
                            }),
                            host: "host".to_string(),
                            path: Some(linkerd2_proxy_api::http_route::PathModifier {
                                replace: Some(
                                    linkerd2_proxy_api::http_route::path_modifier::Replace::Prefix(
                                        "/path".to_string()
                                    )
                                )
                            }),
                            port: 5555,
                            status: 302,
                        }
                    ))
                }
            ]
                );
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}

#[tokio::test(flavor = "current_thread")]
async fn policy_http_route_with_backend_filters() {
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

            let mut route = policy::HttpRoute::make_route(
                ns,
                vec![parent.obj_ref()],
                vec![vec![backend.backend_ref(backend_port)]],
            );
            for rule in route.spec.rules.iter_mut().flatten() {
                for backend in rule.backend_refs.iter_mut().flatten() {
                    backend.filters = Some(vec![
                        gateway::HttpRouteFilter::RequestHeaderModifier {
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
                        },
                        gateway::HttpRouteFilter::RequestRedirect {
                            request_redirect: k8s_gateway_api::HttpRequestRedirectFilter {
                                scheme: Some("http".to_string()),
                                hostname: Some("host".to_string()),
                                path: Some(k8s_gateway_api::HttpPathModifier::ReplacePrefixMatch {
                                    replace_prefix_match: "/path".to_string(),
                                }),
                                port: Some(5555),
                                status_code: Some(302),
                            },
                        },
                    ]);
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
            policy::HttpRoute::routes(&config, |routes| {
                let outbound_route = routes.first().expect("route must exist");
                assert!(route.meta_eq(policy::HttpRoute::extract_meta(outbound_route)));
                let rules = policy::HttpRoute::rules_random_available(outbound_route);
                let rule = assert_singleton(&rules);
                let backend = assert_singleton(rule);
                assert_eq!(
                    backend.filters,
                    vec![
                outbound::http_route::Filter {
                    kind: Some(
                        outbound::http_route::filter::Kind::RequestHeaderModifier(
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
                        )
                    )
                },
                outbound::http_route::Filter {
                    kind: Some(outbound::http_route::filter::Kind::Redirect(
                        api::http_route::RequestRedirect {
                            scheme: Some(api::http_types::Scheme {
                                r#type: Some(api::http_types::scheme::Type::Registered(
                                    api::http_types::scheme::Registered::Http.into(),
                                ))
                            }),
                            host: "host".to_string(),
                            path: Some(linkerd2_proxy_api::http_route::PathModifier {
                                replace: Some(
                                    linkerd2_proxy_api::http_route::path_modifier::Replace::Prefix(
                                        "/path".to_string()
                                    )
                                )
                            }),
                            port: 5555,
                            status: 302,
                        }
                    ))
                }
            ]
                );
            });
        })
        .await;
    }

    test::<k8s::Service>().await;
    test::<policy::EgressNetwork>().await;
}
