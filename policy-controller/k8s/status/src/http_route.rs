use crate::resource_id::ResourceId;
use anyhow::Result;
use linkerd_policy_controller_k8s_api::{
    gateway,
    policy::{self, Server},
};

/// Represents an HTTPRoute's reference from its spec.
///
/// Each HTTPRoute may have a number of parent or backend references.
/// Reference serves as a wrapper around one these reference's identifier and
/// concrete type.
#[derive(Clone, Eq, PartialEq)]
pub struct Reference {
    pub id: ResourceId,

    group: Option<String>,
    kind: Option<String>,
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

/// Convenience trait to get shared route reference types.
pub(crate) trait GetReferences {
    fn get_parents(&self) -> Vec<&gateway::ParentReference>;
}

impl Reference {
    // todo: Once we allow this to return references to all kinds, we can
    // probably make this infallible, or at least remove the Option
    fn from_parent_ref(
        reference: &gateway::ParentReference,
        default_namespace: &str,
    ) -> Option<Result<Self, InvalidReference>> {
        // todo: Allow parent references to target all kinds so that a status
        // is generated for invalid kinds
        if !policy::httproute::parent_ref_targets_kind::<Server>(reference)
            || reference.name.is_empty()
        {
            return None;
        }
        if reference.port.is_some() {
            return Some(Err(InvalidReference::SpecifiesPort));
        }
        if reference.section_name.is_some() {
            return Some(Err(InvalidReference::SpecifiesSection));
        }

        let namespace = reference
            .namespace
            .clone()
            .unwrap_or_else(|| default_namespace.to_string());
        let id = ResourceId::new(namespace, reference.name.to_string());
        let group = reference.group.clone();
        let kind = reference.kind.clone();
        Some(Ok(Reference { id, group, kind }))
    }
}

/// Make internal representation references from Gateway ParentReferences.
pub(crate) fn make_parents(
    parent_refs: Vec<&gateway::ParentReference>,
    namespace: &str,
) -> Result<Vec<Reference>, InvalidReference> {
    let parents = parent_refs
        .into_iter()
        .filter_map(|parent| Reference::from_parent_ref(parent, namespace))
        .collect::<Result<Vec<_>, InvalidReference>>()?;
    if parents.is_empty() {
        return Err(InvalidReference::DoesNotSelectServer);
    }
    Ok(parents)
}

impl GetReferences for policy::HttpRoute {
    fn get_parents(&self) -> Vec<&gateway::ParentReference> {
        self.spec.inner.parent_refs.iter().flatten().collect()
    }
}
