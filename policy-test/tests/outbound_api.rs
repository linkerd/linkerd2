use std::time::Duration;

use futures::prelude::*;
use kube::ResourceExt;
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{
    create, create_opaque_service, create_service, grpc, mk_service, with_temp_ns,
};
use tokio::time;

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

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc).await;
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
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_http_route_without_rules() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc).await;
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

        let _route = create(&client, mk_empty_http_route(&ns, "foo-route", &svc, 4191)).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

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

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc).await;
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

        let _route = create(&client, mk_http_route(&ns, "foo-route", &svc, 4191, None)).await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        // There should be a route with the logical backend.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            let backends = route_backends_random_available(route);
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

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc).await;
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
        let _backend_svc = create_service(&client, &ns, backend_name, 8888).await;
        let backends = [backend_name];
        let _route = create(
            &client,
            mk_http_route(&ns, "foo-route", &svc, 4191, Some(&backends)),
        )
        .await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        // There should be a route with a backend with no filters.
        detect_http_routes(&config, |routes| {
            let route = assert_singleton(routes);
            let backends = route_backends_random_available(route);
            let backend = assert_singleton(backends);
            let filters = &backend.backend.as_ref().unwrap().filters;
            assert_eq!(filters.len(), 0);
        });
    })
    .await;
}

// TODO: Test fails until handling of invalid backends is implemented.
#[tokio::test(flavor = "current_thread")]
async fn service_with_http_routes_with_invalid_backend() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc).await;
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

        let backends = ["invalid-backend"];
        let _route = create(
            &client,
            mk_http_route(&ns, "foo-route", &svc, 4191, Some(&backends)),
        )
        .await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

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

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc).await;
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

        // Routes should be returned in sorted order by creation timestamp then
        // name. To ensure that this test isn't timing dependant, routes should
        // be created in alphabetical order.
        let _a_route = create(&client, mk_http_route(&ns, "a-route", &svc, 4191, None)).await;

        // First route update.
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        let _b_route = create(&client, mk_http_route(&ns, "b-route", &svc, 4191, None)).await;

        // Second route update.
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        // There should be 2 routes, returned in order.
        detect_http_routes(&config, |routes| {
            assert_eq!(routes.len(), 2);
            assert_eq!(route_name(&routes.get(0).unwrap()), "a-route");
            assert_eq!(route_name(&routes.get(1).unwrap()), "b-route");
        });
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn opaque_service() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_opaque_service(&client, &ns, "my-svc", 4191).await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc).await;
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

/* Helpers */

async fn retry_watch_outbound_policy(
    client: &kube::Client,
    ns: &str,
    svc: &k8s::Service,
) -> tonic::Streaming<grpc::outbound::OutboundPolicy> {
    // Port-forward to the control plane and start watching the service's
    // outbound policy.
    let mut policy_api = grpc::OutboundPolicyClient::port_forwarded(client).await;
    loop {
        match policy_api.watch(ns, svc, 4191).await {
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

fn mk_http_route(
    ns: &str,
    name: &str,
    svc: &k8s::Service,
    port: u16,
    backends: Option<&[&str]>,
) -> k8s::policy::HttpRoute {
    use k8s::policy::httproute as api;
    let backend_refs = backends.map(|names| {
        names
            .iter()
            .map(|name| api::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
                        name: name.to_string(),
                        port: Some(8888),
                        group: None,
                        kind: None,
                        namespace: None,
                    },
                }),
                filters: None,
            })
            .collect()
    });
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
                backend_refs,
            }]),
        },
        status: None,
    }
}

fn mk_empty_http_route(
    ns: &str,
    name: &str,
    svc: &k8s::Service,
    port: u16,
) -> k8s::policy::HttpRoute {
    use k8s::policy::httproute as api;
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
    F: Fn(&[grpc::outbound::HttpRoute]) -> (),
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
        grpc::meta::metadata::Kind::Resource(grpc::meta::Resource {
            group: _,
            kind: _,
            ref name,
            namespace: _,
            section: _,
        }) => name,
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
    let backends = route_backends_random_available(route);
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
    backend: &grpc::outbound::http_route::WeightedRouteBackend,
    svc: &k8s::Service,
    port: u16,
) {
    let kind = backend
        .backend
        .as_ref()
        .unwrap()
        .backend
        .as_ref()
        .unwrap()
        .kind
        .as_ref()
        .unwrap();
    let dst = match kind {
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
}

#[track_caller]
fn assert_singleton<T>(ts: &[T]) -> &T {
    assert_eq!(ts.len(), 1);
    ts.get(0).unwrap()
}
