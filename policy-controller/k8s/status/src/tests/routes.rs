use crate::{
    index::{self, SharedIndex},
    resource_id::NamespaceGroupKindName,
    routes::RouteType,
    Index, IndexMetrics,
};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::POLICY_CONTROLLER_NAME;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy::server::Port};
use pretty_assertions::assert_eq;
use rstest::{fixture, rstest};
use std::sync::Arc;
use tokio::sync::{mpsc, watch};

const ACCEPTED: &str = "Accepted";
const TEST_HOSTNAME: &str = "test";
const TEST_NAMESPACE: &str = "ns-0";
const TEST_ROUTE_NAME: &str = "route-foo";
const TEST_SERVICE_NAME: &str = "srv-8080";

#[rstest]
#[case::linkerd_http_route(RouteType::LinkerdHttp)]
#[case::gateway_http_route(RouteType::GatewayHttp)]
#[cfg_attr(
    feature = "experimental",
    case::gateway_grpc_route(RouteType::GatewayGrpc)
)]
fn route_accepted_after_server_create(#[case] route_type: RouteType) {
    let claim = kubert::lease::Claim {
        holder: TEST_HOSTNAME.to_string(),
        expiry: chrono::DateTime::<chrono::Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        TEST_HOSTNAME,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    // Apply the route.
    let index = apply_matching_route_type(
        index,
        TEST_NAMESPACE,
        TEST_ROUTE_NAME,
        TEST_SERVICE_NAME,
        route_type,
    );

    // Create the expected update.
    let id = NamespaceGroupKindName {
        namespace: TEST_NAMESPACE.to_string(),
        gkn: route_type.gkn(TEST_ROUTE_NAME),
    };
    let parent_status = make_parent_status(
        TEST_NAMESPACE,
        TEST_SERVICE_NAME,
        ACCEPTED,
        "False",
        "NoMatchingParent",
        route_type,
    );
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(TEST_ROUTE_NAME, status, route_type);

    // The first update will be that the route is not accepted because the
    // Server has been created yet.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the server
    let server = make_test_server(
        Some(TEST_NAMESPACE),
        Some(TEST_SERVICE_NAME),
        Port::Number(8080.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // Create the expected update.
    let parent_status = make_parent_status(
        TEST_NAMESPACE,
        TEST_SERVICE_NAME,
        ACCEPTED,
        "True",
        ACCEPTED,
        route_type,
    );
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(TEST_ROUTE_NAME, status, route_type);

    // The second update will be that the route is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[rstest]
#[case::linkerd_http_route(RouteType::LinkerdHttp)]
#[case::gateway_http_route(RouteType::GatewayHttp)]
#[cfg_attr(
    feature = "experimental",
    case::gateway_grpc_route(RouteType::GatewayGrpc)
)]
fn route_rejected_after_server_delete(#[case] route_type: RouteType) {
    let claim = kubert::lease::Claim {
        holder: TEST_HOSTNAME.to_string(),
        expiry: chrono::DateTime::<chrono::Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        TEST_HOSTNAME,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    // Apply the server
    let server = make_test_server(
        Some(TEST_NAMESPACE),
        Some(TEST_SERVICE_NAME),
        Port::Number(8080.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // There should be no update since there are no routes yet.
    assert!(updates_rx.try_recv().is_err());

    // Apply the route.
    let index = apply_matching_route_type(
        index,
        TEST_NAMESPACE,
        TEST_ROUTE_NAME,
        TEST_SERVICE_NAME,
        route_type,
    );

    // Create the expected update.
    let id = NamespaceGroupKindName {
        namespace: TEST_NAMESPACE.to_string(),
        gkn: route_type.gkn(TEST_ROUTE_NAME),
    };
    let parent_status = make_parent_status(
        TEST_NAMESPACE,
        TEST_SERVICE_NAME,
        ACCEPTED,
        "True",
        ACCEPTED,
        route_type,
    );
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(TEST_ROUTE_NAME, status, route_type);

    // The second update will be that the route is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    {
        let mut index = index.write();
        <index::Index as kubert::index::IndexNamespacedResource<k8s::policy::Server>>::delete(
            &mut index,
            TEST_NAMESPACE.to_string(),
            TEST_SERVICE_NAME.to_string(),
        );
    }

    // Create the expected update.
    let parent_status = make_parent_status(
        TEST_NAMESPACE,
        TEST_SERVICE_NAME,
        ACCEPTED,
        "False",
        "NoMatchingParent",
        route_type,
    );
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(TEST_ROUTE_NAME, status, route_type);

    // The third update will be that the route is not accepted because the
    // Server has been deleted.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err());
}

#[fixture]
fn make_test_server(
    #[default(Option::<&str>::None)] namespace: Option<impl ToString>,
    #[default(Option::<&str>::None)] name: Option<impl ToString>,
    #[default(Port::Number(8080.try_into().unwrap()))] port: Port,
    #[default(Some(("app", "app-0")))] srv_labels: impl IntoIterator<
        Item = (&'static str, &'static str),
    >,
    #[default(Some(("app", "app-0")))] pod_labels: impl IntoIterator<
        Item = (&'static str, &'static str),
    >,
    #[default(Some(k8s::policy::server::ProxyProtocol::Http1))] proxy_protocol: Option<
        k8s::policy::server::ProxyProtocol,
    >,
) -> k8s::policy::Server {
    let (name, namespace) = (
        name.map(|value| value.to_string())
            .unwrap_or(TEST_ROUTE_NAME.to_string()),
        namespace
            .map(|value| value.to_string())
            .unwrap_or(TEST_NAMESPACE.to_string()),
    );

    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            name: Some(name),
            namespace: Some(namespace),
            labels: Some(
                srv_labels
                    .into_iter()
                    .map(|(key, value)| (key.to_string(), value.to_string()))
                    .collect(),
            ),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            port,
            proxy_protocol,
            selector: k8s::policy::server::Selector::Pod(pod_labels.into_iter().collect()),
        },
    }
}

fn make_status(parents: Vec<gateway::RouteParentStatus>) -> gateway::HttpRouteStatus {
    gateway::HttpRouteStatus {
        inner: gateway::RouteStatus { parents },
    }
}

fn make_parent_status(
    namespace: impl ToString,
    name: impl ToString,
    type_: impl ToString,
    status: impl ToString,
    reason: impl ToString,
    route_type: RouteType,
) -> gateway::RouteParentStatus {
    use k8s::Resource;

    let parent_ref_group: String = match route_type {
        RouteType::LinkerdHttp => k8s::policy::HttpRoute::group(&()).to_string(),
        RouteType::GatewayHttp => k8s_gateway_api::HttpRoute::group(&()).to_string(),
        #[cfg(feature = "experimental")]
        RouteType::GatewayGrpc => k8s_gateway_api::GrpcRoute::group(&()).to_string(),
    };

    let condition = k8s::Condition {
        message: "".to_string(),
        type_: type_.to_string(),
        observed_generation: None,
        reason: reason.to_string(),
        status: status.to_string(),
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
    };

    gateway::RouteParentStatus {
        conditions: vec![condition],
        parent_ref: gateway::ParentReference {
            port: None,
            section_name: None,
            name: name.to_string(),
            group: Some(parent_ref_group),
            kind: Some("Server".to_string()),
            namespace: Some(namespace.to_string()),
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
    }
}

fn make_linkerd_http_route(
    namespace: impl ToString,
    name: impl ToString,
    server: impl ToString,
) -> k8s::policy::HttpRoute {
    use chrono::Utc;
    use k8s::{policy::httproute::*, Resource, Time};

    k8s::policy::HttpRoute {
        status: None,
        metadata: k8s::ObjectMeta {
            name: Some(name.to_string()),
            namespace: Some(namespace.to_string()),
            creation_timestamp: Some(Time(Utc::now())),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            hostnames: None,
            inner: CommonRouteSpec {
                parent_refs: Some(vec![ParentReference {
                    port: None,
                    namespace: None,
                    section_name: None,
                    name: server.to_string(),
                    kind: Some(k8s::policy::Server::kind(&()).to_string()),
                    group: Some(k8s::policy::Server::group(&()).to_string()),
                }]),
            },
            rules: Some(vec![HttpRouteRule {
                filters: None,
                timeouts: None,
                backend_refs: None,
                matches: Some(vec![HttpRouteMatch {
                    headers: None,
                    query_params: None,
                    method: Some("GET".to_string()),
                    path: Some(HttpPathMatch::PathPrefix {
                        value: "/foo/bar".to_string(),
                    }),
                }]),
            }]),
        },
    }
}

fn make_gateway_http_route(
    namespace: impl ToString,
    name: impl ToString,
    server: impl ToString,
) -> k8s_gateway_api::HttpRoute {
    use chrono::Utc;
    use k8s::Resource;

    k8s_gateway_api::HttpRoute {
        status: None,
        metadata: k8s::ObjectMeta {
            name: Some(name.to_string()),
            namespace: Some(namespace.to_string()),
            creation_timestamp: Some(k8s::Time(Utc::now())),
            ..Default::default()
        },
        spec: k8s_gateway_api::HttpRouteSpec {
            hostnames: None,
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                    port: None,
                    namespace: None,
                    section_name: None,
                    name: server.to_string(),
                    kind: Some(k8s::policy::Server::kind(&()).to_string()),
                    group: Some(k8s::policy::Server::group(&()).to_string()),
                }]),
            },
            rules: Some(vec![k8s_gateway_api::HttpRouteRule {
                filters: None,
                backend_refs: None,
                matches: Some(vec![k8s_gateway_api::HttpRouteMatch {
                    headers: None,
                    query_params: None,
                    method: Some("GET".to_string()),
                    path: Some(k8s_gateway_api::HttpPathMatch::PathPrefix {
                        value: "/foo/bar".to_string(),
                    }),
                }]),
            }]),
        },
    }
}

#[cfg(feature = "experimental")]
fn make_gateway_grpc_route(
    namespace: impl ToString,
    name: impl ToString,
    server: impl ToString,
) -> k8s_gateway_api::GrpcRoute {
    use chrono::Utc;
    use k8s::Resource;

    k8s_gateway_api::GrpcRoute {
        status: None,
        metadata: k8s::ObjectMeta {
            name: Some(name.to_string()),
            namespace: Some(namespace.to_string()),
            creation_timestamp: Some(k8s::Time(Utc::now())),
            ..Default::default()
        },
        spec: k8s_gateway_api::GrpcRouteSpec {
            hostnames: None,
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                    port: None,
                    namespace: None,
                    section_name: None,
                    name: server.to_string(),
                    kind: Some(k8s::policy::Server::kind(&()).to_string()),
                    group: Some(k8s::policy::Server::group(&()).to_string()),
                }]),
            },
            rules: Some(vec![k8s_gateway_api::GrpcRouteRule {
                filters: None,
                backend_refs: None,
                matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                    headers: None,
                    method: Some(k8s_gateway_api::GrpcMethodMatch::Exact {
                        method: Some("TestMethod".to_string()),
                        service: Some("testing.Tests".to_string()),
                    }),
                }]),
            }]),
        },
    }
}

fn apply_matching_route_type(
    index: SharedIndex,
    namespace: impl ToString,
    name: impl ToString,
    server: impl ToString,
    route_type: RouteType,
) -> SharedIndex {
    match route_type {
        RouteType::LinkerdHttp => index
            .write()
            .apply(make_linkerd_http_route(namespace, name, server)),
        RouteType::GatewayHttp => index
            .write()
            .apply(make_gateway_http_route(namespace, name, server)),
        #[cfg(feature = "experimental")]
        RouteType::GatewayGrpc => index
            .write()
            .apply(make_gateway_grpc_route(namespace, name, server)),
    };

    index
}
