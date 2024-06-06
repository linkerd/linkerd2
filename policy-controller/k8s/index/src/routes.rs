use linkerd_policy_controller_core::routes::{GroupKindName, GroupKindNamespaceName};
use linkerd_policy_controller_k8s_api::{gateway as api, policy, Resource, ResourceExt};

pub mod http;

#[derive(Debug, Clone)]
pub(crate) enum RouteResource {
    LinkerdHttp(policy::HttpRoute),
    GatewayHttp(api::HttpRoute),
}

impl RouteResource {
    pub(crate) fn name(&self) -> String {
        match self {
            RouteResource::LinkerdHttp(route) => route.name_unchecked(),
            RouteResource::GatewayHttp(route) => route.name_unchecked(),
        }
    }

    pub(crate) fn namespace(&self) -> String {
        match self {
            RouteResource::LinkerdHttp(route) => {
                route.namespace().expect("HttpRoute must have a namespace")
            }
            RouteResource::GatewayHttp(route) => {
                route.namespace().expect("HttpRoute must have a namespace")
            }
        }
    }

    pub(crate) fn inner(&self) -> &api::CommonRouteSpec {
        match self {
            RouteResource::LinkerdHttp(route) => &route.spec.inner,
            RouteResource::GatewayHttp(route) => &route.spec.inner,
        }
    }

    pub(crate) fn status(&self) -> Option<&api::RouteStatus> {
        match self {
            RouteResource::LinkerdHttp(route) => route.status.as_ref().map(|status| &status.inner),
            RouteResource::GatewayHttp(route) => route.status.as_ref().map(|status| &status.inner),
        }
    }

    pub(crate) fn gknn(&self) -> GroupKindNamespaceName {
        match self {
            RouteResource::LinkerdHttp(route) => route
                .gkn()
                .namespaced(route.namespace().expect("Route must have namespace")),
            RouteResource::GatewayHttp(route) => route
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

impl<T: AsRef<str>> ExplicitGKN for T {
    fn gkn<R: Resource<DynamicType = ()>>(&self) -> GroupKindName {
        let (kind, group, name) = (
            R::kind(&()),
            R::group(&()),
            self.as_ref().to_string().into(),
        );

        GroupKindName { group, kind, name }
    }
}
