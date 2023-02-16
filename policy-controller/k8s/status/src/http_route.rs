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

#[derive(Clone, Debug, thiserror::Error)]
pub enum InvalidParentReference {
    #[error("HTTPRoute resource does not reference a Server resource")]
    DoesNotSelectServer,

    #[error("HTTPRoute resource may not reference a parent by port")]
    SpecifiesPort,

    #[error("HTTPRoute resource may not reference a parent by section name")]
    SpecifiesSection,
}

pub(crate) fn make_parents(http_route: policy::HttpRoute) -> Result<Vec<ParentReference>> {
    let namespace = http_route
        .metadata
        .namespace
        .expect("HTTPRoute must have a namespace");
    let parents = ParentReference::collect_from(http_route.spec.inner.parent_refs, &namespace)?;
    Ok(parents)
}

impl ParentReference {
    fn collect_from(
        parent_refs: Option<Vec<gateway::ParentReference>>,
        namespace: &str,
    ) -> Result<Vec<Self>, InvalidParentReference> {
        let parents = parent_refs
            .into_iter()
            .flatten()
            .filter_map(|parent| Self::from_parent_ref(parent, namespace))
            .collect::<Result<Vec<_>, InvalidParentReference>>()?;
        if parents.is_empty() {
            return Err(InvalidParentReference::DoesNotSelectServer);
        }

        Ok(parents)
    }

    fn from_parent_ref(
        parent_ref: gateway::ParentReference,
        default_namespace: &str,
    ) -> Option<Result<Self, InvalidParentReference>> {
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
            return Some(Err(InvalidParentReference::SpecifiesPort));
        }
        if section_name.is_some() {
            return Some(Err(InvalidParentReference::SpecifiesSection));
        }

        // If the parent reference does not have a namespace, default to using
        // the HTTPRoute's namespace.
        let namespace = parent_namespace.unwrap_or_else(|| default_namespace.to_string());
        Some(Ok(ParentReference::Server(ResourceId::new(
            namespace, name,
        ))))
    }
}
