use crate::resource_id::ResourceId;
use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api, gateway as k8s_gateway_api, policy as linkerd_k8s_api,
};

pub(crate) mod grpc;
pub(crate) mod http;

#[cfg(test)]
pub(crate) mod tcp;
#[cfg(test)]
pub(crate) mod tls;

/// Represents an xRoute's parent reference from its spec.
///
/// This is separate from the policy controller index's `InboundParentRef`
/// because it does not validate that the parent reference is not in another
/// namespace. This is something that should be relaxed in the future in the
/// policy controller's index, and we could then consider consolidating these
/// types into a single shared lib.
#[derive(Clone, Eq, PartialEq)]
pub enum ParentReference {
    Server(ResourceId),
    Service(ResourceId, Option<u16>),
    UnmeshedNetwork(ResourceId, Option<u16>),
    UnknownKind,
}

#[derive(Clone, Eq, PartialEq)]
pub enum BackendReference {
    Service(ResourceId),
    UnmeshedNetwork(ResourceId),
    Unknown,
}

impl ParentReference {
    fn from_parent_ref(
        parent_ref: &k8s_gateway_api::ParentReference,
        default_namespace: &str,
    ) -> Self {
        if linkerd_k8s_api::httproute::parent_ref_targets_kind::<linkerd_k8s_api::Server>(
            parent_ref,
        ) {
            // If the parent reference does not have a namespace, default to using
            // the route's namespace.
            let namespace = parent_ref.namespace.as_deref().unwrap_or(default_namespace);
            Self::Server(ResourceId::new(
                namespace.to_string(),
                parent_ref.name.clone(),
            ))
        } else if linkerd_k8s_api::httproute::parent_ref_targets_kind::<k8s_core_api::Service>(
            parent_ref,
        ) {
            // If the parent reference does not have a namespace, default to using
            // the route's namespace.
            let namespace = parent_ref.namespace.as_deref().unwrap_or(default_namespace);
            Self::Service(
                ResourceId::new(namespace.to_string(), parent_ref.name.clone()),
                parent_ref.port,
            )
        } else if linkerd_k8s_api::httproute::parent_ref_targets_kind::<
            linkerd_k8s_api::UnmeshedNetwork,
        >(parent_ref)
        {
            // If the parent reference does not have a namespace, default to using
            // the route's namespace.
            let namespace = parent_ref.namespace.as_deref().unwrap_or(default_namespace);
            Self::UnmeshedNetwork(
                ResourceId::new(namespace.to_string(), parent_ref.name.clone()),
                parent_ref.port,
            )
        } else {
            Self::UnknownKind
        }
    }
}

impl BackendReference {
    pub(crate) fn from_backend_ref(
        backend_ref: &k8s_gateway_api::BackendObjectReference,
        default_namespace: &str,
    ) -> Self {
        if linkerd_k8s_api::httproute::backend_ref_targets_kind::<k8s_core_api::Service>(
            backend_ref,
        ) {
            let namespace = backend_ref
                .namespace
                .as_deref()
                .unwrap_or(default_namespace);
            Self::Service(ResourceId::new(
                namespace.to_string(),
                backend_ref.name.clone(),
            ))
        } else if linkerd_k8s_api::httproute::backend_ref_targets_kind::<
            linkerd_k8s_api::UnmeshedNetwork,
        >(backend_ref)
        {
            let namespace = backend_ref
                .namespace
                .as_deref()
                .unwrap_or(default_namespace);
            Self::UnmeshedNetwork(ResourceId::new(
                namespace.to_string(),
                backend_ref.name.clone(),
            ))
        } else {
            Self::Unknown
        }
    }
}

pub(crate) fn make_parents(
    namespace: &str,
    route: &k8s_gateway_api::CommonRouteSpec,
) -> Vec<ParentReference> {
    route
        .parent_refs
        .iter()
        .flatten()
        .map(|pr| ParentReference::from_parent_ref(pr, namespace))
        .collect()
}
