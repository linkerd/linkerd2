use crate::{
    index::{self, POLICY_API_GROUP},
    resource_id::NamespaceGroupKindName,
    Index, IndexMetrics,
};
use gateway::{BackendObjectReference, BackendRef, HttpBackendRef, ParentReference};
use k8s::{Resource, ResourceExt};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{routes::GroupKindName, POLICY_CONTROLLER_NAME};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy::server::Port};
use std::sync::Arc;
use tokio::sync::{mpsc, watch};

#[test]
fn http_route_accepted_after_server_create() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: chrono::DateTime::<chrono::Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    // Apply the route.
    let parent = ParentReference {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("Server".to_string()),
        namespace: None,
        name: "srv-8080".to_string(),
        section_name: None,
        port: None,
    };
    let http_route = make_route("ns-0", "route-foo", parent, None);
    index.write().apply(http_route);

    // Create the expected update.
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: k8s::policy::HttpRoute::group(&()),
            kind: k8s::policy::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent_status =
        make_parent_status("ns-0", "srv-8080", "Accepted", "False", "NoMatchingParent");
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
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: k8s::policy::HttpRoute::group(&()),
            kind: k8s::policy::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent_status = make_parent_status("ns-0", "srv-8080", "Accepted", "True", "Accepted");
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
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: chrono::DateTime::<chrono::Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

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

    let parent = ParentReference {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("Server".to_string()),
        namespace: None,
        name: "srv-8080".to_string(),
        section_name: None,
        port: None,
    };
    let http_route = make_route("ns-0", "route-foo", parent, None);
    index.write().apply(http_route);

    // Create the expected update.
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: k8s::policy::HttpRoute::group(&()),
            kind: k8s::policy::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent_status = make_parent_status("ns-0", "srv-8080", "Accepted", "True", "Accepted");
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
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: k8s::policy::HttpRoute::group(&()),
            kind: k8s::policy::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent_status =
        make_parent_status("ns-0", "srv-8080", "Accepted", "False", "NoMatchingParent");
    let status = make_status(vec![parent_status]);
    let patch = index::make_patch("route-foo", status);

    // The third update will be that the HTTPRoute is not accepted because the
    // Server has been deleted.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err());
}

#[test]
fn http_route_with_invalid_backend() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: chrono::DateTime::<chrono::Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    // Apply the parent service
    let parent = make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply one backend service
    let backend = make_service("ns-0", "backend-1");
    index.write().apply(backend.clone());

    // Apply the route.
    let parent = ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let http_route = make_route(
        "ns-0",
        "route-foo",
        parent.clone(),
        Some(vec![
            HttpBackendRef {
                backend_ref: Some(BackendRef {
                    weight: None,
                    inner: BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: backend.name_unchecked(),
                        namespace: backend.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
            HttpBackendRef {
                backend_ref: Some(BackendRef {
                    weight: None,
                    inner: BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: "nonexistant-backend".to_string(),
                        namespace: backend.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
        ]),
    );
    index.write().apply(http_route);

    // Create the expected update.
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: k8s::policy::HttpRoute::group(&()),
            kind: k8s::policy::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let accepted_condition = k8s::Condition {
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // One of the backends does not exist so the status should be BackendNotFound.
    let backend_condition = k8s::Condition {
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "BackendNotFound".to_string(),
        status: "False".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = gateway::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = index::make_patch("route-foo", status);

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn http_route_with_no_backends() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: chrono::DateTime::<chrono::Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    // Apply the parent service
    let parent = make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply the route.
    let parent = ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let http_route = make_route("ns-0", "route-foo", parent.clone(), None);
    index.write().apply(http_route);

    // Create the expected update.
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: k8s::policy::HttpRoute::group(&()),
            kind: k8s::policy::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let accepted_condition = k8s::Condition {
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // No backends were specified, so we have vacuously have resolved them all.
    let backend_condition = k8s::Condition {
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "ResolvedRefs".to_string(),
        status: "True".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = gateway::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = index::make_patch("route-foo", status);

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn http_route_with_valid_backends() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: chrono::DateTime::<chrono::Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    // Apply the parent service
    let parent = make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply one backend service
    let backend1 = make_service("ns-0", "backend-1");
    index.write().apply(backend1.clone());

    // Apply one backend service
    let backend2 = make_service("ns-0", "backend-2");
    index.write().apply(backend2.clone());

    // Apply the route.
    let parent = ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let http_route = make_route(
        "ns-0",
        "route-foo",
        parent.clone(),
        Some(vec![
            HttpBackendRef {
                backend_ref: Some(BackendRef {
                    weight: None,
                    inner: BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: backend1.name_unchecked(),
                        namespace: backend1.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
            HttpBackendRef {
                backend_ref: Some(BackendRef {
                    weight: None,
                    inner: BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: backend2.name_unchecked(),
                        namespace: backend2.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
        ]),
    );
    index.write().apply(http_route);

    // Create the expected update.
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: k8s::policy::HttpRoute::group(&()),
            kind: k8s::policy::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let accepted_condition = k8s::Condition {
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // All backends exist and can be resolved.
    let backend_condition = k8s::Condition {
        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "ResolvedRefs".to_string(),
        status: "True".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = gateway::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = index::make_patch("route-foo", status);

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
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
            selector: k8s::policy::server::Selector::Pod(pod_labels.into_iter().collect()),
            proxy_protocol,
        },
    }
}

fn make_service(namespace: impl ToString, name: impl ToString) -> k8s::api::core::v1::Service {
    k8s::api::core::v1::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some(namespace.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: Some(k8s::ServiceSpec {
            cluster_ip: Some("1.2.3.4".to_string()),
            cluster_ips: Some(vec!["1.2.3.4".to_string()]),
            ..Default::default()
        }),
        status: None,
    }
}

fn make_route(
    namespace: impl ToString,
    name: impl ToString,
    parent: ParentReference,
    backends: Option<Vec<HttpBackendRef>>,
) -> k8s::policy::HttpRoute {
    use chrono::Utc;
    use k8s::{policy::httproute::*, Time};

    HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(namespace.to_string()),
            name: Some(name.to_string()),
            creation_timestamp: Some(Time(Utc::now())),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![parent]),
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
                timeouts: None,
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
        conditions: vec![condition],
    }
}

fn make_status(parents: Vec<gateway::RouteParentStatus>) -> gateway::HttpRouteStatus {
    gateway::HttpRouteStatus {
        inner: gateway::RouteStatus { parents },
    }
}
