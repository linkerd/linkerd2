use anyhow::{bail, Result};
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

    let method = method.map(|m| match m {
        gateway::HTTPRouteRulesMatchesMethod::Get => routes::Method::GET,
        gateway::HTTPRouteRulesMatchesMethod::Head => routes::Method::HEAD,
        gateway::HTTPRouteRulesMatchesMethod::Post => routes::Method::POST,
        gateway::HTTPRouteRulesMatchesMethod::Put => routes::Method::PUT,
        gateway::HTTPRouteRulesMatchesMethod::Delete => routes::Method::DELETE,
        gateway::HTTPRouteRulesMatchesMethod::Connect => routes::Method::CONNECT,
        gateway::HTTPRouteRulesMatchesMethod::Options => routes::Method::OPTIONS,
        gateway::HTTPRouteRulesMatchesMethod::Trace => routes::Method::TRACE,
        gateway::HTTPRouteRulesMatchesMethod::Patch => routes::Method::PATCH,
    });

    Ok(routes::HttpRouteMatch {
        path,
        headers,
        query_params,
        method,
    })
}

pub fn path_match(path_match: gateway::HTTPRouteRulesMatchesPath) -> Result<routes::PathMatch> {
    let value = path_match.value.unwrap_or_else(|| "/".to_string());
    match path_match.r#type {
        Some(gateway::HTTPRouteRulesMatchesPathType::Exact) => {
            if !value.starts_with('/') {
                bail!("HttpPathMatch paths must be absolute (begin with `/`); {value:?} is not an absolute path")
            }
            Ok(routes::PathMatch::Exact(value))
        }
        Some(gateway::HTTPRouteRulesMatchesPathType::PathPrefix) | None => {
            if !value.starts_with('/') {
                bail!("HttpPathMatch paths must be absolute (begin with `/`); {value:?} is not an absolute path")
            }
            Ok(routes::PathMatch::Prefix(value))
        }
        Some(gateway::HTTPRouteRulesMatchesPathType::RegularExpression) => value
            .parse()
            .map(routes::PathMatch::Regex)
            .map_err(Into::into),
    }
}

pub fn header_match(
    header_match: gateway::HTTPRouteRulesMatchesHeaders,
) -> Result<routes::HeaderMatch> {
    match header_match.r#type {
        Some(gateway::HTTPRouteRulesMatchesHeadersType::Exact) | None => Ok(
            routes::HeaderMatch::Exact(header_match.name.parse()?, header_match.value.parse()?),
        ),
        Some(gateway::HTTPRouteRulesMatchesHeadersType::RegularExpression) => Ok(
            routes::HeaderMatch::Regex(header_match.name.parse()?, header_match.value.parse()?),
        ),
    }
}

pub fn query_param_match(
    query_match: gateway::HTTPRouteRulesMatchesQueryParams,
) -> Result<routes::QueryParamMatch> {
    match query_match.r#type {
        Some(gateway::HTTPRouteRulesMatchesQueryParamsType::Exact) | None => Ok(
            routes::QueryParamMatch::Exact(query_match.name, query_match.value),
        ),
        Some(gateway::HTTPRouteRulesMatchesQueryParamsType::RegularExpression) => Ok(
            routes::QueryParamMatch::Regex(query_match.name, query_match.value.parse()?),
        ),
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
    let scheme = scheme.map(|s| match s {
        gateway::HTTPRouteRulesFiltersRequestRedirectScheme::Http => routes::Scheme::HTTP,
        gateway::HTTPRouteRulesFiltersRequestRedirectScheme::Https => routes::Scheme::HTTPS,
    });
    Ok(routes::RequestRedirectFilter {
        scheme,
        host: hostname,
        path: path.map(path_modifier).transpose()?,
        port: port
            .and_then(|p| p.try_into().ok())
            .and_then(NonZeroU16::new),
        status: status_code
            .map(|code| code.try_into())
            .transpose()?
            .map(routes::StatusCode::from_u16)
            .transpose()?,
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
    let scheme = scheme.map(|s| match s {
        gateway::HTTPRouteRulesBackendRefsFiltersRequestRedirectScheme::Http => {
            routes::Scheme::HTTP
        }
        gateway::HTTPRouteRulesBackendRefsFiltersRequestRedirectScheme::Https => {
            routes::Scheme::HTTPS
        }
    });
    Ok(routes::RequestRedirectFilter {
        scheme,
        host: hostname,
        path: path.map(backend_path_modifier).transpose()?,
        port: port
            .and_then(|p| p.try_into().ok())
            .and_then(NonZeroU16::new),
        status: status_code
            .map(|code| code.try_into())
            .transpose()?
            .map(routes::StatusCode::from_u16)
            .transpose()?,
    })
}

fn path_modifier(
    path_modifier: gateway::HTTPRouteRulesFiltersRequestRedirectPath,
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
    path_modifier: gateway::HTTPRouteRulesBackendRefsFiltersRequestRedirectPath,
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
