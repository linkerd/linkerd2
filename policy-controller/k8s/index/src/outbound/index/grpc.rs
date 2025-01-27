use std::time;

use super::http::{convert_backend, convert_gateway_filter};
use super::{parse_duration, parse_timeouts, ParentRef, ParentState};
use crate::{routes, ClusterInfo};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use kube::ResourceExt;
use linkerd_policy_controller_core::outbound::{
    GrpcRetryCondition, GrpcRoute, OutboundRoute, RouteRetry, RouteTimeouts,
};
use linkerd_policy_controller_core::{outbound::OutboundRouteRule, routes::GrpcRouteMatch};
use linkerd_policy_controller_k8s_api::{gateway, Time};

pub(super) fn convert_route(
    ns: &str,
    route: gateway::GrpcRoute,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ParentRef, ParentState>,
) -> Result<GrpcRoute> {
    let timeouts = parse_timeouts(route.annotations())?;
    let retry = parse_grpc_retry(route.annotations())?;

    let hostnames = route
        .spec
        .hostnames
        .into_iter()
        .flatten()
        .map(routes::host_match)
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
                resource_info,
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
    resource_info: &HashMap<ParentRef, ParentState>,
    timeouts: RouteTimeouts,
    retry: Option<RouteRetry<GrpcRetryCondition>>,
) -> Result<OutboundRouteRule<GrpcRouteMatch, GrpcRetryCondition>> {
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
        .filter_map(|b| convert_backend(ns, b, cluster, resource_info))
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

pub fn parse_grpc_retry(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<RouteRetry<GrpcRetryCondition>>> {
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

    let conditions = annotations
        .get("retry.linkerd.io/grpc")
        .map(|v| {
            v.split(',')
                .map(|cond| {
                    if cond.eq_ignore_ascii_case("cancelled") {
                        return Ok(GrpcRetryCondition::Cancelled);
                    }
                    if cond.eq_ignore_ascii_case("deadline-exceeded") {
                        return Ok(GrpcRetryCondition::DeadlineExceeded);
                    }
                    if cond.eq_ignore_ascii_case("internal") {
                        return Ok(GrpcRetryCondition::Internal);
                    }
                    if cond.eq_ignore_ascii_case("resource-exhausted") {
                        return Ok(GrpcRetryCondition::ResourceExhausted);
                    }
                    if cond.eq_ignore_ascii_case("unavailable") {
                        return Ok(GrpcRetryCondition::Unavailable);
                    }
                    bail!("Unknown grpc retry condition: {cond}");
                })
                .collect::<Result<Vec<_>>>()
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
