use anyhow::Result;
use linkerd_policy_controller_core::routes;
use linkerd_policy_controller_k8s_api::gateway;

pub fn try_match(
    gateway::GRPCRouteRulesMatches { headers, method }: gateway::GRPCRouteRulesMatches,
) -> Result<routes::GrpcRouteMatch> {
    let headers = headers
        .into_iter()
        .flatten()
        .map(super::http::header_match)
        .collect::<Result<_>>()?;

    let method = method.map(|value| match value {
        gateway::GrpcMethodMatch::Exact { method, service }
        | gateway::GrpcMethodMatch::RegularExpression { method, service } => {
            routes::GrpcMethodMatch { method, service }
        }
    });

    Ok(routes::GrpcRouteMatch { headers, method })
}

pub fn header_match(header_match: gateway::GrpcHeaderMatch) -> Result<routes::HeaderMatch> {
    match header_match {
        gateway::GrpcHeaderMatch::Exact { name, value } => {
            Ok(routes::HeaderMatch::Exact(name.parse()?, value.parse()?))
        }
        gateway::GrpcHeaderMatch::RegularExpression { name, value } => {
            Ok(routes::HeaderMatch::Regex(name.parse()?, value.parse()?))
        }
    }
}

pub fn request_header_modifier(
    gateway::GRPCRouteRulesFiltersRequestHeaderModifier { set, add, remove }: gateway::GRPCRouteRulesFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::GRPCRouteRulesFiltersRequestHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::GRPCRouteRulesFiltersRequestHeaderModifierSet { name, value }| {
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
    gateway::GRPCRouteRulesBackendRefsFiltersRequestHeaderModifier { set, add, remove }: gateway::GRPCRouteRulesBackendRefsFiltersRequestHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::GRPCRouteRulesBackendRefsFiltersRequestHeaderModifierAdd {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::GRPCRouteRulesBackendRefsFiltersRequestHeaderModifierSet {
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
    gateway::GRPCRouteRulesFiltersResponseHeaderModifier { set, add, remove }: gateway::GRPCRouteRulesFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::GRPCRouteRulesFiltersResponseHeaderModifierAdd { name, value }| {
                    Ok((name.parse()?, value.parse()?))
                },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::GRPCRouteRulesFiltersResponseHeaderModifierSet { name, value }| {
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
    gateway::GRPCRouteRulesBackendRefsFiltersResponseHeaderModifier { set, add, remove }: gateway::GRPCRouteRulesBackendRefsFiltersResponseHeaderModifier,
) -> Result<routes::HeaderModifierFilter> {
    Ok(routes::HeaderModifierFilter {
        add: add
            .into_iter()
            .flatten()
            .map(
                |gateway::GRPCRouteRulesBackendRefsFiltersResponseHeaderModifierAdd {
                     name,
                     value,
                 }| { Ok((name.parse()?, value.parse()?)) },
            )
            .collect::<Result<Vec<_>>>()?,
        set: set
            .into_iter()
            .flatten()
            .map(
                |gateway::GRPCRouteRulesBackendRefsFiltersResponseHeaderModifierSet {
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
