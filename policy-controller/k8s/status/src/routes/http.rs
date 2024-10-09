use super::BackendReference;
use linkerd_policy_controller_k8s_api::gateway as k8s_gateway_api;

pub(crate) fn make_backends(
    namespace: &str,
    backends: impl Iterator<Item = k8s_gateway_api::HttpBackendRef>,
) -> Vec<BackendReference> {
    backends
        .filter_map(|http_backend_ref| http_backend_ref.backend_ref)
        .map(|br| BackendReference::from_backend_ref(&br.inner, namespace))
        .collect()
}

#[cfg(test)]
mod test {
    use super::*;
    use crate::index::POLICY_API_GROUP;
    use linkerd_policy_controller_k8s_api::{self as k8s_core_api, policy};

    fn mk_default_http_backends(
        backend_refs: Vec<k8s_gateway_api::BackendObjectReference>,
    ) -> Option<Vec<k8s_gateway_api::HttpBackendRef>> {
        Some(
            backend_refs
                .into_iter()
                .map(|backend_ref| k8s_gateway_api::HttpBackendRef {
                    backend_ref: Some(k8s_gateway_api::BackendRef {
                        inner: backend_ref,
                        weight: None,
                    }),
                    filters: None,
                })
                .collect(),
        )
    }

    #[test]
    fn backendrefs_from_route() {
        let route = policy::HttpRoute {
            metadata: k8s_core_api::ObjectMeta {
                namespace: Some("foo".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: policy::HttpRouteSpec {
                inner: k8s_gateway_api::CommonRouteSpec { parent_refs: None },
                hostnames: None,
                rules: Some(vec![
                    policy::httproute::HttpRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: mk_default_http_backends(vec![
                            k8s_gateway_api::BackendObjectReference {
                                group: None,
                                kind: None,
                                name: "ref-1".to_string(),
                                namespace: Some("default".to_string()),
                                port: None,
                            },
                            k8s_gateway_api::BackendObjectReference {
                                group: None,
                                kind: None,
                                name: "ref-2".to_string(),
                                namespace: None,
                                port: None,
                            },
                        ]),
                        timeouts: None,
                    },
                    policy::httproute::HttpRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: mk_default_http_backends(vec![
                            k8s_gateway_api::BackendObjectReference {
                                group: Some("Core".to_string()),
                                kind: Some("Service".to_string()),
                                name: "ref-3".to_string(),
                                namespace: Some("default".to_string()),
                                port: None,
                            },
                        ]),
                        timeouts: None,
                    },
                    policy::httproute::HttpRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: None,
                        timeouts: None,
                    },
                ]),
            },
            status: None,
        };

        let result = make_backends(
            route
                .metadata
                .namespace
                .as_deref()
                .expect("HttpRoute must have namespace"),
            route
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
        let route = policy::HttpRoute {
            metadata: k8s_core_api::ObjectMeta {
                namespace: Some("default".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: policy::HttpRouteSpec {
                inner: k8s_gateway_api::CommonRouteSpec { parent_refs: None },
                hostnames: None,
                rules: Some(vec![policy::httproute::HttpRouteRule {
                    matches: None,
                    filters: None,
                    backend_refs: mk_default_http_backends(vec![
                        k8s_gateway_api::BackendObjectReference {
                            group: None,
                            kind: None,
                            name: "ref-1".to_string(),
                            namespace: None,
                            port: None,
                        },
                        k8s_gateway_api::BackendObjectReference {
                            group: Some(POLICY_API_GROUP.to_string()),
                            kind: Some("UnmeshedNetwork".to_string()),
                            name: "ref-3".to_string(),
                            namespace: None,
                            port: Some(555),
                        },
                        k8s_gateway_api::BackendObjectReference {
                            group: Some(POLICY_API_GROUP.to_string()),
                            kind: Some("Server".to_string()),
                            name: "ref-2".to_string(),
                            namespace: None,
                            port: None,
                        },
                    ]),
                    timeouts: None,
                }]),
            },
            status: None,
        };

        let result = make_backends(
            route
                .metadata
                .namespace
                .as_deref()
                .expect("HttpRoute must have namespace"),
            route
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
            "expected only two BackendReferences from route"
        );
        let mut iter = result.into_iter();
        let service = iter.next().unwrap();
        assert!(matches!(service, BackendReference::Service(_)));
        let unmeshed_net = iter.next().unwrap();
        assert!(matches!(unmeshed_net, BackendReference::UnmeshedNetwork(_)));
        let unknown = iter.next().unwrap();
        assert!(matches!(unknown, BackendReference::Unknown))
    }
}
