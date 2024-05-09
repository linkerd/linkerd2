use linkerd_policy_controller_core::routes::{GrpcRouteMatch, HeaderMatch};

mod proto {
    pub(super) use linkerd2_proxy_api::{
        grpc_route::{GrpcRouteMatch, GrpcRpcMatch},
        http_route::{header_match::Value as HeaderValue, HeaderMatch},
    };
}

#[allow(dead_code)]
pub(crate) fn convert_match(
    GrpcRouteMatch { headers, method }: GrpcRouteMatch,
) -> proto::GrpcRouteMatch {
    let headers = headers
        .into_iter()
        .map(|rule| match rule {
            HeaderMatch::Exact(name, value) => proto::HeaderMatch {
                name: name.to_string(),
                value: Some(proto::HeaderValue::Exact(value.as_bytes().to_vec())),
            },
            HeaderMatch::Regex(name, re) => proto::HeaderMatch {
                name: name.to_string(),
                value: Some(proto::HeaderValue::Regex(re.to_string())),
            },
        })
        .collect();

    let rpc = method.map(|value| proto::GrpcRpcMatch {
        method: value.method.unwrap_or_default(),
        service: value.service.unwrap_or_default(),
    });

    proto::GrpcRouteMatch { rpc, headers }
}
