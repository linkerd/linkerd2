use linkerd_policy_controller_core::routes::{GroupKindName, GroupKindNamespaceName, HostMatch};
use linkerd_policy_controller_k8s_api::{gateway, policy, Resource, ResourceExt};

pub mod grpc;
pub mod http;

#[derive(Debug, Clone)]
pub(crate) enum HttpRouteResource {
    LinkerdHttp(policy::HttpRoute),
    GatewayHttp(gateway::httproutes::HTTPRoute),
}

impl HttpRouteResource {
    pub(crate) fn name(&self) -> String {
        match self {
            Self::LinkerdHttp(route) => route.name_unchecked(),
            Self::GatewayHttp(route) => route.name_unchecked(),
        }
    }

    pub(crate) fn namespace(&self) -> String {
        match self {
            Self::LinkerdHttp(route) => route.namespace().expect("HttpRoute must have a namespace"),
            Self::GatewayHttp(route) => route.namespace().expect("HttpRoute must have a namespace"),
        }
    }

    pub(crate) fn parent_refs(&self) -> &Option<Vec<gateway::httproutes::HTTPRouteParentRefs>> {
        match self {
            Self::LinkerdHttp(route) => &route.spec.parent_refs,
            Self::GatewayHttp(route) => &route.spec.parent_refs,
        }
    }

    pub(crate) fn status(&self) -> Option<&gateway::httproutes::HTTPRouteStatus> {
        match self {
            Self::LinkerdHttp(route) => route.status.as_ref().map(|status| &status.inner),
            Self::GatewayHttp(route) => route.status.as_ref().map(|status| status),
        }
    }

    pub(crate) fn gknn(&self) -> GroupKindNamespaceName {
        match self {
            Self::LinkerdHttp(route) => route
                .gkn()
                .namespaced(route.namespace().expect("Route must have namespace")),
            Self::GatewayHttp(route) => route
                .gkn()
                .namespaced(route.namespace().expect("Route must have namespace")),
        }
    }
}

pub trait ExplicitGKN {
    fn gkn<R: Resource<DynamicType = ()>>(&self) -> GroupKindName;
}
pub trait ImpliedGKN {
    fn gkn(&self) -> GroupKindName;
}

impl<R: Sized + Resource<DynamicType = ()>> ImpliedGKN for R {
    fn gkn(&self) -> GroupKindName {
        let (kind, group, name) = (
            Self::kind(&()),
            Self::group(&()),
            self.name_unchecked().into(),
        );

        GroupKindName { group, kind, name }
    }
}

impl ExplicitGKN for str {
    fn gkn<R: Resource<DynamicType = ()>>(&self) -> GroupKindName {
        let (kind, group, name) = (R::kind(&()), R::group(&()), self.to_string().into());

        GroupKindName { group, kind, name }
    }
}

pub fn host_match(hostname: String) -> HostMatch {
    if hostname.starts_with("*.") {
        let mut reverse_labels = hostname
            .split('.')
            .skip(1)
            .map(|label| label.to_string())
            .collect::<Vec<String>>();
        reverse_labels.reverse();
        HostMatch::Suffix { reverse_labels }
    } else {
        HostMatch::Exact(hostname)
    }
}
