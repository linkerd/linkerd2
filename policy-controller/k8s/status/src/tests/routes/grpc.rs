use crate::{index::POLICY_API_GROUP, resource_id::NamespaceGroupKindName, Index, IndexMetrics};
use chrono::{DateTime, Utc};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{routes::GroupKindName, POLICY_CONTROLLER_NAME};
use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api, gateway as k8s_gateway_api, policy as linkerd_k8s_api, Resource,
};
use std::sync::Arc;
use tokio::sync::{mpsc, watch};

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
    );

    // Create the route id and route
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            name: "route-foo".into(),
            kind: k8s_gateway_api::GrpcRoute::kind(&()),
            group: k8s_gateway_api::GrpcRoute::group(&()),
        },
    };
    let route = make_route(&id, "srv-8080");

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
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            name: "route-foo".into(),
            kind: k8s_gateway_api::GrpcRoute::kind(&()),
            group: k8s_gateway_api::GrpcRoute::group(&()),
        },
    };
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
            kind: k8s_gateway_api::GrpcRoute::kind(&()),
            group: k8s_gateway_api::GrpcRoute::group(&()),
        },
    };
    let route = make_route(&id, "srv-8080");

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
    let id = NamespaceGroupKindName {
        namespace: "ns-0".to_string(),
        gkn: GroupKindName {
            name: "route-foo".into(),
            kind: k8s_gateway_api::GrpcRoute::kind(&()),
            group: k8s_gateway_api::GrpcRoute::group(&()),
        },
    };
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

fn make_route(id: &NamespaceGroupKindName, server: impl ToString) -> k8s_gateway_api::GrpcRoute {
    k8s_gateway_api::GrpcRoute {
        status: None,
        metadata: k8s_core_api::ObjectMeta {
            name: Some(id.gkn.name.to_string()),
            namespace: Some(id.namespace.clone()),
            creation_timestamp: Some(k8s_core_api::Time(Utc::now())),
            ..Default::default()
        },
        spec: k8s_gateway_api::GrpcRouteSpec {
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                    port: None,
                    namespace: None,
                    section_name: None,
                    name: server.to_string(),
                    kind: Some("Server".to_string()),
                    group: Some(POLICY_API_GROUP.to_string()),
                }]),
            },
            hostnames: None,
            rules: Some(vec![k8s_gateway_api::GrpcRouteRule {
                filters: None,
                backend_refs: None,
                matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                    headers: None,
                    method: Some(k8s_gateway_api::GrpcMethodMatch::Exact {
                        method: Some("MakeRoute".to_string()),
                        service: Some("io.linkerd.Test".to_string()),
                    }),
                }]),
            }]),
        },
    }
}

fn make_status(
    parents: Vec<k8s_gateway_api::RouteParentStatus>,
) -> k8s_gateway_api::GrpcRouteStatus {
    k8s_gateway_api::GrpcRouteStatus {
        inner: k8s_gateway_api::RouteStatus { parents },
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
        message: "".to_string(),
        type_: type_.to_string(),
        observed_generation: None,
        reason: reason.to_string(),
        status: status.to_string(),
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
    };
    k8s_gateway_api::RouteParentStatus {
        conditions: vec![condition],
        parent_ref: k8s_gateway_api::ParentReference {
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
