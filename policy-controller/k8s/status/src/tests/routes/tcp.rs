use super::make_parent_status;
use crate::{
    index::{
        accepted, backend_not_found, invalid_backend_kind, no_matching_parent, resolved_refs,
        POLICY_API_GROUP,
    },
    resource_id::NamespaceGroupKindName,
    tests::default_cluster_networks,
    Index, IndexMetrics,
};
use chrono::{DateTime, Utc};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{routes::GroupKindName, POLICY_CONTROLLER_NAME};
use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api, gateway as k8s_gateway_api, policy as linkerd_k8s_api, Resource,
    ResourceExt,
};
use std::{sync::Arc, vec};
use tokio::sync::{mpsc, watch};

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
            group: k8s_gateway_api::TcpRoute::group(&()),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![
            k8s_gateway_api::BackendRef {
                inner: k8s_gateway_api::BackendObjectReference {
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    name: backend1.name_unchecked(),
                    namespace: backend1.namespace(),
                    port: Some(8080),
                },
                weight: None,
            },
            k8s_gateway_api::BackendRef {
                inner: k8s_gateway_api::BackendObjectReference {
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
    let parent = k8s_gateway_api::ParentReference {
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
            group: k8s_gateway_api::TcpRoute::group(&()),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![k8s_gateway_api::BackendRef {
            inner: k8s_gateway_api::BackendObjectReference {
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
            group: k8s_gateway_api::TcpRoute::group(&()),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![
            k8s_gateway_api::BackendRef {
                inner: k8s_gateway_api::BackendObjectReference {
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    name: backend.name_unchecked(),
                    namespace: backend.namespace(),
                    port: Some(8080),
                },
                weight: None,
            },
            k8s_gateway_api::BackendRef {
                inner: k8s_gateway_api::BackendObjectReference {
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
    let parent = k8s_gateway_api::ParentReference {
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
            group: k8s_gateway_api::TcpRoute::group(&()),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![k8s_gateway_api::BackendRef {
            inner: k8s_gateway_api::BackendObjectReference {
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
            group: k8s_gateway_api::TcpRoute::group(&()),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![k8s_gateway_api::BackendRef {
            inner: k8s_gateway_api::BackendObjectReference {
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
    let parent = k8s_gateway_api::ParentReference {
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
            group: k8s_gateway_api::TcpRoute::group(&()),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let route = make_route(
        &id,
        parent.clone(),
        vec![k8s_gateway_api::BackendRef {
            inner: k8s_gateway_api::BackendObjectReference {
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
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            group: k8s_gateway_api::TcpRoute::group(&()),
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
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The first update will be that the TLSRoute is not accepted because the
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
        Some(linkerd_k8s_api::server::ProxyProtocol::Http2),
    );
    index.write().apply(server);

    // Create the expected update.
    let parent_status =
        make_parent_status(&id.namespace, "srv-8080", "Accepted", "True", "Accepted");
    let status = make_status(vec![parent_status]);
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
            group: k8s_gateway_api::TcpRoute::group(&()),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent = linkerd_k8s_api::httproute::ParentReference {
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
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent.clone(),
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition.clone()],
    };

    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The first update will be that the TCPRoute is not accepted because the
    // EgressNetwork has not been created yet.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    // Apply the egress network
    let egress = super::make_egress_network("ns-0", "egress", accepted());
    index.write().apply(egress);

    // Create the expected update.
    let accepted_condition = accepted();
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent,
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition],
    };

    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the TCPRoute is accepted because the
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

    let server = super::make_server(
        "ns-0",
        "srv-8080",
        8080,
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(linkerd_k8s_api::server::ProxyProtocol::Http2),
    );
    index.write().apply(server);

    // There should be no update since there are no TLSRoutes yet.
    assert!(updates_rx.try_recv().is_err());

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            name: "route-foo".into(),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            group: k8s_gateway_api::TcpRoute::group(&()),
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
    let route = make_route(&id, parent, vec![]);

    // Apply the route
    index.write().apply(route);

    // Create the expected update.
    let parent_status =
        make_parent_status(&id.namespace, "srv-8080", "Accepted", "True", "Accepted");
    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the TCPRoute is accepted because the
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

    // The third update will be that the TCPRoute is not accepted because the
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

    // There should be no update since there are no TCPRoutes yet.
    assert!(updates_rx.try_recv().is_err());

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            group: k8s_gateway_api::TcpRoute::group(&()),
            kind: k8s_gateway_api::TcpRoute::kind(&()),
            name: "route-foo".into(),
        },
    };
    let parent = linkerd_k8s_api::httproute::ParentReference {
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
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent.clone(),
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![accepted_condition, backend_condition.clone()],
    };

    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The second update will be that the TCPRoute is accepted because the
    // EgressNetwork has been created.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);

    {
        let mut index = index.write();
        <Index as IndexNamespacedResource<linkerd_k8s_api::EgressNetwork>>::delete(
            &mut index,
            "ns-0".to_string(),
            "egress".to_string(),
        );
    }

    // Create the expected update.
    let rejected_condition = no_matching_parent();
    let parent_status = k8s_gateway_api::RouteParentStatus {
        parent_ref: parent.clone(),
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
        conditions: vec![rejected_condition, backend_condition.clone()],
    };

    let status = make_status(vec![parent_status]);
    let patch = crate::index::make_patch(&id, status).unwrap();

    // The third update will be that the TCPRoute is not accepted because the
    // Server has been deleted.
    let update = updates_rx.try_recv().unwrap();
    assert_eq!(id, update.id);
    assert_eq!(patch, update.patch);
    assert!(updates_rx.try_recv().is_err());
}

fn make_route(
    id: &NamespaceGroupKindName,
    parent: k8s_gateway_api::ParentReference,
    backends: Vec<k8s_gateway_api::BackendRef>,
) -> k8s_gateway_api::TcpRoute {
    k8s_gateway_api::TcpRoute {
        status: None,
        metadata: k8s_core_api::ObjectMeta {
            name: Some(id.gkn.name.to_string()),
            namespace: Some(id.namespace.clone()),
            creation_timestamp: Some(k8s_core_api::Time(Utc::now())),
            ..Default::default()
        },
        spec: k8s_gateway_api::TcpRouteSpec {
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![parent]),
            },
            rules: vec![k8s_gateway_api::TcpRouteRule {
                backend_refs: backends,
            }],
        },
    }
}

fn make_status(
    parents: Vec<k8s_gateway_api::RouteParentStatus>,
) -> k8s_gateway_api::TcpRouteStatus {
    k8s_gateway_api::TcpRouteStatus {
        inner: k8s_gateway_api::RouteStatus { parents },
    }
}
