use futures::prelude::*;
use kube::ResourceExt;
use linkerd_policy_test::{
    assert_resource_meta, assert_status_accepted, await_egress_net_status, await_grpc_route_status,
    create, create_egress_network, create_service, mk_egress_net, mk_service, outbound_api::*,
    update, with_temp_ns, Resource,
};
use std::collections::BTreeMap;

#[tokio::test(flavor = "current_thread")]
async fn service_grpc_route_retries_and_timeouts() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = Resource::Service(create_service(&client, &ns, "my-svc", 4191).await);
        grpc_route_retries_and_timeouts(svc, &client, &ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_net_grpc_route_retries_and_timeouts() {
    with_temp_ns(|client, ns| async move {
        // Create a egress net
        let egress =
            Resource::EgressNetwork(create_egress_network(&client, &ns, "my-egress").await);
        let status = await_egress_net_status(&client, &ns, "my-egress").await;
        assert_status_accepted(status.conditions);

        grpc_route_retries_and_timeouts(egress, &client, &ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_retries_and_timeouts() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let mut svc = mk_service(&ns, "my-svc", 4191);
        svc.annotations_mut()
            .insert("retry.linkerd.io/grpc".to_string(), "internal".to_string());
        svc.annotations_mut()
            .insert("timeout.linkerd.io/response".to_string(), "10s".to_string());
        let svc = Resource::Service(create(&client, svc).await);

        parent_retries_and_timeouts(svc, &client, &ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_net_retries_and_timeouts() {
    with_temp_ns(|client, ns| async move {
        // Create a egress net
        let mut egress = mk_egress_net(&ns, "my-egress");
        egress
            .annotations_mut()
            .insert("retry.linkerd.io/grpc".to_string(), "internal".to_string());
        egress
            .annotations_mut()
            .insert("timeout.linkerd.io/response".to_string(), "10s".to_string());
        let egress = Resource::EgressNetwork(create(&client, egress).await);
        let status = await_egress_net_status(&client, &ns, "my-egress").await;
        assert_status_accepted(status.conditions);

        parent_retries_and_timeouts(egress, &client, &ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn service_grpc_route_reattachment() {
    with_temp_ns(|client, ns| async move {
        // Create a service
        let svc = create_service(&client, &ns, "my-svc", 4191).await;
        grpc_route_reattachment(Resource::Service(svc), &client, &ns).await;
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn egress_net_grpc_route_reattachment() {
    with_temp_ns(|client, ns| async move {
        // Create a egress network
        let egress = create_egress_network(&client, &ns, "my-egress").await;
        let status = await_egress_net_status(&client, &ns, "my-egress").await;
        assert_status_accepted(status.conditions);

        grpc_route_reattachment(Resource::EgressNetwork(egress), &client, &ns).await;
    })
    .await;
}

/* Helpers */

struct GrpcRouteBuilder(k8s_gateway_api::GrpcRoute);

fn mk_grpc_route(ns: &str, name: &str, parent: &Resource, port: Option<u16>) -> GrpcRouteBuilder {
    GrpcRouteBuilder(k8s_gateway_api::GrpcRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s_gateway_api::GrpcRouteSpec {
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                    group: Some(parent.group()),
                    kind: Some(parent.kind()),
                    namespace: Some(parent.namespace()),
                    name: parent.name(),
                    section_name: None,
                    port,
                }]),
            },
            hostnames: None,
            rules: Some(vec![k8s_gateway_api::GrpcRouteRule {
                matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                    method: Some(k8s_gateway_api::GrpcMethodMatch::Exact {
                        method: Some("foo".to_string()),
                        service: Some("my-gprc-service".to_string()),
                    }),
                    headers: None,
                }]),
                filters: None,
                backend_refs: None,
            }]),
        },
        status: None,
    })
}

impl GrpcRouteBuilder {
    fn with_annotations(self, annotations: BTreeMap<String, String>) -> Self {
        let mut route = self.0;
        route.metadata.annotations = Some(annotations);
        Self(route)
    }

    fn build(self) -> k8s_gateway_api::GrpcRoute {
        self.0
    }
}

async fn grpc_route_reattachment(parent: Resource, client: &kube::Client, ns: &str) {
    let mut route = create(
        client,
        mk_grpc_route(ns, "foo-route", &parent, Some(4191)).build(),
    )
    .await;
    await_grpc_route_status(client, ns, "foo-route").await;

    let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;
    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an initial config");
    tracing::trace!(?config);

    assert_resource_meta(&config.metadata, &parent, 4191);

    {
        // The route should be attached.
        let routes = grpc_routes(&config);
        let route = assert_route_attached(routes, &parent);
        assert_name_eq(route.metadata.as_ref().unwrap(), "foo-route");
    }

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

    // The grpc route should be unattached and the default (http) route
    // should be present.
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
    {
        // The route should be attached.
        let routes = grpc_routes(&config);
        let route = assert_route_attached(routes, &parent);
        assert_name_eq(route.metadata.as_ref().unwrap(), "foo-route");
    }
}

async fn grpc_route_retries_and_timeouts(parent: Resource, client: &kube::Client, ns: &str) {
    let _route = create(
        client,
        mk_grpc_route(ns, "foo-route", &parent, Some(4191))
            .with_annotations(
                vec![
                    ("retry.linkerd.io/grpc".to_string(), "internal".to_string()),
                    ("timeout.linkerd.io/response".to_string(), "10s".to_string()),
                ]
                .into_iter()
                .collect(),
            )
            .build(),
    )
    .await;
    await_grpc_route_status(client, ns, "foo-route").await;

    let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;
    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an initial config");
    tracing::trace!(?config);

    let routes = grpc_routes(&config);
    let route = assert_route_attached(routes, &parent);
    let rule = assert_singleton(&route.rules);
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
}

async fn parent_retries_and_timeouts(parent: Resource, client: &kube::Client, ns: &str) {
    let _route = create(
        client,
        mk_grpc_route(ns, "foo-route", &parent, Some(4191))
            .with_annotations(
                vec![
                    // Route annotations override the timeout config specified
                    // on the service.
                    ("timeout.linkerd.io/request".to_string(), "5s".to_string()),
                ]
                .into_iter()
                .collect(),
            )
            .build(),
    )
    .await;
    await_grpc_route_status(client, ns, "foo-route").await;

    let mut rx = retry_watch_outbound_policy(client, ns, &parent, 4191).await;
    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an initial config");
    tracing::trace!(?config);

    let routes = grpc_routes(&config);
    let route = assert_route_attached(routes, &parent);
    let rule = assert_singleton(&route.rules);
    let conditions = rule
        .retry
        .as_ref()
        .expect("retry config expected")
        .conditions
        .as_ref()
        .expect("retry conditions expected");
    // Retry config inherited from the service.
    assert!(conditions.internal);
    let timeouts = rule.timeouts.as_ref().expect("timeouts expected");
    // Parent timeout config overridden by route timeout config.
    assert_eq!(timeouts.response, None);
    let request_timeout = timeouts.request.as_ref().expect("request timeout expected");
    assert_eq!(request_timeout.seconds, 5);
}
