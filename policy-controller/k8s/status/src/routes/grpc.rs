use super::BackendReference;

#[allow(unused_imports)]
pub(crate) use super::http::make_parents;

pub(crate) fn make_backends(
    namespace: &str,
    backends: impl Iterator<Item = k8s_gateway_api::GrpcRouteBackendRef>,
) -> Vec<BackendReference> {
    backends
        .map(|backend_ref| BackendReference::from_backend_ref(&backend_ref.inner, namespace))
        .collect()
}

#[cfg(test)]
mod test {
    use super::*;
    use k8s_gateway_api as gateway;
    use linkerd_policy_controller_k8s_api::ObjectMeta;

    fn default_grpc_backends(
        backend_refs: Vec<gateway::BackendObjectReference>,
    ) -> Option<Vec<gateway::GrpcRouteBackendRef>> {
        Some(
            backend_refs
                .into_iter()
                .map(|backend_ref| gateway::GrpcRouteBackendRef {
                    inner: backend_ref,
                    weight: None,
                    filters: None,
                })
                .collect(),
        )
    }

    #[test]
    fn backendrefs_from_route() {
        let grpc_route = gateway::GrpcRoute {
            metadata: ObjectMeta {
                namespace: Some("foo".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: gateway::GrpcRouteSpec {
                inner: gateway::CommonRouteSpec { parent_refs: None },
                hostnames: None,
                rules: Some(vec![
                    gateway::GrpcRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: default_grpc_backends(vec![
                            gateway::BackendObjectReference {
                                group: None,
                                kind: None,
                                name: "ref-1".to_string(),
                                namespace: Some("default".to_string()),
                                port: None,
                            },
                            gateway::BackendObjectReference {
                                group: None,
                                kind: None,
                                name: "ref-2".to_string(),
                                namespace: None,
                                port: None,
                            },
                        ]),
                    },
                    gateway::GrpcRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: default_grpc_backends(vec![
                            gateway::BackendObjectReference {
                                group: Some("Core".to_string()),
                                kind: Some("Service".to_string()),
                                name: "ref-3".to_string(),
                                namespace: Some("default".to_string()),
                                port: None,
                            },
                        ]),
                    },
                    gateway::GrpcRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: None,
                    },
                ]),
            },
            status: None,
        };

        let result = make_backends(
            grpc_route
                .metadata
                .namespace
                .as_deref()
                .expect("GrpcRoute must have namespace"),
            grpc_route
                .spec
                .rules
                .into_iter()
                .flatten()
                .flat_map(|rule| rule.backend_refs)
                .flatten(),
        );
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
        let grpc_route = gateway::GrpcRoute {
            metadata: ObjectMeta {
                namespace: Some("default".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: gateway::GrpcRouteSpec {
                inner: gateway::CommonRouteSpec { parent_refs: None },
                hostnames: None,
                rules: Some(vec![gateway::GrpcRouteRule {
                    matches: None,
                    filters: None,
                    backend_refs: default_grpc_backends(vec![
                        gateway::BackendObjectReference {
                            group: None,
                            kind: None,
                            name: "ref-1".to_string(),
                            namespace: None,
                            port: None,
                        },
                        gateway::BackendObjectReference {
                            group: Some("policy.linkerd.io".to_string()),
                            kind: Some("Server".to_string()),
                            name: "ref-2".to_string(),
                            namespace: None,
                            port: None,
                        },
                    ]),
                }]),
            },
            status: None,
        };

        let result = make_backends(
            grpc_route
                .metadata
                .namespace
                .as_deref()
                .expect("GrpcRoute must have namespace"),
            grpc_route
                .spec
                .rules
                .into_iter()
                .flatten()
                .flat_map(|rule| rule.backend_refs)
                .flatten(),
        );
        assert_eq!(
            2,
            result.len(),
            "expected only two BackendReferences from route"
        );
        let mut iter = result.into_iter();
        let known = iter.next().unwrap();
        assert!(matches!(known, BackendReference::Service(_)));
        let unknown = iter.next().unwrap();
        assert!(matches!(unknown, BackendReference::Unknown))
    }
}
