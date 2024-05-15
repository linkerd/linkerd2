use crate::{index::POLICY_API_GROUP, resource_id::NamespaceGroupKindName, Index, IndexMetrics};
use chrono::{DateTime, Utc};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{routes::GroupKindName, POLICY_CONTROLLER_NAME};
use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api, gateway as k8s_gateway_api, policy as linkerd_k8s_api, Resource,
    ResourceExt,
};
use std::sync::Arc;
use tokio::sync::{mpsc, watch};

#[test]
fn linkerd_route_with_no_backends() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
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
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply the route.
    let id = NamespaceGroupKindName {
        namespace: parent.namespace().as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::HttpRoute::group(&()),
            kind: linkerd_k8s_api::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent = linkerd_k8s_api::httproute::ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };

    let route = make_linkerd_route(&id, parent.clone(), None);
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // No backends were specified, so we have vacuously resolved them all.
    let backend_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "ResolvedRefs".to_string(),
        status: "True".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn gateway_route_with_no_backends() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
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
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply the route.
    let parent = k8s_gateway_api::ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: k8s_gateway_api::HttpRoute::group(&()),
            kind: k8s_gateway_api::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_gateway_route(&id, parent.clone(), None);
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // No backends were specified, so we have vacuously resolved them all.
    let backend_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "ResolvedRefs".to_string(),
        status: "True".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn linkerd_route_with_valid_backends() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
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
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply one backend service
    let backend1 = super::make_service("ns-0", "backend-1");
    index.write().apply(backend1.clone());

    // Apply one backend service
    let backend2 = super::make_service("ns-0", "backend-2");
    index.write().apply(backend2.clone());

    // Apply the route.
    let id = NamespaceGroupKindName {
        namespace: parent.namespace().as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::HttpRoute::group(&()),
            kind: linkerd_k8s_api::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent = linkerd_k8s_api::httproute::ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let route = make_linkerd_route(
        &id,
        parent.clone(),
        Some(vec![
            linkerd_k8s_api::httproute::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: backend1.name_unchecked(),
                        namespace: backend1.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
            linkerd_k8s_api::httproute::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
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
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // All backends exist and can be resolved.
    let backend_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "ResolvedRefs".to_string(),
        status: "True".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn gateway_route_with_valid_backends() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
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
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply one backend service
    let backend1 = super::make_service("ns-0", "backend-1");
    index.write().apply(backend1.clone());

    // Apply one backend service
    let backend2 = super::make_service("ns-0", "backend-2");
    index.write().apply(backend2.clone());

    // Apply the route.
    let parent = k8s_gateway_api::ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: k8s_gateway_api::HttpRoute::group(&()),
            kind: k8s_gateway_api::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_gateway_route(
        &id,
        parent.clone(),
        Some(vec![
            k8s_gateway_api::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: backend1.name_unchecked(),
                        namespace: backend1.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
            k8s_gateway_api::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
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
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // All backends exist and can be resolved.
    let backend_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "ResolvedRefs".to_string(),
        status: "True".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn linkerd_route_with_invalid_backend() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
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
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply one backend service
    let backend = super::make_service("ns-0", "backend-1");
    index.write().apply(backend.clone());

    // Apply the route.
    let parent = linkerd_k8s_api::httproute::ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::HttpRoute::group(&()),
            kind: linkerd_k8s_api::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_linkerd_route(
        &id,
        parent.clone(),
        Some(vec![
            linkerd_k8s_api::httproute::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: backend.name_unchecked(),
                        namespace: backend.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
            linkerd_k8s_api::httproute::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: "nonexistent-backend".to_string(),
                        namespace: backend.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
        ]),
    );
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // One of the backends does not exist so the status should be BackendNotFound.
    let backend_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "BackendNotFound".to_string(),
        status: "False".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn gateway_route_with_invalid_backend() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
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
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply one backend service
    let backend = super::make_service("ns-0", "backend-1");
    index.write().apply(backend.clone());

    // Apply the route.
    let parent = k8s_gateway_api::ParentReference {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: k8s_gateway_api::HttpRoute::group(&()),
            kind: k8s_gateway_api::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_gateway_route(
        &id,
        parent.clone(),
        Some(vec![
            k8s_gateway_api::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: backend.name_unchecked(),
                        namespace: backend.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
            k8s_gateway_api::HttpBackendRef {
                backend_ref: Some(k8s_gateway_api::BackendRef {
                    weight: None,
                    inner: k8s_gateway_api::BackendObjectReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        name: "nonexistent-backend".to_string(),
                        namespace: backend.namespace(),
                        port: Some(8080),
                    },
                }),
                filters: None,
            },
        ]),
    );
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "Accepted".to_string(),
        status: "True".to_string(),
        type_: "Accepted".to_string(),
    };
    // One of the backends does not exist so the status should be BackendNotFound.
    let backend_condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: "BackendNotFound".to_string(),
        status: "False".to_string(),
        type_: "ResolvedRefs".to_string(),
    };
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn linkerd_route_accepted_after_server_create() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::HttpRoute::group(&()),
            kind: linkerd_k8s_api::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent = linkerd_k8s_api::httproute::ParentReference {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("Server".to_string()),
        namespace: None,
        name: "srv-8080".to_string(),
        section_name: None,
        port: None,
    };
    let route = make_linkerd_route(&id, parent, None);

    // Apply the route.
    index.write().apply(route);

    // Create the expected update.
    let parent_status = make_parent_status(
        &id.namespace,
        "srv-8080",
        "Accepted",
        "False",
        "NoMatchingParent",
    );
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The first update will be that the HTTPRoute is not accepted because the
    // Server has been created yet.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the server
    let server = super::make_server(
        "ns-0",
        "srv-8080",
        8080,
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(linkerd_k8s_api::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // Create the expected update.
    let parent_status =
        make_parent_status(&id.namespace, "srv-8080", "Accepted", "True", "Accepted");
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the HTTPRoute is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn gateway_route_accepted_after_server_create() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            name: "route-foo".into(),
            kind: k8s_gateway_api::HttpRoute::kind(&()),
            group: k8s_gateway_api::HttpRoute::group(&()),
        },
    };
    let parent = k8s_gateway_api::ParentReference {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("Server".to_string()),
        namespace: None,
        name: "srv-8080".to_string(),
        section_name: None,
        port: None,
    };
    let route = make_gateway_route(&id, parent, None);

    // Apply the route.
    index.write().apply(route);

    // Create the expected update.
    let parent_status = make_parent_status(
        &id.namespace,
        "srv-8080",
        "Accepted",
        "False",
        "NoMatchingParent",
    );
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The first update will be that the HTTPRoute is not accepted because the
    // Server has been created yet.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the server
    let server = super::make_server(
        "ns-0",
        "srv-8080",
        8080,
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(linkerd_k8s_api::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // Create the expected update.
    let parent_status =
        make_parent_status(&id.namespace, "srv-8080", "Accepted", "True", "Accepted");
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the HTTPRoute is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn linkerd_route_rejected_after_server_delete() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    let server = super::make_server(
        "ns-0",
        "srv-8080",
        8080,
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(linkerd_k8s_api::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // There should be no update since there are no HTTPRoutes yet.
    assert!(updates_rx.try_recv().is_err());

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::HttpRoute::group(&()),
            kind: linkerd_k8s_api::HttpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent = linkerd_k8s_api::httproute::ParentReference {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("Server".to_string()),
        namespace: None,
        name: "srv-8080".to_string(),
        section_name: None,
        port: None,
    };
    let route = make_linkerd_route(&id, parent, None);

    // Apply the route
    index.write().apply(route);

    // Create the expected update.
    let parent_status =
        make_parent_status(&id.namespace, "srv-8080", "Accepted", "True", "Accepted");
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the HTTPRoute is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    {
        let mut index = index.write();
        <Index as IndexNamespacedResource<linkerd_k8s_api::Server>>::delete(
            &mut index,
            "ns-0".to_string(),
            "srv-8080".to_string(),
        );
    }

    // Create the expected update.
    let parent_status =
        make_parent_status("ns-0", "srv-8080", "Accepted", "False", "NoMatchingParent");
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The third update will be that the HTTPRoute is not accepted because the
    // Server has been deleted.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err());
}

#[test]
fn gateway_route_rejected_after_server_delete() {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, mut updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
    );

    let server = super::make_server(
        "ns-0",
        "srv-8080",
        8080,
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(linkerd_k8s_api::server::ProxyProtocol::Http1),
    );
    index.write().apply(server);

    // There should be no update since there are no HTTPRoutes yet.
    assert!(updates_rx.try_recv().is_err());

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            name: "route-foo".into(),
            kind: k8s_gateway_api::HttpRoute::kind(&()),
            group: k8s_gateway_api::HttpRoute::group(&()),
        },
    };
    let parent = k8s_gateway_api::ParentReference {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("Server".to_string()),
        namespace: None,
        name: "srv-8080".to_string(),
        section_name: None,
        port: None,
    };
    let route = make_gateway_route(&id, parent, None);

    // Apply the route
    index.write().apply(route);

    // Create the expected update.
    let parent_status =
        make_parent_status(&id.namespace, "srv-8080", "Accepted", "True", "Accepted");
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the HTTPRoute is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    {
        let mut index = index.write();
        <Index as IndexNamespacedResource<linkerd_k8s_api::Server>>::delete(
            &mut index,
            "ns-0".to_string(),
            "srv-8080".to_string(),
        );
    }

    // Create the expected update.
    let parent_status =
        make_parent_status("ns-0", "srv-8080", "Accepted", "False", "NoMatchingParent");
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The third update will be that the HTTPRoute is not accepted because the
    // Server has been deleted.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err());
}

fn make_status(
    parents: Vec<k8s_gateway_api::RouteParentStatus>,
) -> k8s_gateway_api::HttpRouteStatus {
    k8s_gateway_api::HttpRouteStatus {
        inner: k8s_gateway_api::RouteStatus { parents },
    }
}

fn make_linkerd_route(
    id: &NamespaceGroupKindName,
    parent: linkerd_k8s_api::httproute::ParentReference,
    backends: Option<Vec<linkerd_k8s_api::httproute::HttpBackendRef>>,
) -> linkerd_k8s_api::HttpRoute {
    linkerd_k8s_api::HttpRoute {
        metadata: k8s_core_api::ObjectMeta {
            namespace: Some(id.namespace.clone()),
            name: Some(id.gkn.name.to_string()),
            creation_timestamp: Some(k8s_core_api::Time(Utc::now())),
            ..Default::default()
        },
        spec: linkerd_k8s_api::HttpRouteSpec {
            inner: linkerd_k8s_api::httproute::CommonRouteSpec {
                parent_refs: Some(vec![parent]),
            },
            hostnames: None,
            rules: Some(vec![linkerd_k8s_api::httproute::HttpRouteRule {
                matches: Some(vec![linkerd_k8s_api::httproute::HttpRouteMatch {
                    path: Some(linkerd_k8s_api::httproute::HttpPathMatch::PathPrefix {
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

fn make_gateway_route(
    id: &NamespaceGroupKindName,
    parent: k8s_gateway_api::ParentReference,
    backends: Option<Vec<k8s_gateway_api::HttpBackendRef>>,
) -> k8s_gateway_api::HttpRoute {
    k8s_gateway_api::HttpRoute {
        status: None,
        metadata: k8s_core_api::ObjectMeta {
            name: Some(id.gkn.name.to_string()),
            namespace: Some(id.namespace.clone()),
            creation_timestamp: Some(k8s_core_api::Time(Utc::now())),
            ..Default::default()
        },
        spec: k8s_gateway_api::HttpRouteSpec {
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![parent]),
            },
            hostnames: None,
            rules: Some(vec![k8s_gateway_api::HttpRouteRule {
                filters: None,
                backend_refs: backends,
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

fn make_parent_status(
    namespace: impl ToString,
    name: impl ToString,
    type_: impl ToString,
    status: impl ToString,
    reason: impl ToString,
) -> k8s_gateway_api::RouteParentStatus {
    let condition = k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
        message: "".to_string(),
        observed_generation: None,
        reason: reason.to_string(),
        status: status.to_string(),
        type_: type_.to_string(),
    };
    k8s_gateway_api::RouteParentStatus {
        parent_ref: k8s_gateway_api::ParentReference {
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
