#[cfg(test)]
use crate::{
    index::{GRPCRouteRef, HTTPRouteRef},
    resource_id::NamespaceGroupKindName,
    routes,
};

use crate::{
    index::{accepted, in_cluster_net_overlap, SharedIndex, TCPRouteRef, TLSRouteRef},
    resource_id::NamespaceGroupKindName,
    tests::default_cluster_networks,
    Index, IndexMetrics,
};
use crate::{resource_id::ResourceId, Index};
use ahash::HashMap;
use chrono::{DateTime, Utc};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::routes::GroupKindName;
use linkerd_policy_controller_core::routes::GroupKindName;
use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api,
    policy::{self as linkerd_k8s_api, EgressNetworkStatus},
    Resource,
};
use linkerd_policy_controller_k8s_api::{gateway, Resource};
use std::vec;
use std::{sync::Arc, vec};
use tokio::sync::{mpsc, watch};

enum ParentRefType {
    Service,
    EgressNetwork,
}

fn make_index() -> SharedIndex {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, _) = mpsc::channel(10000);
    Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
        default_cluster_networks(),
    )
}

fn grpc_route_no_conflict(p: ParentRefType) {
    let index = make_index();

    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    index.write().update_grpc_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::grpcroutes::GRPCRoute::group(&()),
                kind: gateway::grpcroutes::GRPCRoute::kind(&()),
                name: "grpc-1".into(),
            },
        },
        &GRPCRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );
    index.write().update_http_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::httproutes::HTTPRoute::group(&()),
                kind: gateway::httproutes::HTTPRoute::kind(&()),
                name: "http-1".into(),
            },
        },
        &HTTPRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );
    index.write().update_tls_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::tlsroutes::TLSRoute::group(&()),
                kind: gateway::tlsroutes::TLSRoute::kind(&()),
                name: "tls-1".into(),
            },
        },
        &TLSRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );
    index.write().update_tcp_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::tcproutes::TCPRoute::group(&()),
                kind: gateway::tcproutes::TCPRoute::kind(&()),
                name: "tcp-1".into(),
            },
        },
        &TCPRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );

    assert!(!index
        .read()
        .parent_has_conflicting_routes(&parent, "GRPCRoute"));
}

fn http_route_conflict_grpc(p: ParentRefType) {
    let index = make_index();

    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    index.write().update_grpc_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::grpcroutes::GRPCRoute::group(&()),
                kind: gateway::grpcroutes::GRPCRoute::kind(&()),
                name: "grpc-1".into(),
            },
        },
        &GRPCRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );

    assert!(index
        .read()
        .parent_has_conflicting_routes(&parent, "HTTPRoute"));
}

fn http_route_no_conflict(p: ParentRefType) {
    let index = make_index();

    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    index.write().update_http_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::httproutes::HTTPRoute::group(&()),
                kind: gateway::httproutes::HTTPRoute::kind(&()),
                name: "http-1".into(),
            },
        },
        &HTTPRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );
    index.write().update_tls_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::tlsroutes::TLSRoute::group(&()),
                kind: gateway::tlsroutes::TLSRoute::kind(&()),
                name: "tls-1".into(),
            },
        },
        &TLSRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );
    index.write().update_tcp_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::tcproutes::TCPRoute::group(&()),
                kind: gateway::tcproutes::TCPRoute::kind(&()),
                name: "tcp-1".into(),
            },
        },
        &TCPRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );

    assert!(!index
        .read()
        .parent_has_conflicting_routes(&parent, "HTTPRoute"));
}

fn tls_route_conflict_http(p: ParentRefType) {
    let index = make_index();

    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    index.write().update_http_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::httproutes::HTTPRoute::group(&()),
                kind: gateway::httproutes::HTTPRoute::kind(&()),
                name: "http-1".into(),
            },
        },
        &HTTPRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );

    assert!(index
        .read()
        .parent_has_conflicting_routes(&parent, "TLSRoute"));
}

fn tls_route_conflict_grpc(p: ParentRefType) {
    let index = make_index();

    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    index.write().update_grpc_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::grpcroutes::GRPCRoute::group(&()),
                kind: gateway::grpcroutes::GRPCRoute::kind(&()),
                name: "grpc-1".into(),
            },
        },
        &GRPCRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );

    assert!(index
        .read()
        .parent_has_conflicting_routes(&parent, "TLSRoute"));
}

fn tls_route_no_conflict(p: ParentRefType) {
    let index = make_index();

    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    index.write().update_tls_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::tlsroutes::TLSRoute::group(&()),
                kind: gateway::tlsroutes::TLSRoute::kind(&()),
                name: "tls-1".into(),
            },
        },
        &TLSRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );
    index.write().update_tcp_route(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::tcproutes::TCPRoute::group(&()),
                kind: gateway::tcproutes::TCPRoute::kind(&()),
                name: "tcp-1".into(),
            },
        },
        &TCPRouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    );

    assert!(!index
        .read()
        .parent_has_conflicting_routes(&parent, "TLSRoute"));
}

fn tcp_route_conflict_grpc(p: ParentRefType) {
    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    let known_routes: HashMap<_, _> = vec![(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::grpcroutes::GRPCRoute::group(&()),
                kind: gateway::grpcroutes::GRPCRoute::kind(&()),
                name: "grpc-1".into(),
            },
        },
        RouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    )]
    .into_iter()
    .collect();

    assert!(parent_has_conflicting_routes(
        &mut known_routes.iter(),
        &parent,
        "TCPRoute"
    ));
}

fn tcp_route_conflict_http(p: ParentRefType) {
    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    let known_routes: HashMap<_, _> = vec![(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::httproutes::HTTPRoute::group(&()),
                kind: gateway::httproutes::HTTPRoute::kind(&()),
                name: "http-1".into(),
            },
        },
        RouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    )]
    .into_iter()
    .collect();

    assert!(parent_has_conflicting_routes(
        &mut known_routes.iter(),
        &parent,
        "TCPRoute"
    ));
}

fn tcp_route_conflict_tls(p: ParentRefType) {
    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    let known_routes: HashMap<_, _> = vec![(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::tlsroutes::TLSRoute::group(&()),
                kind: gateway::tlsroutes::TLSRoute::kind(&()),
                name: "tls-1".into(),
            },
        },
        RouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    )]
    .into_iter()
    .collect();

    assert!(parent_has_conflicting_routes(
        &mut known_routes.iter(),
        &parent,
        "TCPRoute"
    ));
}

fn tcp_route_no_conflict(p: ParentRefType) {
    let parent = match p {
        ParentRefType::Service => routes::ParentReference::Service(
            ResourceId::new("ns".to_string(), "service".to_string()),
            None,
        ),

        ParentRefType::EgressNetwork => routes::ParentReference::EgressNetwork(
            ResourceId::new("ns".to_string(), "egress-net".to_string()),
            None,
        ),
    };

    let known_routes: HashMap<_, _> = vec![(
        NamespaceGroupKindName {
            namespace: "default".to_string(),
            gkn: GroupKindName {
                group: gateway::tcproutes::TCPRoute::group(&()),
                kind: gateway::tcproutes::TCPRoute::kind(&()),
                name: "tcp-1".into(),
            },
        },
        RouteRef {
            parents: vec![parent.clone()],
            statuses: vec![],
            backends: vec![],
        },
    )]
    .into_iter()
    .collect();

    assert!(!parent_has_conflicting_routes(
        &mut known_routes.iter(),
        &parent,
        "TCPRoute"
    ));
}

#[test]
fn grpc_route_no_conflict_service() {
    grpc_route_no_conflict(ParentRefType::Service)
}

#[test]
fn http_route_conflict_grpc_service() {
    http_route_conflict_grpc(ParentRefType::Service)
}

#[test]
fn http_route_no_conflict_service() {
    http_route_no_conflict(ParentRefType::Service)
}

#[test]
fn tls_route_conflict_http_service() {
    tls_route_conflict_http(ParentRefType::Service)
}

#[test]
fn tls_route_conflict_grpc_service() {
    tls_route_conflict_grpc(ParentRefType::Service)
}

#[test]
fn tls_route_no_conflict_service() {
    tls_route_no_conflict(ParentRefType::Service)
}

#[test]
fn tcp_route_conflict_grpc_service() {
    tcp_route_conflict_grpc(ParentRefType::Service)
}

#[test]
fn tcp_route_conflict_http_service() {
    tcp_route_conflict_http(ParentRefType::Service)
}

#[test]
fn tcp_route_conflict_tls_service() {
    tcp_route_conflict_tls(ParentRefType::Service)
}

#[test]
fn tcp_route_no_conflict_service() {
    tcp_route_no_conflict(ParentRefType::Service)
}

#[test]
fn grpc_route_no_conflict_egress_network() {
    grpc_route_no_conflict(ParentRefType::EgressNetwork)
}

#[test]
fn http_route_conflict_grpc_egress_network() {
    http_route_conflict_grpc(ParentRefType::EgressNetwork)
}

#[test]
fn http_route_no_conflict_egress_network() {
    http_route_no_conflict(ParentRefType::EgressNetwork)
}

#[test]
fn tls_route_conflict_http_egress_network() {
    tls_route_conflict_http(ParentRefType::EgressNetwork)
}

#[test]
fn tls_route_conflict_grpc_egress_network() {
    tls_route_conflict_grpc(ParentRefType::EgressNetwork)
}

#[test]
fn tls_route_no_conflict_egress_network() {
    tls_route_no_conflict(ParentRefType::EgressNetwork)
}

#[test]
fn tcp_route_conflict_grpc_egress_network() {
    tcp_route_conflict_grpc(ParentRefType::EgressNetwork)
}

#[test]
fn tcp_route_conflict_http_egress_network() {
    tcp_route_conflict_http(ParentRefType::EgressNetwork)
}

#[test]
fn tcp_route_conflict_tls_egress_network() {
    tcp_route_conflict_tls(ParentRefType::EgressNetwork)
}

#[test]
fn tcp_route_no_conflict_egress_network() {
    tcp_route_no_conflict(ParentRefType::EgressNetwork)
}
