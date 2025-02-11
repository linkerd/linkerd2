use super::{BackendReference, ParentReference, ResourceId};
use anyhow::Result;
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    gateway::tlsroutes as gateway,
    policy::{
        self,
        tlsroute::{backend_ref_targets_kind, parent_ref_targets_kind},
    },
};

pub(crate) fn make_backends(
    namespace: &str,
    backends: impl Iterator<Item = gateway::TLSRouteRulesBackendRefs>,
) -> Vec<BackendReference> {
    backends.map(|br| to_backend_ref(&br, namespace)).collect()
}

pub(crate) fn make_parents(
    namespace: &str,
    parents: &[gateway::TLSRouteParentRefs],
) -> Vec<ParentReference> {
    parents
        .iter()
        .filter_map(|pr| {
            to_parent_ref(pr, namespace)
                .inspect_err(|error| tracing::error!(?error, "failed to make parent reference"))
                .ok()
        })
        .collect()
}

fn to_parent_ref(
    parent_ref: &gateway::TLSRouteParentRefs,
    default_namespace: &str,
) -> Result<ParentReference> {
    if parent_ref_targets_kind::<policy::Server>(parent_ref) {
        // If the parent reference does not have a namespace, default to using
        // the route's namespace.
        let namespace = parent_ref.namespace.as_deref().unwrap_or(default_namespace);
        Result::Ok(ParentReference::Server(ResourceId::new(
            namespace.to_string(),
            parent_ref.name.clone(),
        )))
    } else if parent_ref_targets_kind::<k8s::Service>(parent_ref) {
        // If the parent reference does not have a namespace, default to using
        // the route's namespace.
        let namespace = parent_ref.namespace.as_deref().unwrap_or(default_namespace);
        Result::Ok(ParentReference::Service(
            ResourceId::new(namespace.to_string(), parent_ref.name.clone()),
            parent_ref.port.map(|p| p.try_into()).transpose()?,
        ))
    } else if parent_ref_targets_kind::<policy::EgressNetwork>(parent_ref) {
        // If the parent reference does not have a namespace, default to using
        // the route's namespace.
        let namespace = parent_ref.namespace.as_deref().unwrap_or(default_namespace);
        Result::Ok(ParentReference::EgressNetwork(
            ResourceId::new(namespace.to_string(), parent_ref.name.clone()),
            parent_ref.port.map(|p| p.try_into()).transpose()?,
        ))
    } else {
        Result::Ok(ParentReference::UnknownKind)
    }
}

fn to_backend_ref(
    backend_ref: &gateway::TLSRouteRulesBackendRefs,
    default_namespace: &str,
) -> BackendReference {
    if backend_ref_targets_kind::<k8s::Service>(backend_ref) {
        let namespace = backend_ref
            .namespace
            .as_deref()
            .unwrap_or(default_namespace);
        BackendReference::Service(ResourceId::new(
            namespace.to_string(),
            backend_ref.name.clone(),
        ))
    } else if backend_ref_targets_kind::<policy::EgressNetwork>(backend_ref) {
        let namespace = backend_ref
            .namespace
            .as_deref()
            .unwrap_or(default_namespace);
        BackendReference::EgressNetwork(ResourceId::new(
            namespace.to_string(),
            backend_ref.name.clone(),
        ))
    } else {
        BackendReference::Unknown
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use crate::index::POLICY_API_GROUP;

    #[test]
    fn backendrefs_from_route() {
        let route = gateway::TLSRoute {
            metadata: k8s::ObjectMeta {
                namespace: Some("foo".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: gateway::TLSRouteSpec {
                parent_refs: None,
                hostnames: None,
                rules: vec![
                    gateway::TLSRouteRules {
                        name: None,
                        backend_refs: Some(vec![
                            gateway::TLSRouteRulesBackendRefs {
                                weight: None,
                                group: None,
                                kind: None,
                                name: "ref-1".to_string(),
                                namespace: Some("default".to_string()),
                                port: None,
                            },
                            gateway::TLSRouteRulesBackendRefs {
                                weight: None,
                                group: None,
                                kind: None,
                                name: "ref-2".to_string(),
                                namespace: None,
                                port: None,
                            },
                        ]),
                    },
                    gateway::TLSRouteRules {
                        name: None,
                        backend_refs: Some(vec![gateway::TLSRouteRulesBackendRefs {
                            weight: None,
                            group: Some("Core".to_string()),
                            kind: Some("Service".to_string()),
                            name: "ref-3".to_string(),
                            namespace: Some("default".to_string()),
                            port: None,
                        }]),
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
            .flatten()
            .map(|br| to_backend_ref(&br, "default"))
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
        let route = gateway::TLSRoute {
            metadata: k8s::ObjectMeta {
                namespace: Some("default".to_string()),
                name: Some("foo".to_string()),
                ..Default::default()
            },
            spec: gateway::TLSRouteSpec {
                parent_refs: None,
                hostnames: None,
                rules: vec![gateway::TLSRouteRules {
                    name: None,
                    backend_refs: Some(vec![
                        gateway::TLSRouteRulesBackendRefs {
                            weight: None,
                            group: None,
                            kind: None,
                            name: "ref-1".to_string(),
                            namespace: None,
                            port: None,
                        },
                        gateway::TLSRouteRulesBackendRefs {
                            weight: None,
                            group: Some(POLICY_API_GROUP.to_string()),
                            kind: Some("EgressNetwork".to_string()),
                            name: "ref-3".to_string(),
                            namespace: None,
                            port: Some(555),
                        },
                        gateway::TLSRouteRulesBackendRefs {
                            weight: None,
                            group: Some(POLICY_API_GROUP.to_string()),
                            kind: Some("Server".to_string()),
                            name: "ref-2".to_string(),
                            namespace: None,
                            port: None,
                        },
                    ]),
                }],
            },
            status: None,
        };

        let result: Vec<_> = route
            .spec
            .rules
            .into_iter()
            .flat_map(|rule| rule.backend_refs)
            .flatten()
            .map(|br| to_backend_ref(&br, "default"))
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
