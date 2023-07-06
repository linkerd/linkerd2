use linkerd2_proxy_api::{http_route as proto, http_types};
use linkerd_policy_controller_core::http_route::{
    FailureInjectorFilter, HeaderMatch, HeaderModifierFilter, HostMatch, HttpRouteMatch, PathMatch,
    PathModifier, QueryParamMatch, RequestRedirectFilter,
};

pub(crate) fn convert_host_match(h: HostMatch) -> proto::HostMatch {
    proto::HostMatch {
        r#match: Some(match h {
            HostMatch::Exact(host) => proto::host_match::Match::Exact(host),
            HostMatch::Suffix { reverse_labels } => {
                proto::host_match::Match::Suffix(proto::host_match::Suffix {
                    reverse_labels: reverse_labels.to_vec(),
                })
            }
        }),
    }
}

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

pub(crate) fn convert_failure_injector_filter(
    FailureInjectorFilter {
        status,
        message,
        ratio,
    }: FailureInjectorFilter,
) -> proto::HttpFailureInjector {
    proto::HttpFailureInjector {
        status: u32::from(status.as_u16()),
        message,
        ratio: Some(proto::Ratio {
            numerator: ratio.numerator,
            denominator: ratio.denominator,
        }),
    }
}

pub(crate) fn convert_request_header_modifier_filter(
    HeaderModifierFilter { add, set, remove }: HeaderModifierFilter,
) -> proto::RequestHeaderModifier {
    proto::RequestHeaderModifier {
        add: Some(http_types::Headers {
            headers: add
                .into_iter()
                .map(|(n, v)| http_types::headers::Header {
                    name: n.to_string(),
                    value: v.as_bytes().to_owned(),
                })
                .collect(),
        }),
        set: Some(http_types::Headers {
            headers: set
                .into_iter()
                .map(|(n, v)| http_types::headers::Header {
                    name: n.to_string(),
                    value: v.as_bytes().to_owned(),
                })
                .collect(),
        }),
        remove: remove.into_iter().map(|n| n.to_string()).collect(),
    }
}

pub(crate) fn convert_response_header_modifier_filter(
    HeaderModifierFilter { add, set, remove }: HeaderModifierFilter,
) -> proto::ResponseHeaderModifier {
    proto::ResponseHeaderModifier {
        add: Some(http_types::Headers {
            headers: add
                .into_iter()
                .map(|(n, v)| http_types::headers::Header {
                    name: n.to_string(),
                    value: v.as_bytes().to_owned(),
                })
                .collect(),
        }),
        set: Some(http_types::Headers {
            headers: set
                .into_iter()
                .map(|(n, v)| http_types::headers::Header {
                    name: n.to_string(),
                    value: v.as_bytes().to_owned(),
                })
                .collect(),
        }),
        remove: remove.into_iter().map(|n| n.to_string()).collect(),
    }
}

pub(crate) fn convert_redirect_filter(
    RequestRedirectFilter {
        scheme,
        host,
        path,
        port,
        status,
    }: RequestRedirectFilter,
) -> proto::RequestRedirect {
    proto::RequestRedirect {
        scheme: scheme.map(|ref s| s.into()),
        host: host.unwrap_or_default(),
        path: path.map(|pm| proto::PathModifier {
            replace: Some(match pm {
                PathModifier::Full(p) => proto::path_modifier::Replace::Full(p),
                PathModifier::Prefix(p) => proto::path_modifier::Replace::Prefix(p),
            }),
        }),
        port: port.map(u16::from).map(u32::from).unwrap_or_default(),
        status: u32::from(status.unwrap_or_default().as_u16()),
    }
}
