use anyhow::{bail, Result};
use http::{uri::Scheme, Method};
use linkerd_policy_controller_core::routes;
use linkerd_policy_controller_k8s_api::gateway::httproutes as api;
use std::num::NonZeroU16;

pub fn try_match(
    api::HTTPRouteRulesMatches {
        path,
        headers,
        query_params,
        method,
    }: api::HTTPRouteRulesMatches,
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

    let method = method.map(|m| match m {
        api::HTTPRouteRulesMatchesMethod::Get => Method::GET,
        api::HTTPRouteRulesMatchesMethod::Head => Method::HEAD,
        api::HTTPRouteRulesMatchesMethod::Post => Method::POST,
        api::HTTPRouteRulesMatchesMethod::Put => Method::PUT,
        api::HTTPRouteRulesMatchesMethod::Delete => Method::DELETE,
        api::HTTPRouteRulesMatchesMethod::Connect => Method::CONNECT,
        api::HTTPRouteRulesMatchesMethod::Options => Method::OPTIONS,
        api::HTTPRouteRulesMatchesMethod::Trace => Method::TRACE,
        api::HTTPRouteRulesMatchesMethod::Patch => Method::PATCH,
    });

    Ok(routes::HttpRouteMatch {
        path,
        headers,
        query_params,
        method,
    })
}

pub fn path_match(path_match: api::HTTPRouteRulesMatchesPath) -> Result<routes::PathMatch> {
    let value = path_match.value.unwrap_or_else(|| "/".to_string());
    match path_match.r#type {
        Some(api::HTTPRouteRulesMatchesPathType::Exact) => {
            if !value.starts_with('/') {
                bail!("HttpPathMatch paths must be absolute (begin with `/`); {value:?} is not an absolute path")
            }
            Ok(routes::PathMatch::Exact(value))
        }
        Some(api::HTTPRouteRulesMatchesPathType::PathPrefix) | None => {
            if !value.starts_with('/') {
                bail!("HttpPathMatch paths must be absolute (begin with `/`); {value:?} is not an absolute path")
            }
            Ok(routes::PathMatch::Prefix(value))
        }
        Some(api::HTTPRouteRulesMatchesPathType::RegularExpression) => value
            .parse()
            .map(routes::PathMatch::Regex)
            .map_err(Into::into),
    }
}

pub fn header_match(
    header_match: api::HTTPRouteRulesMatchesHeaders,
) -> Result<routes::HeaderMatch> {
    match header_match.r#type {
        Some(api::HTTPRouteRulesMatchesHeadersType::Exact) | None => Ok(
            routes::HeaderMatch::Exact(header_match.name.parse()?, header_match.value.parse()?),
        ),
        Some(api::HTTPRouteRulesMatchesHeadersType::RegularExpression) => Ok(
            routes::HeaderMatch::Regex(header_match.name.parse()?, header_match.value.parse()?),
        ),
    }
}

pub fn query_param_match(
    query_match: api::HTTPRouteRulesMatchesQueryParams,
) -> Result<routes::QueryParamMatch> {
    match query_match.r#type {
        Some(api::HTTPRouteRulesMatchesQueryParamsType::Exact) | None => Ok(
            routes::QueryParamMatch::Exact(query_match.name, query_match.value),
        ),
        Some(api::HTTPRouteRulesMatchesQueryParamsType::RegularExpression) => Ok(
            routes::QueryParamMatch::Regex(query_match.name, query_match.value.parse()?),
        ),
    }
}

pub fn request_header_modifier(
    api::HTTPRouteRulesFiltersRequestHeaderModifier { set, add, remove }: api::HTTPRouteRulesFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |api::HTTPRouteRulesFiltersRequestHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |api::HTTPRouteRulesFiltersRequestHeaderModifierSet { name, value }| {
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
    api::HTTPRouteRulesBackendRefsFiltersRequestHeaderModifier { set, add, remove }: api::HTTPRouteRulesBackendRefsFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |api::HTTPRouteRulesBackendRefsFiltersRequestHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |api::HTTPRouteRulesBackendRefsFiltersRequestHeaderModifierSet { name, value }| {
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

pub fn response_header_modifier(
    api::HTTPRouteRulesFiltersResponseHeaderModifier { set, add, remove }: api::HTTPRouteRulesFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |api::HTTPRouteRulesFiltersResponseHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |api::HTTPRouteRulesFiltersResponseHeaderModifierSet { name, value }| {
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
    api::HTTPRouteRulesBackendRefsFiltersResponseHeaderModifier { set, add, remove }: api::HTTPRouteRulesBackendRefsFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |api::HTTPRouteRulesBackendRefsFiltersResponseHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |api::HTTPRouteRulesBackendRefsFiltersResponseHeaderModifierSet { name, value }| {
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

pub fn req_redirect(
    api::HTTPRouteRulesFiltersRequestRedirect {
        scheme,
        hostname,
        path,
        port,
        status_code,
    }: api::HTTPRouteRulesFiltersRequestRedirect,
) -> Result<routes::RequestRedirectFilter> {
    let scheme = scheme.map(|s| match s {
        api::HTTPRouteRulesFiltersRequestRedirectScheme::Http => Scheme::HTTP,
        api::HTTPRouteRulesFiltersRequestRedirectScheme::Https => Scheme::HTTPS,
    });
    Ok(routes::RequestRedirectFilter {
        scheme,
        host: hostname,
        path: path.map(path_modifier).transpose()?,
        port: port
            .and_then(|p| p.try_into().ok())
            .and_then(NonZeroU16::new),
        status: status_code
            .map(|s| s.try_into().unwrap_or_default())
            .map(routes::StatusCode::from_u16)
            .transpose()?,
    })
}

pub fn backend_req_redirect(
    api::HTTPRouteRulesBackendRefsFiltersRequestRedirect {
        scheme,
        hostname,
        path,
        port,
        status_code,
    }: api::HTTPRouteRulesBackendRefsFiltersRequestRedirect,
) -> Result<routes::RequestRedirectFilter> {
    let scheme = scheme.map(|s| match s {
        api::HTTPRouteRulesBackendRefsFiltersRequestRedirectScheme::Http => Scheme::HTTP,
        api::HTTPRouteRulesBackendRefsFiltersRequestRedirectScheme::Https => Scheme::HTTPS,
    });
    Ok(routes::RequestRedirectFilter {
        scheme,
        host: hostname,
        path: path.map(backend_path_modifier).transpose()?,
        port: port
            .and_then(|p| p.try_into().ok())
            .and_then(NonZeroU16::new),
        status: status_code
            .map(|s| s.try_into().unwrap_or_default())
            .map(routes::StatusCode::from_u16)
            .transpose()?,
    })
}

fn path_modifier(
    path_modifier: api::HTTPRouteRulesFiltersRequestRedirectPath,
) -> Result<routes::PathModifier> {
    if let Some(path) = path_modifier.replace_full_path {
        if !path.starts_with('/') {
            bail!(
                "RequestRedirect filters may only contain absolute paths \
                    (starting with '/'); {path:?} is not an absolute path"
            )
        }
        return Ok(routes::PathModifier::Full(path));
    }
    if let Some(path) = path_modifier.replace_prefix_match {
        if !path.starts_with('/') {
            bail!(
                "RequestRedirect filters may only contain absolute paths \
                    (starting with '/'); {path:?} is not an absolute path"
            )
        }
        return Ok(routes::PathModifier::Prefix(path));
    }
    bail!("RequestRedirect filter must contain either replace_full_path or replace_prefix_match")
}

fn backend_path_modifier(
    path_modifier: api::HTTPRouteRulesBackendRefsFiltersRequestRedirectPath,
) -> Result<routes::PathModifier> {
    if let Some(path) = path_modifier.replace_full_path {
        if !path.starts_with('/') {
            bail!(
                "RequestRedirect filters may only contain absolute paths \
                    (starting with '/'); {path:?} is not an absolute path"
            )
        }
        return Ok(routes::PathModifier::Full(path));
    }
    if let Some(path) = path_modifier.replace_prefix_match {
        if !path.starts_with('/') {
            bail!(
                "RequestRedirect filters may only contain absolute paths \
                    (starting with '/'); {path:?} is not an absolute path"
            )
        }
        return Ok(routes::PathModifier::Prefix(path));
    }
    bail!("RequestRedirect filter must contain either replace_full_path or replace_prefix_match")
}
