use crate::resource_id::ResourceId;
use linkerd_policy_controller_k8s_api::{
    gateway,
    policy::{self, Server},
    Resource, Service,
};
use std::borrow::Cow;

#[cfg(test)]
use linkerd_policy_controller_core::http_route::GroupKindName;

pub(crate) mod http;

#[cfg(feature = "experimental")]
pub(crate) mod grpc;

/// Conserves a given route's original `kind` and
/// `group` (type and spec source) with as little
/// overhead as is absolutely necessary
#[derive(Debug, Copy, Clone, Eq, PartialEq)]
pub(crate) enum RouteType {
    GatewayHttp,
    LinkerdHttp,
    #[cfg(feature = "experimental")]
    GatewayGrpc,
}

/// Represents a given route's parent reference(s) from its spec.
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
    UnknownKind,
}

#[derive(Clone, Eq, PartialEq)]
pub enum BackendReference {
    Service(ResourceId),
    Unknown,
}

impl core::fmt::Display for RouteType {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .write_str(match self {
                Self::LinkerdHttp | Self::GatewayHttp => "HTTP",
                #[cfg(feature = "experimental")]
                Self::GatewayGrpc => "GRPC",
            })
            .and_then(|_| formatter.write_str("Route"))
    }
}

impl RouteType {
    fn k8s_group_kind_api_version(
        &self,
    ) -> (Cow<'static, str>, Cow<'static, str>, Cow<'static, str>) {
        use linkerd_policy_controller_k8s_api::policy as linkerd_policy;

        match self {
            Self::LinkerdHttp => (
                linkerd_policy::HttpRoute::group(&()),
                linkerd_policy::HttpRoute::kind(&()),
                linkerd_policy::HttpRoute::api_version(&()),
            ),
            Self::GatewayHttp => (
                k8s_gateway_api::HttpRoute::group(&()),
                k8s_gateway_api::HttpRoute::kind(&()),
                k8s_gateway_api::HttpRoute::api_version(&()),
            ),
            #[cfg(feature = "experimental")]
            Self::GatewayGrpc => (
                k8s_gateway_api::GrpcRoute::group(&()),
                k8s_gateway_api::GrpcRoute::kind(&()),
                k8s_gateway_api::GrpcRoute::api_version(&()),
            ),
        }
    }
    pub(crate) fn k8s_group(&self) -> Cow<'static, str> {
        self.k8s_group_kind_api_version().0
    }

    #[allow(dead_code)]
    pub(crate) fn k8s_kind(&self) -> Cow<'static, str> {
        self.k8s_group_kind_api_version().1
    }

    pub(crate) fn k8s_api_version(&self) -> Cow<'static, str> {
        self.k8s_group_kind_api_version().2
    }
    #[cfg(test)]
    pub(crate) fn gkn<RouteName: ToString>(&self, name: RouteName) -> GroupKindName {
        let (group, kind, _) = self.k8s_group_kind_api_version();

        GroupKindName {
            group,
            kind,
            name: name.to_string().into(),
        }
    }
}

impl ParentReference {
    fn from_parent_ref(parent_ref: &gateway::ParentReference, default_namespace: &str) -> Self {
        if policy::httproute::parent_ref_targets_kind::<Server>(parent_ref) {
            // If the parent reference does not have a namespace, default to using
            // the route's namespace.
            let namespace = parent_ref.namespace.as_deref().unwrap_or(default_namespace);
            ParentReference::Server(ResourceId::new(
                namespace.to_string(),
                parent_ref.name.clone(),
            ))
        } else if policy::httproute::parent_ref_targets_kind::<Service>(parent_ref) {
            // If the parent reference does not have a namespace, default to using
            // the route's namespace.
            let namespace = parent_ref.namespace.as_deref().unwrap_or(default_namespace);
            ParentReference::Service(
                ResourceId::new(namespace.to_string(), parent_ref.name.clone()),
                parent_ref.port,
            )
        } else {
            ParentReference::UnknownKind
        }
    }
}

impl BackendReference {
    fn from_backend_ref(
        backend_ref: &gateway::BackendObjectReference,
        default_namespace: &str,
    ) -> Self {
        if policy::httproute::backend_ref_targets_kind::<linkerd_policy_controller_k8s_api::Service>(
            backend_ref,
        ) {
            let namespace = backend_ref
                .namespace
                .as_deref()
                .unwrap_or(default_namespace);
            BackendReference::Service(ResourceId::new(
                namespace.to_string(),
                backend_ref.name.clone(),
            ))
        } else {
            BackendReference::Unknown
        }
    }
}
