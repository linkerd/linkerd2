use anyhow::{bail, Result};
use linkerd_policy_controller_core::routes;
use linkerd_policy_controller_k8s_api::gateway;

pub fn try_match(
    gateway::GrpcRouteRulesMatches { headers, method }: gateway::GrpcRouteRulesMatches,
) -> Result<routes::GrpcRouteMatch> {
    let headers = headers
        .into_iter()
        .flatten()
        .map(header_match)
        .collect::<Result<_>>()?;

    let method = method
        .map(|value| {
            if value.r#type == Some(gateway::GrpcRouteRulesMatchesMethodType::RegularExpression) {
                bail!(
                    "unsupported GRPCRoute method match type: {:?}",
                    value.r#type
                );
            }
            Ok(routes::GrpcMethodMatch {
                method: value.method,
                service: value.service,
            })
        })
        .transpose()?;

    Ok(routes::GrpcRouteMatch { headers, method })
}

pub fn header_match(
    header_match: gateway::GrpcRouteRulesMatchesHeaders,
) -> Result<routes::HeaderMatch> {
    match header_match.r#type {
        Some(gateway::GrpcRouteRulesMatchesHeadersType::Exact) | None => Ok(
            routes::HeaderMatch::Exact(header_match.name.parse()?, header_match.value.parse()?),
        ),
        Some(gateway::GrpcRouteRulesMatchesHeadersType::RegularExpression) => Ok(
            routes::HeaderMatch::Regex(header_match.name.parse()?, header_match.value.parse()?),
        ),
    }
}

pub fn request_header_modifier(
    gateway::GrpcRouteRulesFiltersRequestHeaderModifier { set, add, remove }: gateway::GrpcRouteRulesFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::GrpcRouteRulesFiltersRequestHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::GrpcRouteRulesFiltersRequestHeaderModifierSet { name, value }| {
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
    gateway::GrpcRouteRulesBackendRefsFiltersRequestHeaderModifier { set, add, remove }: gateway::GrpcRouteRulesBackendRefsFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::GrpcRouteRulesBackendRefsFiltersRequestHeaderModifierAdd {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::GrpcRouteRulesBackendRefsFiltersRequestHeaderModifierSet {
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
    gateway::GrpcRouteRulesFiltersResponseHeaderModifier { set, add, remove }: gateway::GrpcRouteRulesFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::GrpcRouteRulesFiltersResponseHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::GrpcRouteRulesFiltersResponseHeaderModifierSet { name, value }| {
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
    gateway::GrpcRouteRulesBackendRefsFiltersResponseHeaderModifier { set, add, remove }: gateway::GrpcRouteRulesBackendRefsFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::GrpcRouteRulesBackendRefsFiltersResponseHeaderModifierAdd {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::GrpcRouteRulesBackendRefsFiltersResponseHeaderModifierSet {
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
