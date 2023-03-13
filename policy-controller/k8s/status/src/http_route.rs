use crate::resource_id::ResourceId;
use anyhow::Result;
use linkerd_policy_controller_k8s_api::{gateway, policy};

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

/// Convenience trait to get shared route reference types.
pub(crate) trait GetReferences {
    fn get_parents(&self) -> Vec<&gateway::ParentReference>;
}

impl Reference {
    fn from_parent_ref(reference: &gateway::ParentReference, default_namespace: &str) -> Self {
        let namespace = reference
            .namespace
            .clone()
            .unwrap_or_else(|| default_namespace.to_string());
        let id = ResourceId::new(namespace, reference.name.to_string());
        let group = reference.group.clone();
        let kind = reference.kind.clone();
        Reference { id, group, kind }
    }
}

/// Make internal representation references from Gateway ParentReferences.
pub(crate) fn make_parents(
    parent_refs: Vec<&gateway::ParentReference>,
    namespace: &str,
) -> Vec<Reference> {
    parent_refs
        .into_iter()
        .map(|parent| Reference::from_parent_ref(parent, namespace))
        .collect()
}

impl GetReferences for policy::HttpRoute {
    fn get_parents(&self) -> Vec<&gateway::ParentReference> {
        self.spec.inner.parent_refs.iter().flatten().collect()
    }
}
