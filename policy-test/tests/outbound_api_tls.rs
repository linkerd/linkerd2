use futures::prelude::*;
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{
    assert_resource_meta, assert_status_accepted, await_egress_net_status, await_tls_route_status,
    create, create_cluster_scoped, create_egress_network, create_service, delete_cluster_scoped,
    grpc, mk_egress_net, mk_service, outbound_api::*, update, with_temp_ns, Resource,
};
use maplit::{btreemap, convert_args};

#[tokio::test(flavor = "current_thread")]
async fn service_with_tls_routes_with_backend() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;
        let backend_svc = create_service(&client, &ns, "backend", 8888).await;
        parent_with_tls_routes_with_backend(
            Resource::Service(svc),
            Resource::Service(backend_svc),
            &client,
            &ns,
        )
        .await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_net_with_tls_routes_with_backend() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let egress = create_egress_network(&client, &ns, "my-egress").await;
        let status = await_egress_net_status(&client, &ns, "my-egress").await;
        assert_status_accepted(status.conditions);

        parent_with_tls_routes_with_backend(
            Resource::EgressNetwork(egress.clone()),
            Resource::EgressNetwork(egress),
            &client,
            &ns,
        )
        .await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_tls_routes_with_cross_namespace_backend() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = Resource::Service(create_service(&client, &ns, "my-svc", 4191).await);

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
        let backend_svc =
            Resource::Service(create_service(&client, &backend_ns_name, backend_name, 8888).await);
        let backends = [backend_svc.clone()];
        let route = mk_tls_route(&ns, "foo-route", &svc, Some(4191)).with_backends(&backends);
        let _route = create(&client, route.build()).await;
        await_tls_route_status(&client, &ns, "foo-route").await;

        let mut rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);

        assert_resource_meta(&config.metadata, &svc, 4191);

        let routes = tls_routes(&config);
        let route = assert_singleton(routes);
        let backends = tls_route_backends_random_available(route);
        let backend = assert_singleton(backends);
        assert_tls_backend_matches_parent(backend.backend.as_ref().unwrap(), &backend_svc, 8888);

        delete_cluster_scoped(&client, backend_ns).await
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_tls_routes_with_invalid_backend() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;
        let backend = mk_service(&ns, "invalid", 4191);

        parent_with_tls_routes_with_invalid_backend(
            Resource::Service(svc),
            Resource::Service(backend),
            &client,
            &ns,
        )
        .await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_net_with_tls_routes_with_invalid_backend() {
    with_temp_ns(|client, ns| async move {
        // Create an egress network
        let egress = create_egress_network(&client, &ns, "my-egress").await;
        let status = await_egress_net_status(&client, &ns, "my-egress").await;
        assert_status_accepted(status.conditions);

        let backend = mk_egress_net(&ns, "invalid");

        parent_with_tls_routes_with_invalid_backend(
            Resource::EgressNetwork(egress),
            Resource::EgressNetwork(backend),
            &client,
            &ns,
        )
        .await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_with_multiple_tls_routes() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;
        parent_with_multiple_tls_routes(Resource::Service(svc), &client, &ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_net_with_multiple_http_routes() {
    with_temp_ns(|client, ns| async move {
        // Create an egress net
        let egress = create_egress_network(&client, &ns, "my-egress").await;
        let status = await_egress_net_status(&client, &ns, "my-egress").await;
        assert_status_accepted(status.conditions);

        parent_with_multiple_tls_routes(Resource::EgressNetwork(egress), &client, &ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn tls_route_with_no_port() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = Resource::Service(create_service(&client, &ns, "my-svc", 4191).await);

        let _route = create(
            &client,
            mk_tls_route(&ns, "foo-route", &svc, None)
                .with_backends(&[svc.clone()])
                .build(),
        )
        .await;
        await_tls_route_status(&client, &ns, "foo-route").await;

        let mut rx_4191 = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;
        let mut rx_9999 = retry_watch_outbound_policy(&client, &ns, &svc, 9999).await;

        let config_4191 = rx_4191
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config_4191);

        let routes = tls_routes(&config_4191);
        let route = assert_singleton(routes);
        assert_tls_route_name_eq(route, "foo-route");

        let config_9999 = rx_9999
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config_9999);

        let routes = tls_routes(&config_9999);
        let route = assert_singleton(routes);
        assert_tls_route_name_eq(route, "foo-route");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn producer_route() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = Resource::Service(create_service(&client, &ns, "my-svc", 4191).await);

        // A route created in the same namespace as its parent service is called
        // a producer route. It should be returned in outbound policy requests
        // for that service from ALL namespaces.
        let _route = create(
            &client,
            mk_tls_route(&ns, "foo-route", &svc, Some(4191))
                .with_backends(&[svc.clone()])
                .build(),
        )
        .await;
        await_tls_route_status(&client, &ns, "foo-route").await;

        let mut consumer_rx = retry_watch_outbound_policy(&client, "consumer_ns", &svc, 4191).await;
        let mut producer_rx = retry_watch_outbound_policy(&client, &ns, &svc, 4191).await;

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

        let routes = tls_routes(&producer_config);
        let route = assert_singleton(routes);
        assert_tls_route_name_eq(route, "foo-route");

        let routes = tls_routes(&consumer_config);
        let route = assert_singleton(routes);
        assert_tls_route_name_eq(route, "foo-route");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn pre_existing_producer_route() {
    // We test the scenario where outbound policy watches are initiated after
    // a produce route already exists.
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = Resource::Service(create_service(&client, &ns, "my-svc", 4191).await);

        // A route created in the same namespace as its parent service is called
        // a producer route. It should be returned in outbound policy requests
        // for that service from ALL namespaces.
        let _route = create(
            &client,
            mk_tls_route(&ns, "foo-route", &svc, Some(4191))
                .with_backends(&[svc.clone()])
                .build(),
        )
        .await;
        await_tls_route_status(&client, &ns, "foo-route").await;

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

        // The route should be returned in queries from the producer namespace.
        let routes = tls_routes(&producer_config);
        let route = assert_singleton(routes);
        assert_tls_route_name_eq(route, "foo-route");

        // The route should be returned in queries from a consumer namespace.
        let routes = tls_routes(&consumer_config);
        let route = assert_singleton(routes);
        assert_tls_route_name_eq(route, "foo-route");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn consumer_route() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = Resource::Service(create_service(&client, &ns, "my-svc", 4191).await);

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

        // A route created in a different namespace as its parent service is
        // called a consumer route. It should be returned in outbound policy
        // requests for that service ONLY when the request comes from the
        // consumer namespace.
        let _route = create(
            &client,
            mk_tls_route(&consumer_ns_name, "foo-route", &svc, Some(4191))
                .with_backends(&[svc])
                .build(),
        )
        .await;
        await_tls_route_status(&client, &consumer_ns_name, "foo-route").await;

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

        let routes = tls_routes(&consumer_config);
        let route = assert_singleton(routes);
        assert_tls_route_name_eq(route, "foo-route");

        // The route should NOT be returned in queries from a different consumer
        // namespace.
        assert!(other_rx.next().now_or_never().is_none());

        delete_cluster_scoped(&client, consumer_ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_tls_route_reattachment() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;
        tls_route_reattachment(Resource::Service(svc), &client, &ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_net_tls_route_reattachment() {
    with_temp_ns(|client, ns| async move {
        // Create a egress net
        let egress = create_egress_network(&client, &ns, "my-egress").await;
        let status = await_egress_net_status(&client, &ns, "my-egress").await;
        assert_status_accepted(status.conditions);

        tls_route_reattachment(Resource::EgressNetwork(egress), &client, &ns).await;
    })
    .await;
}

/* Helpers */

struct TlsRouteBuilder(k8s_gateway_api::TlsRoute);

fn mk_tls_route(ns: &str, name: &str, parent: &Resource, port: Option<u16>) -> TlsRouteBuilder {
    use k8s_gateway_api as api;

    TlsRouteBuilder(api::TlsRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: api::TlsRouteSpec {
            inner: api::CommonRouteSpec {
                parent_refs: Some(vec![api::ParentReference {
                    group: Some(parent.group()),
                    kind: Some(parent.kind()),
                    namespace: Some(parent.namespace()),
                    name: parent.name(),
                    section_name: None,
                    port,
                }]),
            },
            hostnames: None,
            rules: vec![api::TlsRouteRule {
                backend_refs: Vec::default(),
            }],
        },
        status: None,
    })
}

impl TlsRouteBuilder {
    fn with_backends(self, backends: &[Resource]) -> Self {
        let mut route = self.0;
        let backend_refs: Vec<_> = backends
            .iter()
            .map(|backend| k8s_gateway_api::BackendRef {
                weight: None,
                inner: k8s_gateway_api::BackendObjectReference {
                    name: backend.name(),
                    port: Some(8888),
                    group: Some(backend.group()),
                    kind: Some(backend.kind()),
                    namespace: Some(backend.namespace()),
                },
            })
            .collect();
        route.spec.rules.iter_mut().for_each(|rule| {
            rule.backend_refs = backend_refs.clone();
        });
        Self(route)
    }

    fn build(self) -> k8s_gateway_api::TlsRoute {
        self.0
    }
}

async fn parent_with_tls_routes_with_backend(
    parent: Resource,
    rule_backend: Resource,
    client: &kube::Client,
    ns: &str,
) {
    let backends = [rule_backend.clone()];
    let route = mk_tls_route(ns, "foo-route", &parent, Some(4191)).with_backends(&backends);
    let _route = create(client, route.build()).await;
    await_tls_route_status(client, ns, "foo-route").await;

    let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;
    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an updated config");
    tracing::trace!(?config);

    assert_resource_meta(&config.metadata, &parent, 4191);

    let routes = tls_routes(&config);
    let route = assert_route_attached(routes, &parent);
    let backends = tls_route_backends_random_available(route);
    let backend = assert_singleton(backends);
    assert_tls_backend_matches_parent(backend.backend.as_ref().unwrap(), &rule_backend, 8888);
}

async fn parent_with_tls_routes_with_invalid_backend(
    parent: Resource,
    backend: Resource,
    client: &kube::Client,
    ns: &str,
) {
    let backends = [backend];
    let route = mk_tls_route(ns, "foo-route", &parent, Some(4191)).with_backends(&backends);
    let _route = create(client, route.build()).await;
    await_tls_route_status(client, ns, "foo-route").await;

    let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;

    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an updated config");
    tracing::trace!(?config);

    assert_resource_meta(&config.metadata, &parent, 4191);

    let routes = tls_routes(&config);
    let route = assert_route_attached(routes, &parent);
    let backends = tls_route_backends_random_available(route);
    assert_singleton(backends);
}

async fn parent_with_multiple_tls_routes(parent: Resource, client: &kube::Client, ns: &str) {
    // Routes should be returned in sorted order by creation timestamp then
    // name. To ensure that this test isn't timing dependant, routes should
    // be created in alphabetical order.
    let _a_route = create(
        client,
        mk_tls_route(ns, "a-route", &parent, Some(4191))
            .with_backends(&[parent.clone()])
            .build(),
    )
    .await;
    await_tls_route_status(client, ns, "a-route").await;

    let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;

    // First route update.
    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an updated config");
    tracing::trace!(?config);

    assert_resource_meta(&config.metadata, &parent, 4191);

    let _b_route = create(
        client,
        mk_tls_route(ns, "b-route", &parent, Some(4191))
            .with_backends(&[parent.clone()])
            .build(),
    )
    .await;
    await_tls_route_status(client, ns, "b-route").await;

    // Second route update.
    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an updated config");
    tracing::trace!(?config);

    assert_resource_meta(&config.metadata, &parent, 4191);

    let routes = tls_routes(&config);
    let num_routes = match parent {
        Resource::EgressNetwork(_) => 3, // three routes for egress net 2 configured + 1 default
        Resource::Service(_) => 2,       // two routes for service
    };
    assert_eq!(routes.len(), num_routes);
    assert_eq!(tls_route_name(&routes[0]), "a-route");
    assert_eq!(tls_route_name(&routes[1]), "b-route");
}

async fn tls_route_reattachment(parent: Resource, client: &kube::Client, ns: &str) {
    let mut route = create(
        client,
        mk_tls_route(ns, "foo-route", &parent, Some(4191))
            .with_backends(&[parent.clone()])
            .build(),
    )
    .await;
    await_tls_route_status(client, ns, "foo-route").await;

    let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;
    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an initial config");
    tracing::trace!(?config);

    assert_resource_meta(&config.metadata, &parent, 4191);

    // The route should be attached.
    let routes = tls_routes(&config);
    let tls_route: &grpc::outbound::TlsRoute = assert_route_attached(routes, &parent);
    assert_tls_route_name_eq(tls_route, "foo-route");

    route
        .spec
        .inner
        .parent_refs
        .as_mut()
        .unwrap()
        .first_mut()
        .unwrap()
        .name = "other".to_string();
    update(client, route.clone()).await;

    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an updated config");
    tracing::trace!(?config);

    assert_resource_meta(&config.metadata, &parent, 4191);

    // The route should be unattached and the default route should be present.
    detect_http_routes(&config, |routes| {
        let route = assert_singleton(routes);
        assert_route_is_default(route, &parent, 4191);
    });

    route
        .spec
        .inner
        .parent_refs
        .as_mut()
        .unwrap()
        .first_mut()
        .unwrap()
        .name = parent.name();
    update(client, route).await;

    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an updated config");
    tracing::trace!(?config);

    assert_resource_meta(&config.metadata, &parent, 4191);

    // The route should be attached again.
    // The route should be attached.
    let routes = tls_routes(&config);
    let tls_route: &grpc::outbound::TlsRoute = assert_route_attached(routes, &parent);
    assert_tls_route_name_eq(tls_route, "foo-route");
}
