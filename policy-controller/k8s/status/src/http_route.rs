use crate::resource_id::ResourceId;
use anyhow::Result;
use linkerd_policy_controller_k8s_api::{
    gateway,
    policy::{self, Server},
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
}

pub(crate) fn make_parents(
    http_route: &policy::HttpRoute,
    namespace: &str,
) -> Result<Vec<ParentReference>> {
    let route_parent_refs = http_route.get_parents()?;
    let parents = ParentReference::collect_from(route_parent_refs, &namespace)?;
    Ok(parents)
}

pub(crate) trait HasReferences {
    fn get_parents(&self) -> Result<&Vec<gateway::ParentReference>, InvalidReference>;
    fn get_backends(&self) -> Result<&Vec<gateway::BackendObjectReference>, InvalidReference>;
}

impl HasReferences for policy::HttpRoute {
    fn get_parents(&self) -> Result<&Vec<gateway::ParentReference>, InvalidReference> {
        match &self.spec.inner.parent_refs {
            Some(parent_refs) => Ok(parent_refs.as_ref()),
            None => Err(InvalidReference::DoesNotSelectServer),
        }
    }

    fn get_backends(&self) -> Result<&Vec<gateway::BackendObjectReference>, InvalidReference> {
        todo!()
    }
}

impl ParentReference {
    fn collect_from(
        parent_refs: &Vec<gateway::ParentReference>,
        namespace: &str,
    ) -> Result<Vec<Self>, InvalidReference> {
        let parents = parent_refs
            .into_iter()
            .filter_map(|parent| Self::from_parent_ref(parent, namespace))
            .collect::<Result<Vec<_>, InvalidReference>>()?;
        if parents.is_empty() {
            return Err(InvalidReference::DoesNotSelectServer);
        }

        Ok(parents)
    }

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
}
