use super::http::{convert_backend, convert_gateway_filter};
use super::{ServiceInfo, ServiceRef};
use crate::{routes, ClusterInfo};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use linkerd_policy_controller_core::outbound::OutboundRoute;
use linkerd_policy_controller_core::{outbound::OutboundRouteRule, routes::GrpcRouteMatch};
use linkerd_policy_controller_k8s_api::{gateway, Time};

pub(crate) fn convert_route(
    ns: &str,
    route: gateway::GrpcRoute,
    cluster: &ClusterInfo,
    service_info: &HashMap<ServiceRef, ServiceInfo>,
) -> Result<OutboundRoute<GrpcRouteMatch>> {
    let hostnames = route
        .spec
        .hostnames
        .into_iter()
        .flatten()
        .map(routes::http::host_match)
        .collect();

    let rules = route
        .spec
        .rules
        .into_iter()
        .flatten()
        .map(|rule| convert_rule(ns, rule, cluster, service_info))
        .collect::<Result<_>>()?;

    let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

    Ok(OutboundRoute {
        hostnames,
        rules,
        creation_timestamp,
    })
}

fn convert_rule(
    ns: &str,
    rule: gateway::GrpcRouteRule,
    cluster: &ClusterInfo,
    service_info: &HashMap<ServiceRef, ServiceInfo>,
) -> Result<OutboundRouteRule<GrpcRouteMatch>> {
    let matches = rule
        .matches
        .into_iter()
        .flatten()
        .map(routes::grpc::try_match)
        .collect::<Result<_>>()?;

    let backends = rule
        .backend_refs
        .into_iter()
        .flatten()
        .filter_map(|b| convert_backend(ns, b, cluster, service_info))
        .collect();

    let filters = rule
        .filters
        .into_iter()
        .flatten()
        .map(convert_gateway_filter)
        .collect::<Result<_>>()?;

    Ok(OutboundRouteRule {
        matches,
        backends,
        request_timeout: None,
        backend_request_timeout: None,
        filters,
    })
}
