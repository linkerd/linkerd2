use anyhow::{bail, Result};
use linkerd_policy_controller_core::routes;
use linkerd_policy_controller_k8s_api::gateway;
use std::num::NonZeroU16;

pub fn try_match(
    gateway::HttpRouteRulesMatches {
        path,
        headers,
        query_params,
        method,
    }: gateway::HttpRouteRulesMatches,
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
        gateway::HttpRouteRulesMatchesMethod::Get => routes::Method::GET,
        gateway::HttpRouteRulesMatchesMethod::Head => routes::Method::HEAD,
        gateway::HttpRouteRulesMatchesMethod::Post => routes::Method::POST,
        gateway::HttpRouteRulesMatchesMethod::Put => routes::Method::PUT,
        gateway::HttpRouteRulesMatchesMethod::Delete => routes::Method::DELETE,
        gateway::HttpRouteRulesMatchesMethod::Connect => routes::Method::CONNECT,
        gateway::HttpRouteRulesMatchesMethod::Options => routes::Method::OPTIONS,
        gateway::HttpRouteRulesMatchesMethod::Trace => routes::Method::TRACE,
        gateway::HttpRouteRulesMatchesMethod::Patch => routes::Method::PATCH,
    });

    Ok(routes::HttpRouteMatch {
        path,
        headers,
        query_params,
        method,
    })
}

pub fn path_match(path_match: gateway::HttpRouteRulesMatchesPath) -> Result<routes::PathMatch> {
    let value = path_match.value.unwrap_or_else(|| "/".to_string());
    match path_match.r#type {
        Some(gateway::HttpRouteRulesMatchesPathType::Exact) => {
            if !value.starts_with('/') {
                bail!("HttpPathMatch paths must be absolute (begin with `/`); {value:?} is not an absolute path")
            }
            Ok(routes::PathMatch::Exact(value))
        }
        Some(gateway::HttpRouteRulesMatchesPathType::PathPrefix) | None => {
            if !value.starts_with('/') {
                bail!("HttpPathMatch paths must be absolute (begin with `/`); {value:?} is not an absolute path")
            }
            Ok(routes::PathMatch::Prefix(value))
        }
        Some(gateway::HttpRouteRulesMatchesPathType::RegularExpression) => value
            .parse()
            .map(routes::PathMatch::Regex)
            .map_err(Into::into),
    }
}

pub fn header_match(
    header_match: gateway::HttpRouteRulesMatchesHeaders,
) -> Result<routes::HeaderMatch> {
    match header_match.r#type {
        Some(gateway::HttpRouteRulesMatchesHeadersType::Exact) | None => Ok(
            routes::HeaderMatch::Exact(header_match.name.parse()?, header_match.value.parse()?),
        ),
        Some(gateway::HttpRouteRulesMatchesHeadersType::RegularExpression) => Ok(
            routes::HeaderMatch::Regex(header_match.name.parse()?, header_match.value.parse()?),
        ),
    }
}

pub fn query_param_match(
    query_match: gateway::HttpRouteRulesMatchesQueryParams,
) -> Result<routes::QueryParamMatch> {
    match query_match.r#type {
        Some(gateway::HttpRouteRulesMatchesQueryParamsType::Exact) | None => Ok(
            routes::QueryParamMatch::Exact(query_match.name, query_match.value),
        ),
        Some(gateway::HttpRouteRulesMatchesQueryParamsType::RegularExpression) => Ok(
            routes::QueryParamMatch::Regex(query_match.name, query_match.value.parse()?),
        ),
    }
}

pub fn request_header_modifier(
    gateway::HttpRouteRulesFiltersRequestHeaderModifier { set, add, remove }: gateway::HttpRouteRulesFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::HttpRouteRulesFiltersRequestHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::HttpRouteRulesFiltersRequestHeaderModifierSet { name, value }| {
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
    gateway::HttpRouteRulesBackendRefsFiltersRequestHeaderModifier { set, add, remove }: gateway::HttpRouteRulesBackendRefsFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::HttpRouteRulesBackendRefsFiltersRequestHeaderModifierAdd {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::HttpRouteRulesBackendRefsFiltersRequestHeaderModifierSet {
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
    gateway::HttpRouteRulesFiltersResponseHeaderModifier { set, add, remove }: gateway::HttpRouteRulesFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::HttpRouteRulesFiltersResponseHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::HttpRouteRulesFiltersResponseHeaderModifierSet { name, value }| {
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
    gateway::HttpRouteRulesBackendRefsFiltersResponseHeaderModifier { set, add, remove }: gateway::HttpRouteRulesBackendRefsFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::HttpRouteRulesBackendRefsFiltersResponseHeaderModifierAdd {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::HttpRouteRulesBackendRefsFiltersResponseHeaderModifierSet {
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
    gateway::HttpRouteRulesFiltersRequestRedirect {
        scheme,
        hostname,
        path,
        port,
        status_code,
    }: gateway::HttpRouteRulesFiltersRequestRedirect,
) -> Result<routes::RequestRedirectFilter> {
    let scheme = scheme.map(|s| match s {
        gateway::HttpRouteRulesFiltersRequestRedirectScheme::Http => routes::Scheme::HTTP,
        gateway::HttpRouteRulesFiltersRequestRedirectScheme::Https => routes::Scheme::HTTPS,
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
    gateway::HttpRouteRulesBackendRefsFiltersRequestRedirect {
        scheme,
        hostname,
        path,
        port,
        status_code,
    }: gateway::HttpRouteRulesBackendRefsFiltersRequestRedirect,
) -> Result<routes::RequestRedirectFilter> {
    let scheme = scheme.map(|s| match s {
        gateway::HttpRouteRulesBackendRefsFiltersRequestRedirectScheme::Http => {
            routes::Scheme::HTTP
        }
        gateway::HttpRouteRulesBackendRefsFiltersRequestRedirectScheme::Https => {
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
    path_modifier: gateway::HttpRouteRulesFiltersRequestRedirectPath,
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
    path_modifier: gateway::HttpRouteRulesBackendRefsFiltersRequestRedirectPath,
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
