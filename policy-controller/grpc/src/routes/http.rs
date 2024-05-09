use linkerd2_proxy_api::http_route as proto;
use linkerd_policy_controller_core::routes::{
    HeaderMatch, HttpRouteMatch, PathMatch, QueryParamMatch,
};

pub(crate) fn convert_match(
    HttpRouteMatch {
        headers,
        path,
        query_params,
        method,
    }: HttpRouteMatch,
) -> proto::HttpRouteMatch {
    let headers = headers
        .into_iter()
        .map(|hm| match hm {
            HeaderMatch::Exact(name, value) => proto::HeaderMatch {
                name: name.to_string(),
                value: Some(proto::header_match::Value::Exact(value.as_bytes().to_vec())),
            },
            HeaderMatch::Regex(name, re) => proto::HeaderMatch {
                name: name.to_string(),
                value: Some(proto::header_match::Value::Regex(re.to_string())),
            },
        })
        .collect();

    let path = path.map(|path| proto::PathMatch {
        kind: Some(match path {
            PathMatch::Exact(path) => proto::path_match::Kind::Exact(path),
            PathMatch::Prefix(prefix) => proto::path_match::Kind::Prefix(prefix),
            PathMatch::Regex(regex) => proto::path_match::Kind::Regex(regex.to_string()),
        }),
    });

    let query_params = query_params
        .into_iter()
        .map(|qpm| match qpm {
            QueryParamMatch::Exact(name, value) => proto::QueryParamMatch {
                name,
                value: Some(proto::query_param_match::Value::Exact(value)),
            },
            QueryParamMatch::Regex(name, re) => proto::QueryParamMatch {
                name,
                value: Some(proto::query_param_match::Value::Regex(re.to_string())),
            },
        })
        .collect();

    proto::HttpRouteMatch {
        headers,
        path,
        query_params,
        method: method.map(Into::into),
    }
}
