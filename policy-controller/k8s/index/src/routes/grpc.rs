use anyhow::Result;
use linkerd_policy_controller_core::routes;
use linkerd_policy_controller_k8s_api::gateway as k8s_gateway_api;

pub fn try_match(
    k8s_gateway_api::GrpcRouteMatch { headers, method }: k8s_gateway_api::GrpcRouteMatch,
) -> Result<routes::GrpcRouteMatch> {
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

    Ok(routes::GrpcRouteMatch { headers, method })
}
