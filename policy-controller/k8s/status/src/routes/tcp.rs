#[cfg(test)]
mod test {
    use crate::index::POLICY_API_GROUP;
    use crate::routes::BackendReference;
    use linkerd_policy_controller_k8s_api::{self as k8s_core_api, gateway as k8s_gateway_api};

    #[test]
    fn backendrefs_from_route() {
        let route = k8s_gateway_api::TcpRoute {
            metadata: k8s_core_api::ObjectMeta {
                namespace: Some("foo".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: k8s_gateway_api::TcpRouteSpec {
                inner: k8s_gateway_api::CommonRouteSpec { parent_refs: None },
                rules: vec![
                    k8s_gateway_api::TcpRouteRule {
                        backend_refs: vec![
                            k8s_gateway_api::BackendRef {
                                weight: None,
                                inner: k8s_gateway_api::BackendObjectReference {
                                    group: None,
                                    kind: None,
                                    name: "ref-1".to_string(),
                                    namespace: Some("default".to_string()),
                                    port: None,
                                },
                            },
                            k8s_gateway_api::BackendRef {
                                weight: None,
                                inner: k8s_gateway_api::BackendObjectReference {
                                    group: None,
                                    kind: None,
                                    name: "ref-2".to_string(),
                                    namespace: None,
                                    port: None,
                                },
                            },
                        ],
                    },
                    k8s_gateway_api::TcpRouteRule {
                        backend_refs: vec![k8s_gateway_api::BackendRef {
                            weight: None,
                            inner: k8s_gateway_api::BackendObjectReference {
                                group: Some("Core".to_string()),
                                kind: Some("Service".to_string()),
                                name: "ref-3".to_string(),
                                namespace: Some("default".to_string()),
                                port: None,
                            },
                        }],
                    },
                ],
            },
            status: None,
        };

        let result: Vec<_> = route
            .spec
            .rules
            .into_iter()
            .flat_map(|rule| rule.backend_refs)
            .map(|br| BackendReference::from_backend_ref(&br.inner, "default"))
            .collect();

        assert_eq!(
            3,
            result.len(),
            "expected only three BackendReferences from route"
        );
        result.into_iter().for_each(|backend_ref| {
            assert!(matches!(backend_ref, BackendReference::Service(_)));
        })
    }

    #[test]
    fn backendrefs_from_multiple_types() {
        let route = k8s_gateway_api::TcpRoute {
            metadata: k8s_core_api::ObjectMeta {
                namespace: Some("default".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: k8s_gateway_api::TcpRouteSpec {
                inner: k8s_gateway_api::CommonRouteSpec { parent_refs: None },
                rules: vec![k8s_gateway_api::TcpRouteRule {
                    backend_refs: vec![
                        k8s_gateway_api::BackendRef {
                            weight: None,
                            inner: k8s_gateway_api::BackendObjectReference {
                                group: None,
                                kind: None,
                                name: "ref-1".to_string(),
                                namespace: None,
                                port: None,
                            },
                        },
                        k8s_gateway_api::BackendRef {
                            weight: None,
                            inner: k8s_gateway_api::BackendObjectReference {
                                group: Some(POLICY_API_GROUP.to_string()),
                                kind: Some("EgressNetwork".to_string()),
                                name: "ref-3".to_string(),
                                namespace: None,
                                port: Some(555),
                            },
                        },
                        k8s_gateway_api::BackendRef {
                            weight: None,
                            inner: k8s_gateway_api::BackendObjectReference {
                                group: Some(POLICY_API_GROUP.to_string()),
                                kind: Some("Server".to_string()),
                                name: "ref-2".to_string(),
                                namespace: None,
                                port: None,
                            },
                        },
                    ],
                }],
            },
            status: None,
        };

        let result: Vec<_> = route
            .spec
            .rules
            .into_iter()
            .flat_map(|rule| rule.backend_refs)
            .map(|br| BackendReference::from_backend_ref(&br.inner, "default"))
            .collect();

        assert_eq!(
            3,
            result.len(),
            "expected only two BackendReferences from route"
        );
        let mut iter = result.into_iter();
        let service = iter.next().unwrap();
        assert!(matches!(service, BackendReference::Service(_)));
        let egress_net = iter.next().unwrap();
        assert!(matches!(egress_net, BackendReference::EgressNetwork(_)));
        let unknown = iter.next().unwrap();
        assert!(matches!(unknown, BackendReference::Unknown))
    }
}
