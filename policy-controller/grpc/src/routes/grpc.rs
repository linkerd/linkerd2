use linkerd2_proxy_api::{grpc_route, http_route};
use linkerd_policy_controller_core::routes::{GrpcRouteMatch, HeaderMatch};

pub(crate) fn convert_match(
    GrpcRouteMatch { headers, method }: GrpcRouteMatch,
) -> grpc_route::GrpcRouteMatch {
    let headers = headers
        .into_iter()
        .map(|rule| match rule {
            HeaderMatch::Exact(name, value) => http_route::HeaderMatch {
                name: name.to_string(),
                value: Some(http_route::header_match::Value::Exact(
                    value.as_bytes().to_vec(),
                )),
            },
            HeaderMatch::Regex(name, re) => http_route::HeaderMatch {
                name: name.to_string(),
                value: Some(http_route::header_match::Value::Regex(re.to_string())),
            },
        })
        .collect();

    let rpc = method.map(|value| grpc_route::GrpcRpcMatch {
        method: value.method.unwrap_or_default(),
        service: value.service.unwrap_or_default(),
    });

    grpc_route::GrpcRouteMatch { rpc, headers }
}
