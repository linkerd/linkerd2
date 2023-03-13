use crate::resource_id::ResourceId;
use anyhow::Result;
use linkerd_policy_controller_k8s_api::{
    gateway,
    policy::{self, Server},
    Condition, Service, Time,
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
}

/// Represents an HTTPRoute's backend reference from its spec.
///
/// Each HTTP Route Rule may have a number of backend references that traffic
/// should be sent to. BackendReference serves as a wrapper around an actual
/// reference's concrete type and identifier (namespace and name).
#[derive(Clone, Eq, PartialEq)]
pub struct BackendReference {
    id: ResourceId,
    kind: Option<String>,
    group: Option<String>,
}

#[derive(Clone, Debug, thiserror::Error)]
pub enum InvalidParentReference {
    #[error("HTTPRoute resource does not reference a Server resource")]
    DoesNotSelectServer,

    #[error("HTTPRoute resource may not reference a parent by port")]
    SpecifiesPort,

    #[error("HTTPRoute resource may not reference a parent by section name")]
    SpecifiesSection,
}

/// Map from Gateway API ParentReference shared type to internal representation
/// of a parent reference.
pub(crate) fn make_parents(
    parent_refs: Vec<&gateway::ParentReference>,
    namespace: &str,
) -> Result<Vec<ParentReference>, InvalidParentReference> {
    let parents = parent_refs
        .into_iter()
        .filter_map(|parent| ParentReference::from_parent_ref(parent, namespace))
        .collect::<Result<Vec<_>, InvalidParentReference>>()?;
    if parents.is_empty() {
        return Err(InvalidParentReference::DoesNotSelectServer);
    }

    Ok(parents)
}

/// Map from Gateway API BackendReference shared type to internal representation of a
/// backend reference.
pub(crate) fn make_backends(
    backend_refs: Vec<&gateway::BackendRef>,
    namespace: &str,
) -> Vec<BackendReference> {
    backend_refs
        .into_iter()
        .map(|backend| BackendReference::from_backend_ref(backend, namespace))
        .collect()
}

/// Convenience trait to get shared route reference types
pub(crate) trait HasReferences {
    fn get_parents(&self) -> Result<Vec<&gateway::ParentReference>, InvalidParentReference>;
    fn get_backends(&self) -> Vec<&gateway::BackendRef>;
}

impl HasReferences for policy::HttpRoute {
    fn get_parents(&self) -> Result<Vec<&gateway::ParentReference>, InvalidParentReference> {
        let mut parents = Vec::new();
        match &self.spec.inner.parent_refs {
            Some(parent_refs) => {
                for parent in parent_refs {
                    parents.push(parent);
                }

                Ok(parents)
            }
            // TODO: should perhaps move to 'DoesNotSelectParent' or something similar?
            None => Err(InvalidParentReference::DoesNotSelectServer),
        }
    }

    // TODO: we will need to consider inexistent backends when Service
    // support is added. When we have no backend_refs, we will need to look
    // at one (and only one) parentRef.
    //   * If a rule does not have a backend, parentRef type is used.
    //   * There may be only one parentRef when Service is used as a
    //   parentRef kind
    //   * Typecheck parentRef, if it's a Service, it should be used,
    //   otherwise an invalid kind should be thrown.
    fn get_backends(&self) -> Vec<&gateway::BackendRef> {
        let mut obj_refs = Vec::new();
        let rules = if let Some(rules) = &self.spec.rules {
            rules
        } else {
            return obj_refs;
        };

        for rule in rules {
            let backends = rule
                .backend_refs
                .iter()
                .flatten()
                .flat_map(|backend| backend.backend_ref.as_ref());
            obj_refs.extend(backends)
        }

        obj_refs
    }
}

impl ParentReference {
    fn from_parent_ref(
        parent_ref: &gateway::ParentReference,
        default_namespace: &str,
    ) -> Option<Result<Self, InvalidParentReference>> {
        // todo: Allow parent references to target all kinds so that a status
        // is generated for invalid kinds
        if !policy::httproute::parent_ref_targets_kind::<Server>(parent_ref)
            || parent_ref.name.is_empty()
        {
            return None;
        }

        let gateway::ParentReference {
            group: _,
            kind: _,
            namespace: parent_namespace,
            name,
            section_name,
            port,
        } = parent_ref;
        if port.is_some() {
            return Some(Err(InvalidParentReference::SpecifiesPort));
        }
        if section_name.is_some() {
            return Some(Err(InvalidParentReference::SpecifiesSection));
        }

        // If the parent reference does not have a namespace, default to using
        // the HTTPRoute's namespace.
        let namespace = parent_namespace
            .to_owned()
            .unwrap_or_else(|| default_namespace.to_owned());
        Some(Ok(ParentReference::Server(ResourceId::new(
            namespace,
            name.to_owned(),
        ))))
    }

    /// Each parentReference on a route is checked against the list of cached resources that may be
    /// a parent. If the parentReference's id is in the cache, then the parent is accepted and a
    /// successful condition is returned. Otherwise, the parentReference is rejected. Sets an
    /// "Accepted" type on the condition field.
    pub(crate) fn into_status_condition(
        accepted: bool,
        timestamp: chrono::DateTime<chrono::Utc>,
    ) -> Condition {
        if accepted {
            Condition {
                last_transition_time: Time(timestamp),
                message: "".to_string(),
                observed_generation: None,
                reason: "Accepted".to_string(),
                status: "True".to_string(),
                type_: "Accepted".to_string(),
            }
        } else {
            Condition {
                last_transition_time: Time(timestamp),
                message: "".to_string(),
                observed_generation: None,
                reason: "NoMatchingParent".to_string(),
                status: "False".to_string(),
                type_: "Accepted".to_string(),
            }
        }
    }
}

impl BackendReference {
    fn from_backend_ref(
        backend_ref: &gateway::BackendRef,
        default_namespace: &str,
    ) -> BackendReference {
        let namespace = backend_ref
            .inner
            .namespace
            .clone()
            .unwrap_or_else(|| default_namespace.to_owned());
        let id = ResourceId::new(namespace, backend_ref.inner.name.to_owned());
        let group = backend_ref.inner.group.clone();
        let kind = backend_ref.inner.group.clone();
        BackendReference { id, group, kind }
    }

    /// An all-or-nothing operation that produces a status condition based on a
    /// route's BackendReferences and based on the internal Index Service cache.
    ///
    /// If all BackendReferences present on a route can be resolved (i.e exist
    /// in the cache) then one status is set on each parent to inform the
    /// backends have been resolved successfully. Each BackReference instance is
    /// typechecked to ensure only allowed types may be used. Consequently, if a
    /// BackendReference has an invalid type, an InvalidKind condition is set
    /// for _all_ references.
    pub(crate) fn into_status_condition(
        backend_refs: &[BackendReference],
        services: &ahash::AHashSet<ResourceId>,
        timestamp: chrono::DateTime<chrono::Utc>,
    ) -> Condition {
        let mut resolved_all = true;
        for backend in backend_refs.iter() {
            let BackendReference { id, group, kind } = backend;
            if !services.contains(id) {
                resolved_all = false;
                break;
            }

            if !policy::httproute::backend_ref_targets_kind::<Service>(kind, group) {
                return Condition {
                    last_transition_time: Time(timestamp),
                    type_: "ResolvedRefs".to_string(),
                    status: "False".to_string(),
                    reason: "InvalidKind".to_string(),
                    observed_generation: None,
                    message: "".to_string(),
                };
            }
        }

        if resolved_all {
            Condition {
                last_transition_time: Time(timestamp),
                type_: "ResolvedRefs".to_string(),
                status: "True".to_string(),
                reason: "ResolvedRefs".to_string(),
                observed_generation: None,
                message: "".to_string(),
            }
        } else {
            Condition {
                last_transition_time: Time(timestamp),
                type_: "ResolvedRefs".to_string(),
                status: "False".to_string(),
                reason: "BackendDoesNotExist".to_string(),
                observed_generation: None,
                message: "".to_string(),
            }
        }
    }
}
