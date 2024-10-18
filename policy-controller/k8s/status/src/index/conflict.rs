use super::RouteRef;
use crate::{resource_id::NamespaceGroupKindName, routes};

use linkerd_policy_controller_k8s_api::{gateway as k8s_gateway_api, Resource};

// This method determines whether a parent that a route attempts to
// attach to has any routes attached that are in conflict with the one
// that we are about to attach. This is done following the logs outlined in:
// https://gateway-api.sigs.k8s.io/geps/gep-1426/#route-types
pub(super) fn parent_has_conflicting_routes<'p>(
    existing_routes: impl Iterator<Item = (&'p NamespaceGroupKindName, &'p RouteRef)>,
    parent_ref: &routes::ParentReference,
    candidate_kind: &str,
) -> bool {
    let grpc_kind = k8s_gateway_api::GrpcRoute::kind(&());
    let http_kind = k8s_gateway_api::HttpRoute::kind(&());
    let tls_kind = k8s_gateway_api::TlsRoute::kind(&());
    let tcp_kind = k8s_gateway_api::TcpRoute::kind(&());

    let mut siblings = existing_routes.filter(|(_, route)| route.parents.contains(parent_ref));
    siblings.any(|(id, _sibling)| {
        if *candidate_kind == grpc_kind {
            false
        } else if *candidate_kind == http_kind {
            id.gkn.kind == grpc_kind
        } else if *candidate_kind == tls_kind {
            id.gkn.kind == grpc_kind || id.gkn.kind == http_kind
        } else if *candidate_kind == tcp_kind {
            id.gkn.kind == grpc_kind || id.gkn.kind == http_kind || id.gkn.kind == tls_kind
        } else {
            false
        }
    })
}

#[cfg(test)]
mod test {
    use super::*;
    use crate::resource_id::ResourceId;
    use ahash::HashMap;
    use linkerd_policy_controller_core::routes::GroupKindName;
    use linkerd_policy_controller_k8s_api::gateway as k8s_gateway_api;
    use std::vec;

    enum ParentRefType {
        Service,
        EgressNetwork,
    }

    fn grpc_route_no_conflict(p: ParentRefType) {
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

        let known_routes: HashMap<_, _> = vec![
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::GrpcRoute::group(&()),
                        kind: k8s_gateway_api::GrpcRoute::kind(&()),
                        name: "grpc-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::HttpRoute::group(&()),
                        kind: k8s_gateway_api::HttpRoute::kind(&()),
                        name: "http-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TlsRoute::group(&()),
                        kind: k8s_gateway_api::TlsRoute::kind(&()),
                        name: "tls-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TcpRoute::group(&()),
                        kind: k8s_gateway_api::TcpRoute::kind(&()),
                        name: "tcp-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
        ]
        .into_iter()
        .collect();

        assert!(!parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "GRPCRoute"
        ));
    }

    fn http_route_conflict_grpc(p: ParentRefType) {
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
                    group: k8s_gateway_api::GrpcRoute::group(&()),
                    kind: k8s_gateway_api::GrpcRoute::kind(&()),
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
            "HTTPRoute"
        ));
    }

    fn http_route_no_conflict(p: ParentRefType) {
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

        let known_routes: HashMap<_, _> = vec![
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::HttpRoute::group(&()),
                        kind: k8s_gateway_api::HttpRoute::kind(&()),
                        name: "http-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TlsRoute::group(&()),
                        kind: k8s_gateway_api::TlsRoute::kind(&()),
                        name: "tls-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TcpRoute::group(&()),
                        kind: k8s_gateway_api::TcpRoute::kind(&()),
                        name: "tcp-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
        ]
        .into_iter()
        .collect();

        assert!(!parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "HTTPRoute"
        ));
    }

    fn tls_route_conflict_http(p: ParentRefType) {
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
                    group: k8s_gateway_api::HttpRoute::group(&()),
                    kind: k8s_gateway_api::HttpRoute::kind(&()),
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
            "TLSRoute"
        ));
    }

    fn tls_route_conflict_grpc(p: ParentRefType) {
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
                    group: k8s_gateway_api::GrpcRoute::group(&()),
                    kind: k8s_gateway_api::GrpcRoute::kind(&()),
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
            "TLSRoute"
        ));
    }

    fn tls_route_no_conflict(p: ParentRefType) {
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
        let known_routes: HashMap<_, _> = vec![
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TlsRoute::group(&()),
                        kind: k8s_gateway_api::TlsRoute::kind(&()),
                        name: "tls-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TcpRoute::group(&()),
                        kind: k8s_gateway_api::TcpRoute::kind(&()),
                        name: "tcp-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
        ]
        .into_iter()
        .collect();

        assert!(!parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "TLSRoute"
        ));
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
                    group: k8s_gateway_api::GrpcRoute::group(&()),
                    kind: k8s_gateway_api::GrpcRoute::kind(&()),
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
                    group: k8s_gateway_api::HttpRoute::group(&()),
                    kind: k8s_gateway_api::HttpRoute::kind(&()),
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
                    group: k8s_gateway_api::TlsRoute::group(&()),
                    kind: k8s_gateway_api::TlsRoute::kind(&()),
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
                    group: k8s_gateway_api::TcpRoute::group(&()),
                    kind: k8s_gateway_api::TcpRoute::kind(&()),
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
}
