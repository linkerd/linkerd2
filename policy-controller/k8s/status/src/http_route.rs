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
