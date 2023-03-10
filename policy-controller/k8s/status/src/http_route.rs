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

#[derive(Clone, Eq, PartialEq)]
pub enum BackendReference {
    Service(ResourceId),
}

#[derive(Clone, Debug, thiserror::Error)]
pub enum InvalidReference {
    #[error("HTTPRoute resource does not reference a Server resource")]
    DoesNotSelectServer,

    #[error("HTTPRoute resource may not reference a parent by port")]
    SpecifiesPort,

    #[error("HTTPRoute resource may not reference a parent by section name")]
    SpecifiesSection,

    #[error("HTTPRoute resource references backend with unsupported group or kind")]
    InvalidBackendKind,
}

pub(crate) fn make_parents(
    http_route: &policy::HttpRoute,
    namespace: &str,
) -> Result<Vec<ParentReference>, InvalidReference> {
    let route_parent_refs = http_route.get_parents()?;
    let parents = route_parent_refs
        .into_iter()
        .filter_map(|parent| ParentReference::from_parent_ref(parent, namespace))
        .collect::<Result<Vec<_>, InvalidReference>>()?;
    if parents.is_empty() {
        return Err(InvalidReference::DoesNotSelectServer);
    }

    Ok(parents)
}

pub(crate) fn make_backends(
    http_route: &policy::HttpRoute,
    namespace: &str,
) -> Result<Vec<BackendReference>> {
    let route_backend_refs = http_route.get_backends()?;
    let backends = route_backend_refs
        .into_iter()
        .map(|backend| BackendReference::from_backend_ref(backend, namespace))
        .collect::<Result<Vec<_>, InvalidReference>>()?;

    Ok(backends)
}

pub(crate) trait HasReferences {
    fn get_parents(&self) -> Result<Vec<&gateway::ParentReference>, InvalidReference>;
    fn get_backends(&self) -> Result<Vec<&gateway::BackendRef>, InvalidReference>;
}

impl HasReferences for policy::HttpRoute {
    fn get_parents(&self) -> Result<Vec<&gateway::ParentReference>, InvalidReference> {
        let mut parents = Vec::new();
        match &self.spec.inner.parent_refs {
            Some(parent_refs) => {
                for parent in parent_refs {
                    parents.push(parent);
                }

                Ok(parents)
            }
            // TODO: should perhaps move to 'DoesNotSelectParent' or something similar?
            None => Err(InvalidReference::DoesNotSelectServer),
        }
    }

    fn get_backends(&self) -> Result<Vec<&gateway::BackendRef>, InvalidReference> {
        let mut obj_refs = Vec::new();
        let rules = if let Some(rules) = &self.spec.rules {
            rules
        } else {
            return Ok(obj_refs);
        };

        // TODO: for now, this function is infallible
        // when service support is added, we need to consider inexistent backends:
        //    * if a rule does not have a backend, the parentReference is used
        //    * there may be only one parent reference when a Service is used (according to GAMMA
        //    spec)
        //    * we will need to check only one parent ref exists, and typecheck it
        // If no errors are returned, the parentRef information should be used as a BackendRef
        // wherever we have 0 backendRefs defined.
        for rule in rules {
            let backends = rule
                .backend_refs
                .iter()
                .flatten()
                .flat_map(|backend| backend.backend_ref.as_ref());
            obj_refs.extend(backends)
        }

        Ok(obj_refs)
    }
}

impl ParentReference {
    fn from_parent_ref(
        parent_ref: &gateway::ParentReference,
        default_namespace: &str,
    ) -> Option<Result<Self, InvalidReference>> {
        // todo: Allow parent references to target all kinds so that a status
        // is generated for invalid kinds
        if !policy::httproute::parent_ref_targets_kind::<Server>(&parent_ref)
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
            return Some(Err(InvalidReference::SpecifiesPort));
        }
        if section_name.is_some() {
            return Some(Err(InvalidReference::SpecifiesSection));
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
        id: &ResourceId,
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
    ) -> Result<BackendReference, InvalidReference> {
        if !policy::httproute::backend_ref_targets_kind::<Service>(backend_ref) {
            return Err(InvalidReference::InvalidBackendKind);
        }

        let namespace = backend_ref
            .inner
            .namespace
            .clone()
            .unwrap_or_else(|| default_namespace.to_owned());
        Ok(BackendReference::Service(ResourceId::new(
            namespace.to_owned(),
            backend_ref.inner.name.to_owned(),
        )))
    }

    /// BackendReferences are an all-or-nothing operation when converted into a status condition.
    /// If all BackendReferences present on a route can be resolved (i.e exist in the cache) then
    /// one status is set on each parent to inform the backends have been resolved successfully.
    pub(crate) fn into_status_condition(
        resolved_all: bool,
        timestamp: chrono::DateTime<chrono::Utc>,
    ) -> Condition {
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
