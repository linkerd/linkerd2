use crate::{
    index::{
        accepted, backend_not_found, invalid_backend_kind, no_matching_parent, resolved_refs,
        route_conflicted, POLICY_API_GROUP,
    },
    resource_id::NamespaceGroupKindName,
    tests::{default_cluster_networks, make_server},
    Index, IndexMetrics,
};
use chrono::{DateTime, Utc};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{routes::GroupKindName, POLICY_CONTROLLER_NAME};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy, Resource, ResourceExt};
use std::{sync::Arc, vec};
use tokio::sync::{mpsc, watch};

pub(crate) fn make_parent_status(
    namespace: impl ToString,
    name: impl ToString,
    type_: impl ToString,
    status: impl ToString,
    reason: impl ToString,
) -> gateway::TLSRouteStatusParents {
    let condition = k8s::Condition {
        message: "".to_string(),
        type_: type_.to_string(),
        observed_generation: None,
        reason: reason.to_string(),
        status: status.to_string(),
        last_transition_time: k8s::Time(DateTime::<Utc>::MIN_UTC),
    };
    gateway::TLSRouteStatusParents {
        conditions: vec![condition],
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            port: None,
            section_name: None,
            name: name.to_string(),
            kind: Some("Server".to_string()),
            namespace: Some(namespace.to_string()),
            group: Some(POLICY_API_GROUP.to_string()),
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
    }
}

#[test]
fn route_with_valid_service_backends() {
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
        default_cluster_networks(),
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
    let parent = gateway::TLSRouteParentRefs {
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
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![
            gateway::TLSRouteRulesBackendRefs {
                inner: gateway::BackendObjectReference {
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    name: backend1.name_unchecked(),
                    namespace: backend1.namespace(),
                    port: Some(8080),
                },
                weight: None,
            },
            gateway::TLSRouteRulesBackendRefs {
                inner: gateway::BackendObjectReference {
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    name: backend2.name_unchecked(),
                    namespace: backend2.namespace(),
                    port: Some(8080),
                },
                weight: None,
            },
        ],
    );
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = accepted();
    // All backends exist and can be resolved.
    let backend_condition = resolved_refs();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group,
            kind: parent.kind,
            name: parent.name,
            namespace: parent.namespace,
            port: parent.port,
            section_name: parent.section_name,
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn route_with_valid_egress_network_backend() {
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
        default_cluster_networks(),
    );

    // Apply the parent egress network
    let parent = super::make_egress_network("ns-0", "egress", accepted());
    index.write().apply(parent.clone());

    // Apply the route.
    let parent = gateway::TLSRouteParentRefs {
        group: Some("policy.linkerd.io".to_string()),
        kind: Some("EgressNetwork".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![gateway::TLSRouteRulesBackendRefs {
            inner: gateway::BackendObjectReference {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                name: parent.name.clone(),
                namespace: parent.namespace.clone(),
                port: Some(8080),
            },
            weight: None,
        }],
    );
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = accepted();
    // All backends exist and can be resolved.
    let backend_condition = resolved_refs();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group,
            kind: parent.kind,
            name: parent.name,
            namespace: parent.namespace,
            port: parent.port,
            section_name: parent.section_name,
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn route_with_invalid_service_backend() {
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
        default_cluster_networks(),
    );

    // Apply the parent service
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply one backend service
    let backend = super::make_service("ns-0", "backend-1");
    index.write().apply(backend.clone());

    // Apply the route.
    let parent = gateway::TLSRouteParentRefs {
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
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![
            gateway::TLSRouteRulesBackendRefs {
                inner: gateway::BackendObjectReference {
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    name: backend.name_unchecked(),
                    namespace: backend.namespace(),
                    port: Some(8080),
                },
                weight: None,
            },
            gateway::TLSRouteRulesBackendRefs {
                inner: gateway::BackendObjectReference {
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    name: "nonexistant-backend".to_string(),
                    namespace: backend.namespace(),
                    port: Some(8080),
                },
                weight: None,
            },
        ],
    );
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = accepted();
    // One of the backends does not exist so the status should be BackendNotFound.
    let backend_condition = backend_not_found();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group,
            kind: parent.kind,
            name: parent.name,
            namespace: parent.namespace,
            port: parent.port,
            section_name: parent.section_name,
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn route_with_egress_network_backend_different_from_parent() {
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
        default_cluster_networks(),
    );

    // Apply the parent egress network
    let parent = super::make_egress_network("ns-0", "svc", accepted());
    index.write().apply(parent.clone());

    // Apply one backend egress network
    let backend = super::make_egress_network("ns-0", "backend-1", accepted());
    index.write().apply(backend.clone());

    // Apply the route.
    let parent = gateway::TLSRouteParentRefs {
        group: Some("policy.linkerd.io".to_string()),
        kind: Some("EgressNetwork".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![gateway::TLSRouteRulesBackendRefs {
            inner: gateway::BackendObjectReference {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                name: backend.name_unchecked(),
                namespace: backend.namespace(),
                port: Some(8080),
            },
            weight: None,
        }],
    );
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = accepted();
    let backend_condition = invalid_backend_kind(
        "EgressNetwork backend needs to be on a route that has an EgressNetwork parent",
    );
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group,
            kind: parent.kind,
            name: parent.name,
            namespace: parent.namespace,
            port: parent.port,
            section_name: parent.section_name,
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn route_with_egress_network_backend_and_service_parent() {
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
        default_cluster_networks(),
    );

    // Apply the parent service
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    // Apply one backend egress network
    let backend = super::make_egress_network("ns-0", "backend-1", accepted());
    index.write().apply(backend.clone());

    // Apply the route.
    let parent = gateway::TLSRouteParentRefs {
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
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![gateway::TLSRouteRulesBackendRefs {
            inner: gateway::BackendObjectReference {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                name: backend.name_unchecked(),
                namespace: backend.namespace(),
                port: Some(8080),
            },
            weight: None,
        }],
    );
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = accepted();
    let backend_condition = invalid_backend_kind(
        "EgressNetwork backend needs to be on a route that has an EgressNetwork parent",
    );
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group,
            kind: parent.kind,
            name: parent.name,
            namespace: parent.namespace,
            port: parent.port,
            section_name: parent.section_name,
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn route_with_egress_network_parent_and_service_backend() {
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
        default_cluster_networks(),
    );

    // Apply the parent egress network
    let parent = super::make_egress_network("ns-0", "egress", accepted());
    index.write().apply(parent.clone());

    // Apply one backend service
    let backend = super::make_service("ns-0", "backend-1");
    index.write().apply(backend.clone());

    // Apply the route.
    let parent = gateway::TLSRouteParentRefs {
        group: Some("policy.linkerd.io".to_string()),
        kind: Some("EgressNetwork".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };
    let id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![gateway::TLSRouteRulesBackendRefs {
            inner: gateway::BackendObjectReference {
                group: Some("core".to_string()),
                kind: Some("Service".to_string()),
                name: backend.name_unchecked(),
                namespace: backend.namespace(),
                port: Some(8080),
            },
            weight: None,
        }],
    );
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = accepted();
    let backend_condition = resolved_refs();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group,
            kind: parent.kind,
            name: parent.name,
            namespace: parent.namespace,
            port: parent.port,
            section_name: parent.section_name,
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn route_accepted_after_server_create() {
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
        default_cluster_networks(),
    );

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            name: "route-foo".into(),
            kind: gateway::TLSRoute::kind(&()),
            group: gateway::TLSRoute::group(&()),
        },
    };
    let parent = gateway::TLSRouteParentRefs {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("Server".to_string()),
        namespace: None,
        name: "srv-8080".to_string(),
        section_name: None,
        port: None,
    };
    let route = make_route(&id, parent, vec![]);

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
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The first update will be that the TLSRoute is not accepted because the
    // Server has been created yet.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the server
    let server = make_server(
        "ns-0",
        "srv-8080",
        8080,
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(policy::server::ProxyProtocol::Http2),
    );
    index.write().apply(server);

    // Create the expected update.
    let parent_status =
        make_parent_status(&id.namespace, "srv-8080", "Accepted", "True", "Accepted");
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the TCPRoute is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn route_accepted_after_egress_network_create() {
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
        default_cluster_networks(),
    );

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent = gateway::TLSRouteParentRefs {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("EgressNetwork".to_string()),
        namespace: Some("ns-0".to_string()),
        name: "egress".to_string(),
        section_name: None,
        port: None,
    };
    let route = make_route(&id, parent.clone(), vec![]);

    // Apply the route.
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = no_matching_parent();
    let backend_condition = resolved_refs();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group.clone(),
            kind: parent.kind.clone(),
            name: parent.name.clone(),
            namespace: parent.namespace.clone(),
            port: parent.port,
            section_name: parent.section_name.clone(),
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition.clone()],
    };

    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The first update will be that the TLSRoute is not accepted because the
    // EgressNetwork has not been created yet.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the egress network
    let egress = super::make_egress_network("ns-0", "egress", accepted());
    index.write().apply(egress);

    // Create the expected update.
    let accepted_condition = accepted();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group,
            kind: parent.kind,
            name: parent.name,
            namespace: parent.namespace,
            port: parent.port,
            section_name: parent.section_name,
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };

    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the TLSRoute is accepted because the
    // EgressNetwork has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn route_rejected_after_server_delete() {
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
        default_cluster_networks(),
    );

    let server = make_server(
        "ns-0",
        "srv-8080",
        8080,
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(policy::server::ProxyProtocol::Http2),
    );
    index.write().apply(server);

    // There should be no update since there are no TLSRoutes yet.
    assert!(updates_rx.try_recv().is_err());

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            name: "route-foo".into(),
            kind: gateway::TLSRoute::kind(&()),
            group: gateway::TLSRoute::group(&()),
        },
    };
    let parent = gateway::TLSRouteParentRefs {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("Server".to_string()),
        namespace: None,
        name: "srv-8080".to_string(),
        section_name: None,
        port: None,
    };
    let route = make_route(&id, parent, vec![]);

    // Apply the route
    index.write().apply(route);

    // Create the expected update.
    let parent_status =
        make_parent_status(&id.namespace, "srv-8080", "Accepted", "True", "Accepted");
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the TLSRoutes is accepted because the
    // Server has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    {
        let mut index = index.write();
        <Index as IndexNamespacedResource<policy::Server>>::delete(
            &mut index,
            "ns-0".to_string(),
            "srv-8080".to_string(),
        );
    }

    // Create the expected update.
    let parent_status =
        make_parent_status("ns-0", "srv-8080", "Accepted", "False", "NoMatchingParent");
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The third update will be that the TLSRoutes is not accepted because the
    // Server has been deleted.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err());
}

#[test]
fn route_rejected_after_egress_network_delete() {
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
        default_cluster_networks(),
    );

    let egress = super::make_egress_network("ns-0", "egress", accepted());
    index.write().apply(egress);

    // There should be no update since there are no TLSRoutes yet.
    assert!(updates_rx.try_recv().is_err());

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent = gateway::TLSRouteParentRefs {
        group: Some(POLICY_API_GROUP.to_string()),
        kind: Some("EgressNetwork".to_string()),
        namespace: Some("ns-0".to_string()),
        name: "egress".to_string(),
        section_name: None,
        port: None,
    };
    let route = make_route(&id, parent.clone(), vec![]);

    // Apply the route
    index.write().apply(route);

    // Create the expected update.
    let accepted_condition = accepted();
    let backend_condition = resolved_refs();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group.clone(),
            kind: parent.kind.clone(),
            name: parent.name.clone(),
            namespace: parent.namespace.clone(),
            port: parent.port,
            section_name: parent.section_name.clone(),
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition.clone()],
    };

    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the TLSRoute is accepted because the
    // EgressNetwork has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    {
        let mut index = index.write();
        <Index as IndexNamespacedResource<policy::EgressNetwork>>::delete(
            &mut index,
            "ns-0".to_string(),
            "egress".to_string(),
        );
    }

    // Create the expected update.
    let rejected_condition = no_matching_parent();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group.clone(),
            kind: parent.kind.clone(),
            name: parent.name.clone(),
            namespace: parent.namespace.clone(),
            port: parent.port,
            section_name: parent.section_name.clone(),
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![rejected_condition, backend_condition.clone()],
    };

    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The third update will be that the TLSRoute is not accepted because the
    // Server has been deleted.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err());
}

#[test]
fn service_route_type_conflict() {
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
        default_cluster_networks(),
    );

    // Apply the parent service
    let parent = super::make_service("ns-0", "svc");
    index.write().apply(parent.clone());

    let parent = gateway::TLSRouteParentRefs {
        group: Some("core".to_string()),
        kind: Some("Service".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };

    // Apply the TCP route.
    let tcp_id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: gateway::TCPRoute::group(&()),
            kind: gateway::TCPRoute::kind(&()),
            name: "tcproute-foo".into(),
        },
    };
    let tcp_route = gateway::TCPRoute {
        status: None,
        metadata: k8s::ObjectMeta {
            name: Some(tcp_id.gkn.name.to_string()),
            namespace: Some(tcp_id.namespace.clone()),
            creation_timestamp: Some(k8s::Time(Utc::now())),
            ..Default::default()
        },
        spec: gateway::TCPRouteSpec {
            inner: gateway::CommonRouteSpec {
                parent_refs: Some(vec![gateway::TCPRouteParentRefs {
                    group: parent.group.clone(),
                    kind: parent.kind.clone(),
                    name: parent.name.clone(),
                    namespace: parent.namespace.clone(),
                    port: parent.port,
                    section_name: parent.section_name.clone(),
                }]),
            },
            rules: vec![],
        },
    };
    index.write().apply(tcp_route);

    // Create the expected update -- TCPRoute should be accepted
    let accepted_condition = accepted();
    // No backends were specified, so we have vacuously resolved them all.
    let backend_condition = resolved_refs();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group.clone(),
            kind: parent.kind.clone(),
            name: parent.name.clone(),
            namespace: parent.namespace.clone(),
            port: parent.port,
            section_name: parent.section_name.clone(),
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition.clone(), backend_condition.clone()],
    };
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&tcp_id, status).unwrap();
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(tcp_id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the TLS route.
    let tls_id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "tlsroute-foo".into(),
        },
    };
    let route = make_route(&tls_id, parent.clone(), vec![]);
    index.write().apply(route);

    // Two expected updates: TCPRoute should be rejected and TLSRoute should be accepted
    for _ in 0..2 {
        let update = updates_rx.try_recv().unwrap();
        if update.id.gkn.kind == gateway::TCPRoute::kind(&()) {
            let conflict_condition = route_conflicted();
            let parent_status = gateway::TLSRouteStatusParents {
                parent_ref: gateway::TLSRouteStatusParentsParentRef {
                    group: parent.group.clone(),
                    kind: parent.kind.clone(),
                    name: parent.name.clone(),
                    namespace: parent.namespace.clone(),
                    port: parent.port,
                    section_name: parent.section_name.clone(),
                },
                controller_name: POLICY_CONTROLLER_NAME.to_string(),
                conditions: vec![conflict_condition, backend_condition.clone()],
            };
            let status = gateway::TLSRouteStatus {
                inner: gateway::RouteStatus {
                    parents: vec![parent_status],
                },
            };
            let patch = crate::index::make_patch(&tcp_id, status).unwrap();
            assert_eq!(patch, update.patch);
        } else {
            let parent_status = gateway::TLSRouteStatusParents {
                parent_ref: gateway::TLSRouteStatusParentsParentRef {
                    group: parent.group.clone(),
                    kind: parent.kind.clone(),
                    name: parent.name.clone(),
                    namespace: parent.namespace.clone(),
                    port: parent.port,
                    section_name: parent.section_name.clone(),
                },
                controller_name: POLICY_CONTROLLER_NAME.to_string(),
                conditions: vec![accepted_condition.clone(), backend_condition.clone()],
            };
            let status = gateway::TLSRouteStatus {
                inner: gateway::RouteStatus {
                    parents: vec![parent_status],
                },
            };
            let patch = crate::index::make_patch(&tls_id, status).unwrap();
            assert_eq!(patch, update.patch);
        }
    }

    // No more updates.
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn egress_network_route_type_conflict() {
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
        default_cluster_networks(),
    );

    // Apply the parent egress network
    let parent = super::make_egress_network("ns-0", "egress", accepted());
    index.write().apply(parent.clone());

    let parent = gateway::TLSRouteParentRefs {
        group: Some("policy.linkerd.io".to_string()),
        kind: Some("EgressNetwork".to_string()),
        namespace: parent.namespace(),
        name: parent.name_unchecked(),
        section_name: None,
        port: Some(8080),
    };

    // Apply the TCP route.
    let tcp_id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: gateway::TCPRoute::group(&()),
            kind: gateway::TCPRoute::kind(&()),
            name: "tcproute-foo".into(),
        },
    };
    let tcp_route = gateway::TCPRoute {
        status: None,
        metadata: k8s::ObjectMeta {
            name: Some(tcp_id.gkn.name.to_string()),
            namespace: Some(tcp_id.namespace.clone()),
            creation_timestamp: Some(k8s::Time(Utc::now())),
            ..Default::default()
        },
        spec: gateway::TCPRouteSpec {
            inner: gateway::CommonRouteSpec {
                parent_refs: Some(vec![gateway::TCPRouteParentRefs {
                    group: parent.group.clone(),
                    kind: parent.kind.clone(),
                    name: parent.name.clone(),
                    namespace: parent.namespace.clone(),
                    port: parent.port,
                    section_name: parent.section_name.clone(),
                }]),
            },
            rules: vec![],
        },
    };
    index.write().apply(tcp_route);

    // Create the expected update -- TCPRoute should be accepted
    let accepted_condition = accepted();
    // No backends were specified, so we have vacuously resolved them all.
    let backend_condition = resolved_refs();
    let parent_status = gateway::TLSRouteStatusParents {
        parent_ref: gateway::TLSRouteStatusParentsParentRef {
            group: parent.group.clone(),
            kind: parent.kind.clone(),
            name: parent.name.clone(),
            namespace: parent.namespace.clone(),
            port: parent.port,
            section_name: parent.section_name.clone(),
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition.clone(), backend_condition.clone()],
    };
    let status = gateway::TLSRouteStatus {
        inner: gateway::RouteStatus {
            parents: vec![parent_status],
        },
    };
    let patch = crate::index::make_patch(&tcp_id, status).unwrap();
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(tcp_id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the TLS route.
    let tls_id = NamespaceGroupKindName {
        namespace: parent.namespace.as_deref().unwrap().to_string(),
        gkn: GroupKindName {
            group: gateway::TLSRoute::group(&()),
            kind: gateway::TLSRoute::kind(&()),
            name: "tlsroute-foo".into(),
        },
    };
    let route = make_route(&tls_id, parent.clone(), vec![]);
    index.write().apply(route);

    // Two expected updates: TCP should be rejected and TLSRoute should be accepted
    for _ in 0..2 {
        let update = updates_rx.try_recv().unwrap();
        if update.id.gkn.kind == gateway::TCPRoute::kind(&()) {
            let conflict_condition = route_conflicted();
            let parent_status = gateway::TLSRouteStatusParents {
                parent_ref: gateway::TLSRouteStatusParentsParentRef {
                    group: parent.group.clone(),
                    kind: parent.kind.clone(),
                    name: parent.name.clone(),
                    namespace: parent.namespace.clone(),
                    port: parent.port,
                    section_name: parent.section_name.clone(),
                },
                controller_name: POLICY_CONTROLLER_NAME.to_string(),
                conditions: vec![conflict_condition, backend_condition.clone()],
            };
            let status = gateway::TLSRouteStatus {
                inner: gateway::RouteStatus {
                    parents: vec![parent_status],
                },
            };
            let patch = crate::index::make_patch(&tcp_id, status).unwrap();
            assert_eq!(patch, update.patch);
        } else {
            let parent_status = gateway::TLSRouteStatusParents {
                parent_ref: gateway::TLSRouteStatusParentsParentRef {
                    group: parent.group.clone(),
                    kind: parent.kind.clone(),
                    name: parent.name.clone(),
                    namespace: parent.namespace.clone(),
                    port: parent.port,
                    section_name: parent.section_name.clone(),
                },
                controller_name: POLICY_CONTROLLER_NAME.to_string(),
                conditions: vec![accepted_condition.clone(), backend_condition.clone()],
            };
            let status = gateway::TLSRouteStatus {
                inner: gateway::RouteStatus {
                    parents: vec![parent_status],
                },
            };
            let patch = crate::index::make_patch(&tls_id, status).unwrap();
            assert_eq!(patch, update.patch);
        }
    }

    // No more updates.
    assert!(updates_rx.try_recv().is_err())
}

fn make_route(
    id: &NamespaceGroupKindName,
    parent: gateway::TLSRouteParentRefs,
    backends: Vec<gateway::TLSRouteRulesBackendRefs>,
) -> gateway::TLSRoute {
    gateway::TLSRoute {
        status: None,
        metadata: k8s::ObjectMeta {
            name: Some(id.gkn.name.to_string()),
            namespace: Some(id.namespace.clone()),
            creation_timestamp: Some(k8s::Time(Utc::now())),
            ..Default::default()
        },
        spec: gateway::TLSRouteSpec {
            inner: gateway::CommonRouteSpec {
                parent_refs: Some(vec![parent]),
            },
            hostnames: None,
            rules: vec![gateway::TLSRouteRules {
                backend_refs: backends,
            }],
        },
    }
}
