use futures::prelude::*;
use kube::ResourceExt;
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{
    assert_default_accrual_backoff, assert_svc_meta, create, create_annotated_service,
    create_cluster_scoped, create_opaque_service, create_service, delete_cluster_scoped, grpc,
    mk_service, with_temp_ns,
};
use maplit::{btreemap, convert_args};
use std::{collections::BTreeMap, time::Duration};
use tokio::time;

// These tests are copies of the tests in outbound_api_gateway.rs but using the
// policy.linkerd.io HttpRoute kubernetes types instead of the Gateway API ones.
// These two files should be kept in sync to ensure that Linkerd can read and
// function correctly with both types of resources.

#[tokio::test(flavor = "current_thread")]
async fn service_does_not_exist() {
    with_temp_ns(|client, ns| async move {
        // Build a service but don't apply it to the cluster.
        let mut svc = mk_service(&ns, "my-svc", 4191);
        // Give it a bogus cluster ip.
        svc.spec.as_mut().unwrap().cluster_ip = Some("1.1.1.1".to_string());

        let mut policy_api = grpc::OutboundPolicyClient::port_forwarded(&client).await;
        let rsp = policy_api.watch(&ns, &svc, 4191).await;

        assert!(rsp.is_err());
        assert_eq!(rsp.err().unwrap().code(), tonic::Code::NotFound);
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_no_http_routes() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_http_route_without_rules() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        let _route = create(&client, mk_empty_http_route(&ns, "foo-route", &svc, 4191)).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a route with no rules.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_eq!(route.rules.len(), 0);
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_http_routes_without_backends() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        let _route = create(
            &client,
            mk_http_route(&ns, "foo-route", &svc, Some(4191)).build(),
        )
        .await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a route with the logical backend.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            let backends = route_backends_first_available(route);
            let backend = assert_singleton(backends);
            assert_backend_matches_service(backend, &svc, 4191);
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_http_routes_with_backend() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        let backend_name = "backend";
        let backend_svc = create_service(&client, &ns, backend_name, 8888).await;
        let backends = [backend_name];
        let route = mk_http_route(&ns, "foo-route", &svc, Some(4191)).with_backends(
            Some(&backends),
            None,
            None,
        );
        let _route = create(&client, route.build()).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a route with a backend with no filters.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            let backends = route_backends_random_available(route);
            let backend = assert_singleton(backends);
            assert_backend_matches_service(backend.backend.as_ref().unwrap(), &backend_svc, 8888);
            let filters = &backend.backend.as_ref().unwrap().filters;
            assert_eq!(filters.len(), 0);
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_http_routes_with_cross_namespace_backend() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
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
        let backend_name = "backend";
        let backend_svc = create_service(&client, &backend_ns_name, backend_name, 8888).await;
        let backends = [backend_name];
        let route = mk_http_route(&ns, "foo-route", &svc, Some(4191)).with_backends(
            Some(&backends),
            Some(backend_ns_name),
            None,
        );
        let _route = create(&client, route.build()).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a route with a backend with no filters.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            let backends = route_backends_random_available(route);
            let backend = assert_singleton(backends);
            assert_backend_matches_service(backend.backend.as_ref().unwrap(), &backend_svc, 8888);
            let filters = &backend.backend.as_ref().unwrap().filters;
            assert_eq!(filters.len(), 0);
        });

        delete_cluster_scoped(&client, backend_ns).await
    })
    .await;
}

// TODO: Test fails until handling of invalid backends is implemented.
#[tokio::test(flavor = "current_thread")]
async fn service_with_http_routes_with_invalid_backend() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        let backends = ["invalid-backend"];
        let route = mk_http_route(&ns, "foo-route", &svc, Some(4191)).with_backends(
            Some(&backends),
            None,
            None,
        );
        let _route = create(&client, route.build()).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a route with a backend.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            let backends = route_backends_random_available(route);
            let backend = assert_singleton(backends);
            assert_backend_has_failure_filter(backend);
        });
    })
    .await;
}

// TODO: Investigate why the policy controller is only returning one route in this
// case instead of two.
#[tokio::test(flavor = "current_thread")]
async fn service_with_multiple_http_routes() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        // Routes should be returned in sorted order by creation timestamp then
        // name. To ensure that this test isn't timing dependant, routes should
        // be created in alphabetical order.
        let _a_route = create(
            &client,
            mk_http_route(&ns, "a-route", &svc, Some(4191)).build(),
        )
        .await;

        // First route update.
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        let _b_route = create(
            &client,
            mk_http_route(&ns, "b-route", &svc, Some(4191)).build(),
        )
        .await;

        // Second route update.
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        assert_svc_meta(&config.metadata, &svc, 4191);

        // There should be 2 routes, returned in order.
        detect_http_routes(&config, |routes| {
            assert_eq!(routes.len(), 2);
            assert_eq!(route_name(&routes[0]), "a-route");
            assert_eq!(route_name(&routes[1]), "b-route");
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_consecutive_failure_accrual() {
    with_temp_ns(|client, ns| async move {
        let svc = create_annotated_service(
            &client,
            &ns,
            "consecutive-accrual-svc",
            80,
            BTreeMap::from([
                (
                    "balancer.linkerd.io/failure-accrual".to_string(),
                    "consecutive".to_string(),
                ),
                (
                    "balancer.linkerd.io/failure-accrual-consecutive-max-failures".to_string(),
                    "8".to_string(),
                ),
                (
                    "balancer.linkerd.io/failure-accrual-consecutive-min-penalty".to_string(),
                    "10s".to_string(),
                ),
                (
                    "balancer.linkerd.io/failure-accrual-consecutive-max-penalty".to_string(),
                    "10m".to_string(),
                ),
                (
                    "balancer.linkerd.io/failure-accrual-consecutive-jitter-ratio".to_string(),
                    "1.0".to_string(),
                ),
            ]),
        )
        .await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        detect_failure_accrual(&config, |accrual| {
            let consecutive = failure_accrual_consecutive(accrual);
            assert_eq!(8, consecutive.max_failures);
            assert_eq!(
                &grpc::outbound::ExponentialBackoff {
                    min_backoff: Some(Duration::from_secs(10).try_into().unwrap()),
                    max_backoff: Some(Duration::from_secs(600).try_into().unwrap()),
                    jitter_ratio: 1.0_f32,
                },
                consecutive
                    .backoff
                    .as_ref()
                    .expect("backoff must be configured")
            );
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_consecutive_failure_accrual_defaults() {
    with_temp_ns(|client, ns| async move {
        // Create a service configured to do consecutive failure accrual, but
        // with no additional configuration
        let svc = create_annotated_service(
            &client,
            &ns,
            "default-accrual-svc",
            80,
            BTreeMap::from([(
                "balancer.linkerd.io/failure-accrual".to_string(),
                "consecutive".to_string(),
            )]),
        )
        .await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // Expect default max_failures and default backoff
        detect_failure_accrual(&config, |accrual| {
            let consecutive = failure_accrual_consecutive(accrual);
            assert_eq!(7, consecutive.max_failures);
            assert_default_accrual_backoff!(consecutive
                .backoff
                .as_ref()
                .expect("backoff must be configured"));
        });

        // Create a service configured to do consecutive failure accrual with
        // max number of failures and with default backoff
        let svc = create_annotated_service(
            &client,
            &ns,
            "no-backoff-svc",
            80,
            BTreeMap::from([
                (
                    "balancer.linkerd.io/failure-accrual".to_string(),
                    "consecutive".to_string(),
                ),
                (
                    "balancer.linkerd.io/failure-accrual-consecutive-max-failures".to_string(),
                    "8".to_string(),
                ),
            ]),
        )
        .await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // Expect default backoff and overridden max_failures
        detect_failure_accrual(&config, |accrual| {
            let consecutive = failure_accrual_consecutive(accrual);
            assert_eq!(8, consecutive.max_failures);
            assert_default_accrual_backoff!(consecutive
                .backoff
                .as_ref()
                .expect("backoff must be configured"));
        });

        // Create a service configured to do consecutive failure accrual with
        // only the jitter ratio configured in the backoff
        let svc = create_annotated_service(
            &client,
            &ns,
            "only-jitter-svc",
            80,
            BTreeMap::from([
                (
                    "balancer.linkerd.io/failure-accrual".to_string(),
                    "consecutive".to_string(),
                ),
                (
                    "balancer.linkerd.io/failure-accrual-consecutive-jitter-ratio".to_string(),
                    "1.0".to_string(),
                ),
            ]),
        )
        .await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // Expect defaults for everything except for the jitter ratio
        detect_failure_accrual(&config, |accrual| {
            let consecutive = failure_accrual_consecutive(accrual);
            assert_eq!(7, consecutive.max_failures);
            assert_eq!(
                &grpc::outbound::ExponentialBackoff {
                    min_backoff: Some(Duration::from_secs(1).try_into().unwrap()),
                    max_backoff: Some(Duration::from_secs(60).try_into().unwrap()),
                    jitter_ratio: 1.0_f32,
                },
                consecutive
                    .backoff
                    .as_ref()
                    .expect("backoff must be configured")
            );
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_default_failure_accrual() {
    with_temp_ns(|client, ns| async move {
        // Default config for Service, no failure accrual
        let svc = create_service(&client, &ns, "default-failure-accrual", 80).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // Expect failure accrual config to be default (no failure accrual)
        detect_failure_accrual(&config, |accrual| {
            assert!(
                accrual.is_none(),
                "consecutive failure accrual should not be configured for service"
            );
        });

        // Create Service with consecutive failure accrual config for
        // max_failures but no mode
        let svc = create_annotated_service(
            &client,
            &ns,
            "default-max-failure-svc",
            80,
            BTreeMap::from([(
                "balancer.linkerd.io/failure-accrual-consecutive-max-failures".to_string(),
                "8".to_string(),
            )]),
        )
        .await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // Expect failure accrual config to be default (no failure accrual)
        detect_failure_accrual(&config, |accrual| {
            assert!(
                accrual.is_none(),
                "consecutive failure accrual should not be configured for service"
            )
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn opaque_service() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_opaque_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // Proxy protocol should be opaque.
        match config.protocol.unwrap().kind.unwrap() {
            grpc::outbound::proxy_protocol::Kind::Opaque(_) => {}
            _ => panic!("proxy protocol must be Opaque"),
        };
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn route_with_filters() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        let backend_name = "backend";
        let backends = [backend_name];
        let route = mk_http_route(&ns, "foo-route", &svc, Some(4191))
            .with_backends(Some(&backends), None, None)
            .with_filters(Some(vec![
                k8s_gateway_api::HttpRouteFilter::RequestHeaderModifier {
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
                k8s_gateway_api::HttpRouteFilter::RequestRedirect {
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
            ]));
        let _route = create(&client, route.build()).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        // There should be a route with filters.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            let rule = assert_singleton(&route.rules);
            let filters = &rule.filters;
            assert_eq!(
                *filters,
                vec![
                    grpc::outbound::http_route::Filter {
                        kind: Some(
                            grpc::outbound::http_route::filter::Kind::RequestHeaderModifier(
                                grpc::http_route::RequestHeaderModifier {
                                    add: Some(grpc::http_types::Headers {
                                        headers: vec![grpc::http_types::headers::Header {
                                            name: "add".to_string(),
                                            value: "add-value".into(),
                                        }]
                                    }),
                                    set: Some(grpc::http_types::Headers {
                                        headers: vec![grpc::http_types::headers::Header {
                                            name: "set".to_string(),
                                            value: "set-value".into(),
                                        }]
                                    }),
                                    remove: vec!["remove".to_string()],
                                }
                            )
                        )
                    },
                    grpc::outbound::http_route::Filter {
                        kind: Some(grpc::outbound::http_route::filter::Kind::Redirect(
                            grpc::http_route::RequestRedirect {
                                scheme: Some(grpc::http_types::Scheme {
                                    r#type: Some(grpc::http_types::scheme::Type::Registered(
                                        grpc::http_types::scheme::Registered::Http.into(),
                                    ))
                                }),
                                host: "host".to_string(),
                                path: Some(linkerd2_proxy_api::http_route::PathModifier { replace: Some(linkerd2_proxy_api::http_route::path_modifier::Replace::Prefix("/path".to_string())) }),
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

#[tokio::test(flavor = "current_thread")]
async fn backend_with_filters() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);

        // There should be a default route.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        let backend_name = "backend";
        let backend_svc = create_service(&client, &ns, backend_name, 8888).await;
        let backends = [backend_name];
        let route = mk_http_route(&ns, "foo-route", &svc, Some(4191))
            .with_backends(Some(&backends), None, Some(vec![
                k8s_gateway_api::HttpRouteFilter::RequestHeaderModifier {
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
                k8s_gateway_api::HttpRouteFilter::RequestRedirect {
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
            ]));
        let _route = create(&client, route.build()).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        // There should be a route without rule filters.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            let rule = assert_singleton(&route.rules);
            assert_eq!(rule.filters.len(), 0);
            let backends = route_backends_random_available(route);
            let backend = assert_singleton(backends);
            assert_backend_matches_service(backend.backend.as_ref().unwrap(), &backend_svc, 8888);
            let filters = &backend.backend.as_ref().unwrap().filters;
            assert_eq!(
                *filters,
                vec![
                    grpc::outbound::http_route::Filter {
                        kind: Some(
                            grpc::outbound::http_route::filter::Kind::RequestHeaderModifier(
                                grpc::http_route::RequestHeaderModifier {
                                    add: Some(grpc::http_types::Headers {
                                        headers: vec![grpc::http_types::headers::Header {
                                            name: "add".to_string(),
                                            value: "add-value".into(),
                                        }]
                                    }),
                                    set: Some(grpc::http_types::Headers {
                                        headers: vec![grpc::http_types::headers::Header {
                                            name: "set".to_string(),
                                            value: "set-value".into(),
                                        }]
                                    }),
                                    remove: vec!["remove".to_string()],
                                }
                            )
                        )
                    },
                    grpc::outbound::http_route::Filter {
                        kind: Some(grpc::outbound::http_route::filter::Kind::Redirect(
                            grpc::http_route::RequestRedirect {
                                scheme: Some(grpc::http_types::Scheme {
                                    r#type: Some(grpc::http_types::scheme::Type::Registered(
                                        grpc::http_types::scheme::Registered::Http.into(),
                                    ))
                                }),
                                host: "host".to_string(),
                                path: Some(linkerd2_proxy_api::http_route::PathModifier { replace: Some(linkerd2_proxy_api::http_route::path_modifier::Replace::Prefix("/path".to_string())) }),
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

#[tokio::test(flavor = "current_thread")]
async fn http_route_with_no_port() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx_4191 = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config_4191 = rx_4191
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config_4191);

        let mut rx_9999 = retry_watch_outbound_policy(&client, &ns, &svc, 9999).await;
        let config_9999 = rx_9999
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config_9999);

        // There should be a default route.
        detect_http_routes(&config_4191, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });
        detect_http_routes(&config_9999, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 9999);
        });

        let _route = create(&client, mk_http_route(&ns, "foo-route", &svc, None).build()).await;

        let config_4191 = rx_4191
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config_4191);

        // The route should apply to the service.
        detect_http_routes(&config_4191, |routes| {
            let route = assert_singleton(routes);
            assert_route_name_eq(route, "foo-route");
        });

        let config_9999 = rx_9999
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config_9999);

        // The route should apply to other ports too.
        detect_http_routes(&config_9999, |routes| {
            let route = assert_singleton(routes);
            assert_route_name_eq(route, "foo-route");
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn producer_route() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut producer_rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let producer_config = producer_rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?producer_config);

        let mut consumer_rx = retry_watch_outbound_policy(&client, "consumer_ns", &svc, 4191).await;
        let consumer_config = consumer_rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?consumer_config);

        // There should be a default route.
        detect_http_routes(&producer_config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });
        detect_http_routes(&consumer_config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        // A route created in the same namespace as its parent service is called
        // a producer route. It should be returned in outbound policy requests
        // for that service from ALL namespaces.
        let _route = create(
            &client,
            mk_http_route(&ns, "foo-route", &svc, Some(4191)).build(),
        )
        .await;

        let producer_config = producer_rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?producer_config);
        let consumer_config = consumer_rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?consumer_config);

        // The route should be returned in queries from the producer namespace.
        detect_http_routes(&producer_config, |routes| {
            let route = assert_singleton(routes);
            assert_route_name_eq(route, "foo-route");
        });

        // The route should be returned in queries from a consumer namespace.
        detect_http_routes(&consumer_config, |routes| {
            let route = assert_singleton(routes);
            assert_route_name_eq(route, "foo-route");
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn consumer_route() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

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

        let mut producer_rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let producer_config = producer_rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?producer_config);

        let mut consumer_rx =
            retry_watch_outbound_policy(&client, &consumer_ns_name, &svc, 4191).await;
        let consumer_config = consumer_rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?consumer_config);

        let mut other_rx = retry_watch_outbound_policy(&client, "other_ns", &svc, 4191).await;
        let other_config = other_rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?other_config);

        // There should be a default route.
        detect_http_routes(&producer_config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });
        detect_http_routes(&consumer_config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });
        detect_http_routes(&other_config, |routes| {
            let route = assert_singleton(routes);
            assert_route_is_default(route, &svc, 4191);
        });

        // A route created in a different namespace as its parent service is
        // called a consumer route. It should be returned in outbound policy
        // requests for that service ONLY when the request comes from the
        // consumer namespace.
        let _route = create(
            &client,
            mk_http_route(&consumer_ns_name, "foo-route", &svc, Some(4191)).build(),
        )
        .await;

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

        detect_http_routes(&consumer_config, |routes| {
            let route = assert_singleton(routes);
            assert_route_name_eq(route, "foo-route");
        });

        // The route should NOT be returned in queries from a different consumer
        // namespace.
        assert!(other_rx.next().now_or_never().is_none());

        delete_cluster_scoped(&client, consumer_ns).await;
    })
    .await;
}

/* Helpers */

struct HttpRouteBuilder(k8s_gateway_api::HttpRoute);

async fn retry_watch_outbound_policy(
    client: &kube::Client,
    ns: &str,
    svc: &k8s::Service,
    port: u16,
) -> tonic::Streaming<grpc::outbound::OutboundPolicy> {
    // Port-forward to the control plane and start watching the service's
    // outbound policy.
    let mut policy_api = grpc::OutboundPolicyClient::port_forwarded(client).await;
    loop {
        match policy_api.watch(ns, svc, port).await {
            Ok(rx) => return rx,
            Err(error) => {
                tracing::error!(
                    ?error,
                    ns,
                    svc = svc.name_unchecked(),
                    "failed to watch outbound policy for port 4191"
                );
                time::sleep(Duration::from_secs(1)).await;
            }
        }
    }
}

fn mk_http_route(ns: &str, name: &str, svc: &k8s::Service, port: Option<u16>) -> HttpRouteBuilder {
    use k8s_gateway_api as api;

    HttpRouteBuilder(api::HttpRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: api::HttpRouteSpec {
            inner: api::CommonRouteSpec {
                parent_refs: Some(vec![api::ParentReference {
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    namespace: svc.namespace(),
                    name: svc.name_unchecked(),
                    section_name: None,
                    port,
                }]),
            },
            hostnames: None,
            rules: Some(vec![api::HttpRouteRule {
                matches: Some(vec![api::HttpRouteMatch {
                    path: Some(api::HttpPathMatch::Exact {
                        value: "/foo".to_string(),
                    }),
                    headers: None,
                    query_params: None,
                    method: Some("GET".to_string()),
                }]),
                filters: None,
                backend_refs: None,
            }]),
        },
        status: None,
    })
}

impl HttpRouteBuilder {
    fn with_backends(
        self,
        backends: Option<&[&str]>,
        backends_ns: Option<String>,
        backend_filters: Option<Vec<k8s_gateway_api::HttpRouteFilter>>,
    ) -> Self {
        let mut route = self.0;
        let backend_refs = backends.map(|names| {
            names
                .iter()
                .map(|name| k8s_gateway_api::HttpBackendRef {
                    backend_ref: Some(k8s_gateway_api::BackendRef {
                        weight: None,
                        inner: k8s_gateway_api::BackendObjectReference {
                            name: name.to_string(),
                            port: Some(8888),
                            group: None,
                            kind: None,
                            namespace: backends_ns.clone(),
                        },
                    }),
                    filters: backend_filters.clone(),
                })
                .collect()
        });
        route.spec.rules.iter_mut().flatten().for_each(|rule| {
            rule.backend_refs = backend_refs.clone();
        });
        Self(route)
    }

    fn with_filters(self, filters: Option<Vec<k8s_gateway_api::HttpRouteFilter>>) -> Self {
        let mut route = self.0;
        route
            .spec
            .rules
            .iter_mut()
            .flatten()
            .for_each(|rule| rule.filters = filters.clone());
        Self(route)
    }

    fn build(self) -> k8s_gateway_api::HttpRoute {
        self.0
    }
}

fn mk_empty_http_route(
    ns: &str,
    name: &str,
    svc: &k8s::Service,
    port: u16,
) -> k8s_gateway_api::HttpRoute {
    use k8s_gateway_api as api;
    api::HttpRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: api::HttpRouteSpec {
            inner: api::CommonRouteSpec {
                parent_refs: Some(vec![api::ParentReference {
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    namespace: svc.namespace(),
                    name: svc.name_unchecked(),
                    section_name: None,
                    port: Some(port),
                }]),
            },
            hostnames: None,
            rules: Some(vec![]),
        },
        status: None,
    }
}

// detect_http_routes asserts that the given outbound policy has a proxy protcol
// of "Detect" and then invokes the given function with the Http1 and Http2
// routes from the Detect.
#[track_caller]
fn detect_http_routes<F>(config: &grpc::outbound::OutboundPolicy, f: F)
where
    F: Fn(&[grpc::outbound::HttpRoute]),
{
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Detect(grpc::outbound::proxy_protocol::Detect {
        opaque: _,
        timeout: _,
        http1,
        http2,
    }) = kind
    {
        let http1 = http1
            .as_ref()
            .expect("proxy protocol must have http1 field");
        let http2 = http2
            .as_ref()
            .expect("proxy protocol must have http2 field");
        f(&http1.routes);
        f(&http2.routes);
    } else {
        panic!("proxy protocol must be Detect; actually got:\n{kind:#?}")
    }
}

#[track_caller]
fn detect_failure_accrual<F>(config: &grpc::outbound::OutboundPolicy, f: F)
where
    F: Fn(Option<&grpc::outbound::FailureAccrual>),
{
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Detect(grpc::outbound::proxy_protocol::Detect {
        opaque: _,
        timeout: _,
        http1,
        http2,
    }) = kind
    {
        let http1 = http1
            .as_ref()
            .expect("proxy protocol must have http1 field");
        let http2 = http2
            .as_ref()
            .expect("proxy protocol must have http2 field");
        f(http1.failure_accrual.as_ref());
        f(http2.failure_accrual.as_ref());
    } else {
        panic!("proxy protocol must be Detect; actually got:\n{kind:#?}")
    }
}

#[track_caller]
fn failure_accrual_consecutive(
    accrual: Option<&grpc::outbound::FailureAccrual>,
) -> &grpc::outbound::failure_accrual::ConsecutiveFailures {
    assert!(
        accrual.is_some(),
        "failure accrual must be configured for service"
    );
    let kind = accrual
        .unwrap()
        .kind
        .as_ref()
        .expect("failure accrual must have kind");
    let grpc::outbound::failure_accrual::Kind::ConsecutiveFailures(accrual) = kind;
    accrual
}

#[track_caller]
fn route_backends_first_available(
    route: &grpc::outbound::HttpRoute,
) -> &[grpc::outbound::http_route::RouteBackend] {
    let kind = assert_singleton(&route.rules)
        .backends
        .as_ref()
        .expect("Rule must have backends")
        .kind
        .as_ref()
        .expect("Backend must have kind");
    match kind {
        grpc::outbound::http_route::distribution::Kind::FirstAvailable(fa) => &fa.backends,
        _ => panic!("Distribution must be FirstAvailable"),
    }
}

#[track_caller]
fn route_backends_random_available(
    route: &grpc::outbound::HttpRoute,
) -> &[grpc::outbound::http_route::WeightedRouteBackend] {
    let kind = assert_singleton(&route.rules)
        .backends
        .as_ref()
        .expect("Rule must have backends")
        .kind
        .as_ref()
        .expect("Backend must have kind");
    match kind {
        grpc::outbound::http_route::distribution::Kind::RandomAvailable(dist) => &dist.backends,
        _ => panic!("Distribution must be RandomAvailable"),
    }
}

#[track_caller]
fn route_name(route: &grpc::outbound::HttpRoute) -> &str {
    match route.metadata.as_ref().unwrap().kind.as_ref().unwrap() {
        grpc::meta::metadata::Kind::Resource(grpc::meta::Resource { ref name, .. }) => name,
        _ => panic!("route must be a resource kind"),
    }
}

#[track_caller]
fn assert_backend_has_failure_filter(backend: &grpc::outbound::http_route::WeightedRouteBackend) {
    let filter = assert_singleton(&backend.backend.as_ref().unwrap().filters);
    match filter.kind.as_ref().unwrap() {
        grpc::outbound::http_route::filter::Kind::FailureInjector(_) => {}
        _ => panic!("backend must have FailureInjector filter"),
    };
}

#[track_caller]
fn assert_route_is_default(route: &grpc::outbound::HttpRoute, svc: &k8s::Service, port: u16) {
    let kind = route.metadata.as_ref().unwrap().kind.as_ref().unwrap();
    match kind {
        grpc::meta::metadata::Kind::Default(_) => {}
        grpc::meta::metadata::Kind::Resource(r) => {
            panic!("route expected to be default but got resource {r:?}")
        }
    }

    let backends = route_backends_first_available(route);
    let backend = assert_singleton(backends);
    assert_backend_matches_service(backend, svc, port);

    let rule = assert_singleton(&route.rules);
    let route_match = assert_singleton(&rule.matches);
    let path_match = route_match.path.as_ref().unwrap().kind.as_ref().unwrap();
    assert_eq!(
        *path_match,
        grpc::http_route::path_match::Kind::Prefix("/".to_string())
    );
}

#[track_caller]
fn assert_backend_matches_service(
    backend: &grpc::outbound::http_route::RouteBackend,
    svc: &k8s::Service,
    port: u16,
) {
    let backend = backend.backend.as_ref().unwrap();
    let dst = match backend.kind.as_ref().unwrap() {
        grpc::outbound::backend::Kind::Balancer(balance) => {
            let kind = balance.discovery.as_ref().unwrap().kind.as_ref().unwrap();
            match kind {
                grpc::outbound::backend::endpoint_discovery::Kind::Dst(dst) => &dst.path,
            }
        }
        grpc::outbound::backend::Kind::Forward(_) => {
            panic!("default route backend must be Balancer")
        }
    };
    assert_eq!(
        *dst,
        format!(
            "{}.{}.svc.{}:{}",
            svc.name_unchecked(),
            svc.namespace().unwrap(),
            "cluster.local",
            port
        )
    );

    assert_svc_meta(&backend.metadata, svc, port)
}

#[track_caller]
fn assert_singleton<T>(ts: &[T]) -> &T {
    assert_eq!(ts.len(), 1);
    ts.first().unwrap()
}

#[track_caller]
fn assert_route_name_eq(route: &grpc::outbound::HttpRoute, name: &str) {
    let kind = route.metadata.as_ref().unwrap().kind.as_ref().unwrap();
    match kind {
        grpc::meta::metadata::Kind::Default(d) => {
            panic!("route expected to not be default, but got default {d:?}")
        }
        grpc::meta::metadata::Kind::Resource(resource) => assert_eq!(resource.name, *name),
    }
}
