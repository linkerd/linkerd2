use crate::inbound::routes::{ParentRef, RouteBinding, Status};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Error, Result};
use http::Method;
use linkerd_policy_controller_core::{
    inbound::{Filter, HttpRoute, InboundRoute, InboundRouteRule},
    routes::HttpRouteMatch,
};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy};

impl TryFrom<gateway::HttpRoute> for RouteBinding<HttpRoute> {
    type Error = Error;

    fn try_from(route: gateway::HttpRoute) -> Result<Self, Self::Error> {
        let route_ns = route.metadata.namespace.as_deref();
        let creation_timestamp = route.metadata.creation_timestamp.map(|k8s::Time(t)| t);
        let parents = ParentRef::collect_from(route_ns, route.spec.inner.parent_refs)?;
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
                |gateway::HttpRouteRule {
                     matches,
                     filters,
                     backend_refs: _,
                 }| try_http_rule(matches, filters, try_gateway_filter),
            )
            .collect::<Result<_>>()?;

        let statuses = route
            .status
            .map_or_else(Vec::new, |status| Status::collect_from(status.inner));

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
        let parents = ParentRef::collect_from(route_ns, route.spec.inner.parent_refs)?;
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
            .map_or_else(Vec::new, |status| Status::collect_from(status.inner));

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

pub fn try_http_match(
    gateway::HttpRouteMatch {
        path,
        headers,
        query_params,
        method,
    }: gateway::HttpRouteMatch,
) -> Result<HttpRouteMatch> {
    let path = path.map(crate::routes::http::path_match).transpose()?;

    let headers = headers
        .into_iter()
        .flatten()
        .map(crate::routes::http::header_match)
        .collect::<Result<_>>()?;

    let query_params = query_params
        .into_iter()
        .flatten()
        .map(crate::routes::http::query_param_match)
        .collect::<Result<_>>()?;

    let method = method.as_deref().map(Method::try_from).transpose()?;

    Ok(HttpRouteMatch {
        path,
        headers,
        query_params,
        method,
    })
}

fn try_http_rule<F>(
    matches: Option<Vec<gateway::HttpRouteMatch>>,
    filters: Option<Vec<F>>,
    try_filter: impl Fn(F) -> Result<Filter>,
) -> Result<InboundRouteRule<HttpRouteMatch>> {
    let matches = matches
        .into_iter()
        .flatten()
        .map(try_http_match)
        .collect::<Result<_>>()?;

    let filters = filters
        .into_iter()
        .flatten()
        .map(try_filter)
        .collect::<Result<_>>()?;

    Ok(InboundRouteRule { matches, filters })
}

fn try_gateway_filter(filter: gateway::HttpRouteFilter) -> Result<Filter> {
    let filter = match filter {
        gateway::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = crate::routes::http::header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        gateway::HttpRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = crate::routes::http::header_modifier(response_header_modifier)?;
            Filter::ResponseHeaderModifier(filter)
        }

        gateway::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = crate::routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }

        gateway::HttpRouteFilter::RequestMirror { .. } => {
            bail!("RequestMirror filter is not supported")
        }
        gateway::HttpRouteFilter::URLRewrite { .. } => {
            bail!("URLRewrite filter is not supported")
        }
        gateway::HttpRouteFilter::ExtensionRef { .. } => {
            bail!("ExtensionRef filter is not supported")
        }
    };
    Ok(filter)
}

fn try_policy_filter(filter: policy::httproute::HttpRouteFilter) -> Result<Filter> {
    let filter = match filter {
        policy::httproute::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = crate::routes::http::header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        policy::httproute::HttpRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = crate::routes::http::header_modifier(response_header_modifier)?;
            Filter::ResponseHeaderModifier(filter)
        }

        policy::httproute::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = crate::routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
    };
    Ok(filter)
}
