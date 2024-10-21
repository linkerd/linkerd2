use crate::inbound::routes::{ParentRef, RouteBinding, Status};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Error, Result};
use linkerd_policy_controller_core::{
    inbound::{Filter, GrpcRoute, InboundRoute, InboundRouteRule},
    routes::{GrpcMethodMatch, GrpcRouteMatch},
};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway};

impl TryFrom<gateway::GrpcRoute> for RouteBinding<GrpcRoute> {
    type Error = Error;

    fn try_from(route: gateway::GrpcRoute) -> Result<Self, Self::Error> {
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
                |gateway::GrpcRouteRule {
                     matches, filters, ..
                 }| { try_grpc_rule(matches, filters, try_grpc_filter) },
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

pub fn try_grpc_match(
    gateway::GrpcRouteMatch { headers, method }: gateway::GrpcRouteMatch,
) -> Result<GrpcRouteMatch> {
    let headers = headers
        .into_iter()
        .flatten()
        .map(crate::routes::http::header_match)
        .collect::<Result<_>>()?;

    let method = match method {
        Some(gateway::GrpcMethodMatch::Exact { method, service }) => {
            Some(GrpcMethodMatch { method, service })
        }
        Some(gateway::GrpcMethodMatch::RegularExpression { .. }) => {
            bail!("Regular expression gRPC method match is not supported")
        }
        None => None,
    };

    Ok(GrpcRouteMatch { headers, method })
}

fn try_grpc_rule<F>(
    matches: Option<Vec<gateway::GrpcRouteMatch>>,
    filters: Option<Vec<F>>,
    try_filter: impl Fn(F) -> Result<Filter>,
) -> Result<InboundRouteRule<GrpcRouteMatch>> {
    let matches = matches
        .into_iter()
        .flatten()
        .map(try_grpc_match)
        .collect::<Result<_>>()?;

    let filters = filters
        .into_iter()
        .flatten()
        .map(try_filter)
        .collect::<Result<_>>()?;

    Ok(InboundRouteRule { matches, filters })
}

fn try_grpc_filter(filter: gateway::GrpcRouteFilter) -> Result<Filter> {
    let filter = match filter {
        gateway::GrpcRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = crate::routes::http::header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        gateway::GrpcRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = crate::routes::http::header_modifier(response_header_modifier)?;
            Filter::ResponseHeaderModifier(filter)
        }
        gateway::GrpcRouteFilter::RequestMirror { .. } => {
            bail!("RequestMirror filter is not supported")
        }
        gateway::GrpcRouteFilter::ExtensionRef { .. } => {
            bail!("ExtensionRef filter is not supported")
        }
    };
    Ok(filter)
}
