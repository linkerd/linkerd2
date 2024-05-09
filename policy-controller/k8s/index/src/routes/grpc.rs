use anyhow::Result;
use kube::Resource;
use linkerd_policy_controller_core::routes::{self, GroupKindName, GrpcRouteMatch};
use linkerd_policy_controller_k8s_api::gateway as k8s_gateway_api;

pub fn try_match(
    k8s_gateway_api::GrpcRouteMatch { headers, method }: k8s_gateway_api::GrpcRouteMatch,
) -> Result<routes::RouteMatch> {
    let headers = headers
        .into_iter()
        .flatten()
        .map(super::http::header_match)
        .collect::<Result<_>>()?;

    let method = method.map(|value| match value {
        k8s_gateway_api::GrpcMethodMatch::Exact { method, service }
        | k8s_gateway_api::GrpcMethodMatch::RegularExpression { method, service } => {
            routes::GrpcMethodMatch { method, service }
        }
    });

    Ok(routes::RouteMatch::Grpc(GrpcRouteMatch { headers, method }))
}

pub(crate) fn gkn_for_gateway_grpc_route(name: String) -> GroupKindName {
    GroupKindName {
        group: k8s_gateway_api::GrpcRoute::group(&()),
        kind: k8s_gateway_api::GrpcRoute::kind(&()),
        name: name.into(),
    }
}
