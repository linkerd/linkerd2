use kube::{Resource, ResourceExt};
use linkerd_policy_controller_core::http_route::{GroupKindName, GroupKindNamespaceName};
use linkerd_policy_controller_k8s_api::{gateway as api, policy};

pub(crate) mod grpc;
pub(crate) mod http;

#[derive(Debug, Clone)]
pub(crate) enum RouteResource {
    LinkerdHttp(policy::HttpRoute),
    GatewayHttp(api::HttpRoute),
    #[allow(dead_code)]
    GatewayGrpc(api::GrpcRoute),
}

impl RouteResource {
    pub(crate) fn name(&self) -> String {
        match self {
            RouteResource::LinkerdHttp(route) => route.name_unchecked(),
            RouteResource::GatewayHttp(route) => route.name_unchecked(),
            RouteResource::GatewayGrpc(route) => route.name_unchecked(),
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
            RouteResource::GatewayGrpc(route) => {
                route.namespace().expect("GrpcRoute must have a namespace")
            }
        }
    }

    pub(crate) fn inner(&self) -> &api::CommonRouteSpec {
        match self {
            RouteResource::LinkerdHttp(route) => &route.spec.inner,
            RouteResource::GatewayHttp(route) => &route.spec.inner,
            RouteResource::GatewayGrpc(route) => &route.spec.inner,
        }
    }

    pub(crate) fn status(&self) -> Option<&api::RouteStatus> {
        match self {
            RouteResource::LinkerdHttp(route) => route.status.as_ref().map(|status| &status.inner),
            RouteResource::GatewayHttp(route) => route.status.as_ref().map(|status| &status.inner),
            RouteResource::GatewayGrpc(route) => route.status.as_ref().map(|status| &status.inner),
        }
    }

    pub(crate) fn gknn(&self) -> GroupKindNamespaceName {
        match self {
            RouteResource::LinkerdHttp(route) => gkn_for_resource(route)
                .namespaced(route.namespace().expect("Route must have namespace")),
            RouteResource::GatewayHttp(route) => gkn_for_resource(route)
                .namespaced(route.namespace().expect("Route must have namespace")),
            RouteResource::GatewayGrpc(route) => gkn_for_resource(route)
                .namespaced(route.namespace().expect("Route must have namespace")),
        }
    }
}

pub(crate) fn gkn_for_resource<T>(t: &T) -> GroupKindName
where
    T: Resource<DynamicType = ()>,
{
    let kind = T::kind(&());
    let group = T::group(&());
    let name = t.name_unchecked().into();
    GroupKindName { group, kind, name }
}
