use crate::{
    index::{self, POLICY_API_GROUP},
    resource_id::NamespaceGroupKindName,
    Index, IndexMetrics,
};
use k8s::Resource;
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{http_route::GroupKindName, POLICY_CONTROLLER_NAME};
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
    let http_route = make_route("ns-0", "route-foo", "srv-8080");
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

    let http_route = make_route("ns-0", "route-foo", "srv-8080");
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

fn make_route(
    namespace: impl ToString,
    name: impl ToString,
    server: impl ToString,
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
                backend_refs: None,
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
