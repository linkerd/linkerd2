use linkerd2_proxy_api::{http_route, http_types};
use linkerd_policy_controller_core::routes::{
    FailureInjectorFilter, HeaderMatch, HttpRouteMatch, PathMatch, QueryParamMatch,
};

pub(crate) fn convert_match(
    HttpRouteMatch {
        headers,
        path,
        query_params,
        method,
    }: HttpRouteMatch,
) -> http_route::HttpRouteMatch {
    let headers = headers
        .into_iter()
        .map(|hm| match hm {
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

    let path = path.map(|path| http_route::PathMatch {
        kind: Some(match path {
            PathMatch::Exact(path) => http_route::path_match::Kind::Exact(path),
            PathMatch::Prefix(prefix) => http_route::path_match::Kind::Prefix(prefix),
            PathMatch::Regex(regex) => http_route::path_match::Kind::Regex(regex.to_string()),
        }),
    });

    let query_params = query_params
        .into_iter()
        .map(|qpm| match qpm {
            QueryParamMatch::Exact(name, value) => http_route::QueryParamMatch {
                name,
                value: Some(http_route::query_param_match::Value::Exact(value)),
            },
            QueryParamMatch::Regex(name, re) => http_route::QueryParamMatch {
                name,
                value: Some(http_route::query_param_match::Value::Regex(re.to_string())),
            },
        })
        .collect();

    http_route::HttpRouteMatch {
        headers,
        path,
        query_params,
        method: method.map(|m| {
            if let Some(m) = http_types::http_method::Registered::from_str_name(m.as_str()) {
                http_types::HttpMethod {
                    r#type: Some(http_types::http_method::Type::Registered(m.into())),
                }
            } else {
                http_types::HttpMethod {
                    r#type: Some(http_types::http_method::Type::Unregistered(m.to_string())),
                }
            }
        }),
    }
}

pub(crate) fn convert_failure_injector_filter(
    FailureInjectorFilter {
        status,
        message,
        ratio,
    }: FailureInjectorFilter,
) -> http_route::HttpFailureInjector {
    http_route::HttpFailureInjector {
        status: u32::from(status.as_u16()),
        message,
        ratio: Some(http_route::Ratio {
            numerator: ratio.numerator,
            denominator: ratio.denominator,
        }),
    }
}
