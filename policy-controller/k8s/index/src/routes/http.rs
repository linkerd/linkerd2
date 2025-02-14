use anyhow::{anyhow, bail, Result};
use linkerd_policy_controller_core::routes;
use linkerd_policy_controller_k8s_api::gateway;
use std::num::NonZeroU16;

pub fn try_match(
    gateway::HTTPRouteRulesMatches {
        path,
        headers,
        query_params,
        method,
    }: gateway::HTTPRouteRulesMatches,
) -> Result<routes::HttpRouteMatch> {
    let path = path.map(path_match).transpose()?;

    let headers = headers
        .into_iter()
        .flatten()
        .map(header_match)
        .collect::<Result<_>>()?;

    let query_params = query_params
        .into_iter()
        .flatten()
        .map(query_param_match)
        .collect::<Result<_>>()?;

    let method = method.as_deref().and_then(|m| match m {
        gateway::http_method::GET => Some(routes::Method::GET),
        gateway::http_method::HEAD => Some(routes::Method::HEAD),
        gateway::http_method::POST => Some(routes::Method::POST),
        gateway::http_method::PUT => Some(routes::Method::PUT),
        gateway::http_method::DELETE => Some(routes::Method::DELETE),
        gateway::http_method::CONNECT => Some(routes::Method::CONNECT),
        gateway::http_method::OPTIONS => Some(routes::Method::OPTIONS),
        gateway::http_method::TRACE => Some(routes::Method::TRACE),
        gateway::http_method::PATCH => Some(routes::Method::PATCH),
        _ => None,
    });

    Ok(routes::HttpRouteMatch {
        path,
        headers,
        query_params,
        method,
    })
}

pub fn path_match(path_match: gateway::HttpPathMatch) -> Result<routes::PathMatch> {
    match path_match {
        gateway::HttpPathMatch::Exact { value } | gateway::HttpPathMatch::PathPrefix { value }
        if !value.starts_with('/') =>
            {
                Err(anyhow!("HttpPathMatch paths must be absolute (begin with `/`); {value:?} is not an absolute path"))
            }
        gateway::HttpPathMatch::Exact { value } => Ok(routes::PathMatch::Exact(value)),
        gateway::HttpPathMatch::PathPrefix { value } => Ok(routes::PathMatch::Prefix(value)),
        gateway::HttpPathMatch::RegularExpression { value } => value
            .parse()
            .map(routes::PathMatch::Regex)
            .map_err(Into::into),
    }
}

pub fn header_match(header_match: gateway::HttpHeaderMatch) -> Result<routes::HeaderMatch> {
    match header_match {
        gateway::HttpHeaderMatch::Exact { name, value } => {
            Ok(routes::HeaderMatch::Exact(name.parse()?, value.parse()?))
        }
        gateway::HttpHeaderMatch::RegularExpression { name, value } => {
            Ok(routes::HeaderMatch::Regex(name.parse()?, value.parse()?))
        }
    }
}

pub fn query_param_match(
    query_match: gateway::HttpQueryParamMatch,
) -> Result<routes::QueryParamMatch> {
    match query_match {
        gateway::HttpQueryParamMatch::Exact { name, value } => {
            Ok(routes::QueryParamMatch::Exact(name, value))
        }
        gateway::HttpQueryParamMatch::RegularExpression { name, value } => {
            Ok(routes::QueryParamMatch::Regex(name, value.parse()?))
        }
    }
}

pub fn request_header_modifier(
    gateway::HTTPRouteRulesFiltersRequestHeaderModifier { set, add, remove }: gateway::HTTPRouteRulesFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRulesFiltersRequestHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRulesFiltersRequestHeaderModifierSet { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        remove: remove
            .into_iter()
            .flatten()
            .map(routes::HeaderName::try_from)
            .collect::<Result<_, _>>()?,
    })
}

pub fn backend_request_header_modifier(
    gateway::HTTPRouteRulesBackendRefsFiltersRequestHeaderModifier { set, add, remove }: gateway::HTTPRouteRulesBackendRefsFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRulesBackendRefsFiltersRequestHeaderModifierAdd {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRulesBackendRefsFiltersRequestHeaderModifierSet {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        remove: remove
            .into_iter()
            .flatten()
            .map(routes::HeaderName::try_from)
            .collect::<Result<_, _>>()?,
    })
}

pub fn response_header_modifier(
    gateway::HTTPRouteRulesFiltersResponseHeaderModifier { set, add, remove }: gateway::HTTPRouteRulesFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRulesFiltersResponseHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRulesFiltersResponseHeaderModifierSet { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        remove: remove
            .into_iter()
            .flatten()
            .map(routes::HeaderName::try_from)
            .collect::<Result<_, _>>()?,
    })
}

pub fn backend_response_header_modifier(
    gateway::HTTPRouteRulesBackendRefsFiltersResponseHeaderModifier { set, add, remove }: gateway::HTTPRouteRulesBackendRefsFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRulesBackendRefsFiltersResponseHeaderModifierAdd {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRulesBackendRefsFiltersResponseHeaderModifierSet {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        remove: remove
            .into_iter()
            .flatten()
            .map(routes::HeaderName::try_from)
            .collect::<Result<_, _>>()?,
    })
}

pub fn req_redirect(
    gateway::HTTPRouteRulesFiltersRequestRedirect {
        scheme,
        hostname,
        path,
        port,
        status_code,
    }: gateway::HTTPRouteRulesFiltersRequestRedirect,
) -> Result<routes::RequestRedirectFilter> {
    let scheme = scheme.as_deref().and_then(|s| match s {
        gateway::http_scheme::HTTP => Some(routes::Scheme::HTTP),
        gateway::http_scheme::HTTPS => Some(routes::Scheme::HTTPS),
        _ => None,
    });
    Ok(routes::RequestRedirectFilter {
        scheme,
        host: hostname,
        path: path.map(path_modifier).transpose()?,
        port: port.and_then(NonZeroU16::new),
        status: status_code.map(routes::StatusCode::from_u16).transpose()?,
    })
}

pub fn backend_req_redirect(
    gateway::HTTPRouteRulesBackendRefsFiltersRequestRedirect {
        scheme,
        hostname,
        path,
        port,
        status_code,
    }: gateway::HTTPRouteRulesBackendRefsFiltersRequestRedirect,
) -> Result<routes::RequestRedirectFilter> {
    let scheme = scheme.as_deref().and_then(|s| match s {
        gateway::http_scheme::HTTP => Some(routes::Scheme::HTTP),
        gateway::http_scheme::HTTPS => Some(routes::Scheme::HTTPS),
        _ => None,
    });
    Ok(routes::RequestRedirectFilter {
        scheme,
        host: hostname,
        path: path.map(backend_path_modifier).transpose()?,
        port: port.and_then(NonZeroU16::new),
        status: status_code.map(routes::StatusCode::from_u16).transpose()?,
    })
}

fn path_modifier(
    path_modifier: gateway::HTTPRouteRulesFiltersRequestRedirectPath,
) -> Result<routes::PathModifier> {
    use gateway::HttpPathModifier::*;
    match path_modifier {
        ReplaceFullPath {
            replace_full_path: path,
        }
        | ReplacePrefixMatch {
            replace_prefix_match: path,
        } if !path.starts_with('/') => {
            bail!(
                "RequestRedirect filters may only contain absolute paths \
                    (starting with '/'); {path:?} is not an absolute path"
            )
        }
        ReplaceFullPath { replace_full_path } => Ok(routes::PathModifier::Full(replace_full_path)),
        ReplacePrefixMatch {
            replace_prefix_match,
        } => Ok(routes::PathModifier::Prefix(replace_prefix_match)),
    }
}

fn backend_path_modifier(
    path_modifier: gateway::HTTPRouteRulesBackendRefsFiltersRequestRedirectPath,
) -> Result<routes::PathModifier> {
    use gateway::HttpPathModifier::*;
    match path_modifier {
        ReplaceFullPath {
            replace_full_path: path,
        }
        | ReplacePrefixMatch {
            replace_prefix_match: path,
        } if !path.starts_with('/') => {
            bail!(
                "RequestRedirect filters may only contain absolute paths \
                    (starting with '/'); {path:?} is not an absolute path"
            )
        }
        ReplaceFullPath { replace_full_path } => Ok(routes::PathModifier::Full(replace_full_path)),
        ReplacePrefixMatch {
            replace_prefix_match,
        } => Ok(routes::PathModifier::Prefix(replace_prefix_match)),
    }
}
