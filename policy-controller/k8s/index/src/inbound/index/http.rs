use crate::inbound::routes::{ParentRef, RouteBinding, Status};
use crate::routes::http::try_match;
use ahash::AHashMap as HashMap;
use anyhow::{bail, Error, Result};
use linkerd_policy_controller_core::{
    inbound::{Filter, HttpRoute, InboundRoute, InboundRouteRule},
    routes::HttpRouteMatch,
};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway::httproutes as gateway, policy};

impl TryFrom<gateway::HTTPRoute> for RouteBinding<HttpRoute> {
    type Error = Error;

    fn try_from(route: gateway::HTTPRoute) -> Result<Self, Self::Error> {
        let route_ns = route.metadata.namespace.as_deref();
        let creation_timestamp = route.metadata.creation_timestamp.map(|k8s::Time(t)| t);
        let parents = ParentRef::collect_from_http(route_ns, route.spec.parent_refs)?;
        let hostnames = route
            .spec
            .hostnames
            .into_iter()
            .flatten()
            .map(crate::routes::host_match)
            .collect();

        let rules = route
            .spec
            .rules
            .into_iter()
            .flatten()
            .map(
                |gateway::HTTPRouteRules {
                     matches, filters, ..
                 }| try_http_rule(matches, filters, try_gateway_filter),
            )
            .collect::<Result<_>>()?;

        let statuses = route
            .status
            .map_or_else(Vec::new, Status::collect_from_http);

        Ok(RouteBinding {
            parents,
            route: InboundRoute {
                hostnames,
                rules,
                authorizations: HashMap::default(),
                creation_timestamp,
            },
            statuses,
        })
    }
}

impl TryFrom<policy::HttpRoute> for RouteBinding<HttpRoute> {
    type Error = Error;

    fn try_from(route: policy::HttpRoute) -> Result<Self, Self::Error> {
        let route_ns = route.metadata.namespace.as_deref();
        let creation_timestamp = route.metadata.creation_timestamp.map(|k8s::Time(t)| t);
        let parents = ParentRef::collect_from_http(route_ns, route.spec.parent_refs)?;
        let hostnames = route
            .spec
            .hostnames
            .into_iter()
            .flatten()
            .map(crate::routes::host_match)
            .collect();

        let rules = route
            .spec
            .rules
            .into_iter()
            .flatten()
            .map(
                |policy::httproute::HttpRouteRule {
                     matches, filters, ..
                 }| { try_http_rule(matches, filters, try_policy_filter) },
            )
            .collect::<Result<_>>()?;

        let statuses = route
            .status
            .map_or_else(Vec::new, |status| Status::collect_from_http(status.inner));

        Ok(RouteBinding {
            parents,
            route: InboundRoute {
                hostnames,
                rules,
                authorizations: HashMap::default(),
                creation_timestamp,
            },
            statuses,
        })
    }
}

fn try_http_rule<F>(
    matches: Option<Vec<gateway::HTTPRouteRulesMatches>>,
    filters: Option<Vec<F>>,
    try_filter: impl Fn(F) -> Result<Filter>,
) -> Result<InboundRouteRule<HttpRouteMatch>> {
    let matches = matches
        .into_iter()
        .flatten()
        .map(try_match)
        .collect::<Result<_>>()?;

    let filters = filters
        .into_iter()
        .flatten()
        .map(try_filter)
        .collect::<Result<_>>()?;

    Ok(InboundRouteRule { matches, filters })
}

fn try_gateway_filter(filter: gateway::HTTPRouteRulesFilters) -> Result<Filter> {
    if let Some(request_header_modifier) = filter.request_header_modifier {
        let filter = crate::routes::http::request_header_modifier(request_header_modifier)?;
        return Ok(Filter::RequestHeaderModifier(filter));
    }
    if let Some(response_header_modifier) = filter.response_header_modifier {
        let filter = crate::routes::http::response_header_modifier(response_header_modifier)?;
        return Ok(Filter::ResponseHeaderModifier(filter));
    }
    if let Some(request_redirect) = filter.request_redirect {
        let filter = crate::routes::http::req_redirect(request_redirect)?;
        return Ok(Filter::RequestRedirect(filter));
    }
    if let Some(_request_mirror) = filter.request_mirror {
        bail!("RequestMirror filter is not supported")
    }
    if let Some(_url_rewrite) = filter.url_rewrite {
        bail!("URLRewrite filter is not supported")
    }
    if let Some(_extension_ref) = filter.extension_ref {
        bail!("ExtensionRef filter is not supported")
    }
    bail!("No filter specified");
}

fn try_policy_filter(filter: policy::httproute::HttpRouteFilter) -> Result<Filter> {
    let filter = match filter {
        policy::httproute::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = crate::routes::http::request_header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        policy::httproute::HttpRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = crate::routes::http::response_header_modifier(response_header_modifier)?;
            Filter::ResponseHeaderModifier(filter)
        }

        policy::httproute::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = crate::routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
    };
    Ok(filter)
}
