use super::{BackendReference, ParentReference};
use linkerd_policy_controller_k8s_api::gateway::{CommonRouteSpec, HttpBackendRef};

pub(crate) fn make_parents(namespace: &str, route: &CommonRouteSpec) -> Vec<ParentReference> {
    route
        .parent_refs
        .iter()
        .flatten()
        .map(|pr| ParentReference::from_parent_ref(pr, namespace))
        .collect()
}

pub(crate) fn make_backends(
    namespace: &str,
    backends: impl Iterator<Item = HttpBackendRef>,
) -> Vec<BackendReference> {
    backends
        .filter_map(|http_backend_ref| http_backend_ref.backend_ref)
        .map(|br| BackendReference::from_backend_ref(&br.inner, namespace))
        .collect()
}

#[cfg(test)]
mod test {
    use super::*;
    use linkerd_policy_controller_k8s_api::{gateway, policy, ObjectMeta};

    fn default_http_backends(
        backend_refs: Vec<gateway::BackendObjectReference>,
    ) -> Option<Vec<gateway::HttpBackendRef>> {
        Some(
            backend_refs
                .into_iter()
                .map(|backend_ref| gateway::HttpBackendRef {
                    backend_ref: Some(gateway::BackendRef {
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
        let http_route = policy::HttpRoute {
            metadata: ObjectMeta {
                namespace: Some("foo".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: policy::HttpRouteSpec {
                inner: gateway::CommonRouteSpec { parent_refs: None },
                hostnames: None,
                rules: Some(vec![
                    policy::httproute::HttpRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: default_http_backends(vec![
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
                        timeouts: None,
                    },
                    policy::httproute::HttpRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: default_http_backends(vec![
                            gateway::BackendObjectReference {
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
            http_route
                .metadata
                .namespace
                .as_deref()
                .expect("HttpRoute must have namespace"),
            http_route
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
        let http_route = policy::HttpRoute {
            metadata: ObjectMeta {
                namespace: Some("default".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: policy::HttpRouteSpec {
                inner: gateway::CommonRouteSpec { parent_refs: None },
                hostnames: None,
                rules: Some(vec![policy::httproute::HttpRouteRule {
                    matches: None,
                    filters: None,
                    backend_refs: default_http_backends(vec![
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
                    timeouts: None,
                }]),
            },
            status: None,
        };

        let result = make_backends(
            http_route
                .metadata
                .namespace
                .as_deref()
                .expect("HttpRoute must have namespace"),
            http_route
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
