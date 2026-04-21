use crate::inbound::routes::{ParentRef, RouteBinding, Status};
use crate::routes::grpc::try_match;
use ahash::AHashMap as HashMap;
use anyhow::{bail, Error, Result};
use linkerd_policy_controller_core::{
    inbound::{Filter, GrpcRoute, InboundRoute, InboundRouteRule},
    routes::GrpcRouteMatch,
};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway};

impl TryFrom<gateway::GRPCRoute> for RouteBinding<GrpcRoute> {
    type Error = Error;

    fn try_from(route: gateway::GRPCRoute) -> Result<Self, Self::Error> {
        let route_ns = route.metadata.namespace.as_deref();
        let creation_timestamp = route.metadata.creation_timestamp.map(|k8s::Time(t)| t);
        let parents = ParentRef::collect_from_grpc(route_ns, route.spec.parent_refs)?;
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
                |gateway::GRPCRouteRules {
                     matches, filters, ..
                 }| { try_grpc_rule(matches, filters, try_grpc_filter) },
            )
            .collect::<Result<_>>()?;

        let statuses = route
            .status
            .map_or_else(Vec::new, Status::collect_from_grpc);

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

fn try_grpc_rule<F>(
    matches: Option<Vec<gateway::GRPCRouteRulesMatches>>,
    filters: Option<Vec<F>>,
    try_filter: impl Fn(F) -> Result<Filter>,
) -> Result<InboundRouteRule<GrpcRouteMatch>> {
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

fn try_grpc_filter(filter: gateway::GRPCRouteRulesFilters) -> Result<Filter> {
    if let Some(request_header_modifier) = filter.request_header_modifier {
        let filter = crate::routes::grpc::request_header_modifier(request_header_modifier)?;
        return Ok(Filter::RequestHeaderModifier(filter));
    }

    if let Some(response_header_modifier) = filter.response_header_modifier {
        let filter = crate::routes::grpc::response_header_modifier(response_header_modifier)?;
        return Ok(Filter::ResponseHeaderModifier(filter));
    }

    if let Some(_request_mirror) = filter.request_mirror {
        bail!("RequestMirror filter is not supported")
    }

    if let Some(_extension_ref) = filter.extension_ref {
        bail!("ExtensionRef filter is not supported")
    }

    bail!("No filter specified");
}
