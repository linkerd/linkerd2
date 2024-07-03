use std::time;

use super::http::{convert_backend, convert_gateway_filter};
use super::{parse_duration, parse_timeouts, ServiceInfo, ServiceRef};
use crate::{routes, ClusterInfo};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use kube::ResourceExt;
use linkerd_policy_controller_core::outbound::{
    GrpcRetryConditions, GrpcRoute, OutboundRoute, RouteRetry, RouteTimeouts,
};
use linkerd_policy_controller_core::{outbound::OutboundRouteRule, routes::GrpcRouteMatch};
use linkerd_policy_controller_k8s_api::{gateway, Time};

pub(crate) fn convert_route(
    ns: &str,
    route: gateway::GrpcRoute,
    cluster: &ClusterInfo,
    service_info: &HashMap<ServiceRef, ServiceInfo>,
) -> Result<GrpcRoute> {
    let timeouts = parse_timeouts(route.annotations())?;
    let retry = parse_grpc_retry(route.annotations())?;

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
        .map(|rule| {
            convert_rule(
                ns,
                rule,
                cluster,
                service_info,
                timeouts.clone(),
                retry.clone(),
            )
        })
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
    timeouts: RouteTimeouts,
    retry: Option<RouteRetry<GrpcRetryConditions>>,
) -> Result<OutboundRouteRule<GrpcRouteMatch, GrpcRetryConditions>> {
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
        timeouts,
        retry,
        filters,
    })
}

fn parse_grpc_retry(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<RouteRetry<GrpcRetryConditions>>> {
    let limit = annotations
        .get("retry.linkerd.io/limit")
        .map(|s| s.parse::<u16>())
        .transpose()?
        .filter(|v| *v != 0);

    let timeout = annotations
        .get("retry.linkerd.io/timeout")
        .map(|v| parse_duration(v))
        .transpose()?
        .filter(|v| *v != time::Duration::ZERO);

    // TODO(alex): support condition list
    let conditions = annotations
        .get("retry.linkerd.io/grpc")
        .map(|v| {
            if v == "cancelled" {
                return Ok(GrpcRetryConditions::Cancelled);
            }
            if v == "deadline-exceeded" {
                return Ok(GrpcRetryConditions::DeadlineExceeded);
            }
            bail!("invalid retry condition: {v}")
        })
        .transpose()?;

    if limit.is_none() && timeout.is_none() && conditions.is_none() {
        return Ok(None);
    }

    Ok(Some(RouteRetry {
        limit: limit.unwrap_or(1),
        timeout,
        conditions,
    }))
}
