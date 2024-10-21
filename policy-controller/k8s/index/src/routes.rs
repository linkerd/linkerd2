use linkerd_policy_controller_core::routes::{GroupKindName, GroupKindNamespaceName, HostMatch};
use linkerd_policy_controller_k8s_api::{gateway as api, policy, Resource, ResourceExt};

pub mod grpc;
pub mod http;

#[derive(Debug, Clone)]
pub(crate) enum HttpRouteResource {
    LinkerdHttp(policy::HttpRoute),
    GatewayHttp(api::HttpRoute),
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

    pub(crate) fn inner(&self) -> &api::CommonRouteSpec {
        match self {
            Self::LinkerdHttp(route) => &route.spec.inner,
            Self::GatewayHttp(route) => &route.spec.inner,
        }
    }

    pub(crate) fn status(&self) -> Option<&api::RouteStatus> {
        match self {
            Self::LinkerdHttp(route) => route.status.as_ref().map(|status| &status.inner),
            Self::GatewayHttp(route) => route.status.as_ref().map(|status| &status.inner),
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

pub fn host_match(hostname: api::Hostname) -> HostMatch {
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
