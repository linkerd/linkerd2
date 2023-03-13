use crate::{index, index::POLICY_API_GROUP, resource_id::ResourceId, Index};
use k8s::{gateway::HttpBackendRef, Time};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::POLICY_CONTROLLER_NAME;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy::server::Port};
use std::sync::Arc;
use tokio::sync::{mpsc, watch};

#[test]
fn http_route_accepted_with_resolved_refs() {
    let (index, mut updates_rx) = new_test_index();

    // SharedIndex init
    // TODO (matei): change to service when we introduce svc parentRef support
    // Apply the server
    let server = make_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // Apply service
    let svc = k8s::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some("ns-0".to_string()),
            name: Some("foo".to_string()),
            creation_timestamp: Some(Time(chrono::Utc::now())),
            ..Default::default()
        },
        ..Default::default()
    };
    index.write().apply(svc);

    // Apply the route.
    let backend_ref = {
        let backend_ref = gateway::BackendRef {
            weight: None,
            // Unspecified group & kind default to Service object
            inner: gateway::BackendObjectReference {
                name: "foo".to_string(),
                namespace: Some("ns-0".to_string()),
                port: Some(8080),
                group: None,
                kind: None,
            },
        };
        HttpBackendRef {
            backend_ref: Some(backend_ref),
            filters: None,
        }
    };

    let http_route =
        make_route_with_backends("ns-0", "route-foo", "srv-8080", Some(vec![backend_ref]));
    index.write().apply(http_route);

    // Create the expected update.
    let expected_id = ResourceId::new("ns-0".to_string(), "route-foo".to_string());
    let backend_condition = make_backend_condition(true, false);
    let parent_status = make_parent_status(
        "ns-0",
        "srv-8080",
        "Accepted",
        "True",
        "Accepted",
        backend_condition,
    );
    let status = make_status(vec![parent_status]);
    let expected_patch = index::make_patch("route-foo", status);

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(expected_id, update.id);
    assert_eq!(expected_patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn http_backend_rejected_invalid_kind() {
    let (index, mut updates_rx) = new_test_index();

    // SharedIndex init
    // TODO (matei): change to service when we introduce svc parentRef support
    // Apply the server
    let server = make_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // Apply the route with an unsupported BackendReference
    // In this case, we re-use the Server object
    let backend_ref = {
        let backend_ref = gateway::BackendRef {
            weight: None,
            inner: gateway::BackendObjectReference {
                name: "srv-8080".to_string(),
                namespace: Some("ns-0".to_string()),
                port: None,
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("Server".to_string()),
            },
        };
        HttpBackendRef {
            backend_ref: Some(backend_ref),
            filters: None,
        }
    };

    let http_route =
        make_route_with_backends("ns-0", "route-foo", "srv-8080", Some(vec![backend_ref]));
    index.write().apply(http_route);

    // Create the expected update.
    let expected_id = ResourceId::new("ns-0".to_string(), "route-foo".to_string());
    let backend_condition = make_backend_condition(false, true);
    let parent_status = make_parent_status(
        "ns-0",
        "srv-8080",
        "Accepted",
        "True",
        "Accepted",
        backend_condition,
    );

    let status = make_status(vec![parent_status]);
    let expected_patch = index::make_patch("route-foo", status);

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(expected_id, update.id);
    assert_eq!(expected_patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn http_backend_rejected_not_found() {
    let (index, mut updates_rx) = new_test_index();

    // SharedIndex init
    // TODO (matei): change to service when we introduce svc parentRef support
    // Apply the server
    let server = make_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // Apply the route with an unsupported BackendReference
    // In this case, we re-use the Server object
    let backend_ref = {
        let backend_ref = gateway::BackendRef {
            weight: None,
            inner: gateway::BackendObjectReference {
                name: "404notfound".to_string(),
                namespace: Some("ns-0".to_string()),
                port: None,
                group: None,
                kind: Some("Service".to_string()),
            },
        };
        HttpBackendRef {
            backend_ref: Some(backend_ref),
            filters: None,
        }
    };

    let http_route =
        make_route_with_backends("ns-0", "route-foo", "srv-8080", Some(vec![backend_ref]));
    index.write().apply(http_route);

    // Create the expected update.
    let expected_id = ResourceId::new("ns-0".to_string(), "route-foo".to_string());
    let backend_condition = make_backend_condition(false, false);
    let parent_status = make_parent_status(
        "ns-0",
        "srv-8080",
        "Accepted",
        "True",
        "Accepted",
        backend_condition,
    );

    let status = make_status(vec![parent_status]);
    let expected_patch = index::make_patch("route-foo", status);

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(expected_id, update.id);
    assert_eq!(expected_patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn http_route_accepted_after_server_create() {
    let (index, mut updates_rx) = new_test_index();

    // Apply the route.
    let http_route = make_route("ns-0", "route-foo", "srv-8080");
    index.write().apply(http_route);

    // Create the expected update.
    let id = ResourceId::new("ns-0".to_string(), "route-foo".to_string());
    let backend_condition = make_backend_condition(true, false);
    let parent_status = make_parent_status(
        "ns-0",
        "srv-8080",
        "Accepted",
        "False",
        "NoMatchingParent",
        backend_condition,
    );
    let status = make_status(vec![parent_status]);
    let patch = index::make_patch("route-foo", status);

    // The first update will be that the HTTPRoute is not accepted because the
    // Server has been created yet.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the server
    let server = make_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // Create the expected update.
    let id = ResourceId::new("ns-0".to_string(), "route-foo".to_string());
    let backend_condition = make_backend_condition(true, false);
    let parent_status = make_parent_status(
        "ns-0",
        "srv-8080",
        "Accepted",
        "True",
        "Accepted",
        backend_condition,
    );
    let status = make_status(vec![parent_status]);
    let patch = index::make_patch("route-foo", status);

    // The second update will be that the HTTPRoute is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn http_route_rejected_after_server_delete() {
    let (index, mut updates_rx) = new_test_index();

    let server = make_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // There should be no update since there are no HTTPRoutes yet.
    assert!(updates_rx.try_recv().is_err());

    let http_route = make_route("ns-0", "route-foo", "srv-8080");
    index.write().apply(http_route);

    // Create the expected update.
    let id = ResourceId::new("ns-0".to_string(), "route-foo".to_string());
    let backend_condition = make_backend_condition(true, false);
    let parent_status = make_parent_status(
        "ns-0",
        "srv-8080",
        "Accepted",
        "True",
        "Accepted",
        backend_condition,
    );
    let status = make_status(vec![parent_status]);
    let patch = index::make_patch("route-foo", status);

    // The second update will be that the HTTPRoute is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    {
        let mut index = index.write();
        <index::Index as kubert::index::IndexNamespacedResource<k8s::policy::Server>>::delete(
            &mut index,
            "ns-0".to_string(),
            "srv-8080".to_string(),
        );
    }

    // Create the expected update.
    let id = ResourceId::new("ns-0".to_string(), "route-foo".to_string());

    let backend_condition = make_backend_condition(true, false);
    let parent_status = make_parent_status(
        "ns-0",
        "srv-8080",
        "Accepted",
        "False",
        "NoMatchingParent",
        backend_condition,
    );
    let status = make_status(vec![parent_status]);
    let patch = index::make_patch("route-foo", status);

    // The third update will be that the HTTPRoute is not accepted because the
    // Server has been deleted.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err());
}

// Helper to create shared index instances that have a leader election claim
fn new_test_index() -> (index::SharedIndex, mpsc::UnboundedReceiver<index::Update>) {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: chrono::DateTime::<chrono::Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, updates_rx) = mpsc::unbounded_channel();
    (Index::shared(hostname, claims_rx, updates_tx), updates_rx)
}

fn make_server(
    namespace: impl ToString,
    name: impl ToString,
    port: Port,
    srv_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    pod_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    proxy_protocol: Option<k8s::policy::server::ProxyProtocol>,
) -> k8s::policy::Server {
    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            namespace: Some(namespace.to_string()),
            name: Some(name.to_string()),
            labels: Some(
                srv_labels
                    .into_iter()
                    .map(|(k, v)| (k.to_string(), v.to_string()))
                    .collect(),
            ),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            port,
            pod_selector: pod_labels.into_iter().collect(),
            proxy_protocol,
        },
    }
}

fn make_route(
    namespace: impl ToString,
    name: impl ToString,
    server: impl ToString,
) -> k8s::policy::HttpRoute {
    make_route_with_backends(namespace, name, server, None)
}

fn make_route_with_backends(
    namespace: impl ToString,
    name: impl ToString,
    server: impl ToString,
    backends: Option<Vec<gateway::HttpBackendRef>>,
) -> k8s::policy::HttpRoute {
    use chrono::Utc;
    use k8s::policy::httproute::*;

    HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(namespace.to_string()),
            name: Some(name.to_string()),
            creation_timestamp: Some(Time(Utc::now())),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![ParentReference {
                    group: Some(POLICY_API_GROUP.to_string()),
                    kind: Some("Server".to_string()),
                    namespace: None,
                    name: server.to_string(),
                    section_name: None,
                    port: None,
                }]),
            },
            hostnames: None,
            rules: Some(vec![HttpRouteRule {
                matches: Some(vec![HttpRouteMatch {
                    path: Some(HttpPathMatch::PathPrefix {
                        value: "/foo/bar".to_string(),
                    }),
                    headers: None,
                    query_params: None,
                    method: Some("GET".to_string()),
                }]),
                filters: None,
                backend_refs: backends,
            }]),
        },
        status: None,
    }
}

fn make_parent_status(
    namespace: impl ToString,
    name: impl ToString,
    type_: impl ToString,
    status: impl ToString,
    reason: impl ToString,
    backend_condition: k8s::Condition,
) -> gateway::RouteParentStatus {
    let condition = k8s::Condition {
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: reason.to_string(),
        status: status.to_string(),
        type_: type_.to_string(),
    };
    gateway::RouteParentStatus {
        parent_ref: gateway::ParentReference {
            group: Some(POLICY_API_GROUP.to_string()),
            kind: Some("Server".to_string()),
            namespace: Some(namespace.to_string()),
            name: name.to_string(),
            section_name: None,
            port: None,
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![condition, backend_condition],
    }
}

fn make_backend_condition(resolved_all: bool, invalid_kind: bool) -> k8s::Condition {
    if invalid_kind {
        return k8s::Condition {
            last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
            type_: "ResolvedRefs".to_string(),
            status: "False".to_string(),
            reason: "InvalidKind".to_string(),
            observed_generation: None,
            message: "".to_string(),
        };
    }

    if !resolved_all {
        return k8s::Condition {
            last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
            type_: "ResolvedRefs".to_string(),
            status: "False".to_string(),
            reason: "BackendDoesNotExist".to_string(),
            observed_generation: None,
            message: "".to_string(),
        };
    }

    k8s::Condition {
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
        type_: "ResolvedRefs".to_string(),
        status: "True".to_string(),
        reason: "ResolvedRefs".to_string(),
        observed_generation: None,
        message: "".to_string(),
    }
}

fn make_status(parents: Vec<gateway::RouteParentStatus>) -> gateway::HttpRouteStatus {
    gateway::HttpRouteStatus {
        inner: gateway::RouteStatus { parents },
    }
}
