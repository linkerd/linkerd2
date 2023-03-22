use crate::resource_id::ResourceId;
use linkerd_policy_controller_k8s_api::{
    gateway,
    policy::{self, Server},
    Service,
};

/// Represents an HTTPRoute's parent reference from its spec.
///
/// This is separate from the policy controller index's `InboundParentRef`
/// because it does not validate that the parent reference is not in another
/// namespace. This is something that should be relaxed in the future in the
/// policy controller's index and we could then consider consolidating these
/// types into a single shared lib.
#[derive(Clone, Eq, PartialEq)]
pub enum ParentReference {
    Server(ResourceId),
    Service(ResourceId, Option<u16>),
    UnknownKind,
}

#[derive(Clone, Eq, PartialEq)]
pub enum BackendReference {
    Service(ResourceId),
    Unknown,
}

pub(crate) fn make_parents(http_route: policy::HttpRoute) -> Vec<ParentReference> {
    let namespace = http_route
        .metadata
        .namespace
        .expect("HTTPRoute must have a namespace");
    http_route
        .spec
        .inner
        .parent_refs
        .into_iter()
        .flatten()
        .map(|parent_ref| ParentReference::from_parent_ref(parent_ref, &namespace))
        .collect()
}

pub(crate) fn make_backends(http_route: policy::HttpRoute) -> Vec<BackendReference> {
    let namespace = http_route
        .metadata
        .namespace
        .expect("HTTPRoute must have a namespace");
    http_route
        .spec
        .rules
        .into_iter()
        .flatten()
        .fold(Vec::new(), |acc, rule| {
            rule.backend_refs
                .into_iter()
                .flatten()
                .flat_map(|http_match| http_match.backend_ref.into_iter())
                .chain(acc)
                .collect()
        })
        .into_iter()
        .map(|backend_ref| BackendReference::from_backend_ref(backend_ref.inner, &namespace))
        .collect()
}

impl ParentReference {
    fn from_parent_ref(parent_ref: gateway::ParentReference, default_namespace: &str) -> Self {
        if policy::httproute::parent_ref_targets_kind::<Server>(&parent_ref) {
            // If the parent reference does not have a namespace, default to using
            // the HTTPRoute's namespace.
            let namespace = parent_ref
                .namespace
                .unwrap_or_else(|| default_namespace.to_string());
            ParentReference::Server(ResourceId::new(namespace, parent_ref.name))
        } else if policy::httproute::parent_ref_targets_kind::<Service>(&parent_ref) {
            // If the parent reference does not have a namespace, default to using
            // the HTTPRoute's namespace.
            let namespace = parent_ref
                .namespace
                .unwrap_or_else(|| default_namespace.to_string());
            ParentReference::Service(ResourceId::new(namespace, parent_ref.name), parent_ref.port)
        } else {
            ParentReference::UnknownKind
        }
    }
}

impl BackendReference {
    fn from_backend_ref(
        backend_ref: gateway::BackendObjectReference,
        default_namespace: &str,
    ) -> Self {
        if policy::httproute::backend_ref_targets_kind::<linkerd_policy_controller_k8s_api::Service>(
            &backend_ref,
        ) {
            let namespace = backend_ref
                .namespace
                .unwrap_or_else(|| default_namespace.to_string());
            BackendReference::Service(ResourceId::new(namespace, backend_ref.name))
        } else {
            BackendReference::Unknown
        }
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use linkerd_policy_controller_k8s_api::{policy, ObjectMeta};

    fn mk_default_http_backends(
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
    fn test_backendrefs_from_route() {
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
                        backend_refs: mk_default_http_backends(vec![
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
                    policy::httproute::HttpRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: mk_default_http_backends(vec![
                            gateway::BackendObjectReference {
                                group: Some("Core".to_string()),
                                kind: Some("Service".to_string()),
                                name: "ref-3".to_string(),
                                namespace: Some("default".to_string()),
                                port: None,
                            },
                        ]),
                    },
                    policy::httproute::HttpRouteRule {
                        matches: None,
                        filters: None,
                        backend_refs: None,
                    },
                ]),
            },
            status: None,
        };

        let result = make_backends(http_route);
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
    fn test_backendrefs_from_multiple_types() {
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
                    backend_refs: mk_default_http_backends(vec![
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

        let result = make_backends(http_route);
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
