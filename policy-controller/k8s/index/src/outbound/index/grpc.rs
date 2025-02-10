use std::num::NonZeroU16;
use std::time;

use super::{
    parse_duration, parse_timeouts, ResourceInfo, ResourceKind, ResourcePort, ResourceRef,
};
use crate::{routes, ClusterInfo};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use kube::ResourceExt;
use linkerd_policy_controller_core::outbound::{
    Backend, Filter, GrpcRetryCondition, GrpcRoute, OutboundRoute, RouteRetry, RouteTimeouts,
    WeightedEgressNetwork, WeightedService,
};
use linkerd_policy_controller_core::{outbound::OutboundRouteRule, routes::GrpcRouteMatch};
use linkerd_policy_controller_k8s_api::{
    gateway::grpcroutes as gateway, policy, Resource, Service, Time,
};

pub(super) fn convert_route(
    ns: &str,
    route: gateway::GRPCRoute,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ResourceRef, ResourceInfo>,
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
    rule: gateway::GRPCRouteRules,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ResourceRef, ResourceInfo>,
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
        .map(convert_filter)
        .collect::<Result<_>>()?;

    Ok(OutboundRouteRule {
        matches,
        backends,
        timeouts,
        retry,
        filters,
    })
}

pub(super) fn convert_backend(
    ns: &str,
    backend: gateway::GRPCRouteRulesBackendRefs,
    cluster: &ClusterInfo,
    resources: &HashMap<ResourceRef, ResourceInfo>,
) -> Option<Backend> {
    let backend_kind = match backend_kind(&backend) {
        Some(backend_kind) => backend_kind,
        None => {
            return Some(Backend::Invalid {
                weight: backend.weight.unwrap_or(1) as u32,
                message: format!(
                    "unsupported backend type {group} {kind}",
                    group = backend.group.as_deref().unwrap_or("core"),
                    kind = backend.kind.as_deref().unwrap_or("<empty>"),
                ),
            });
        }
    };

    let filters = backend.filters;

    let backend_ref = ResourceRef {
        name: backend.name.clone(),
        namespace: backend.namespace.unwrap_or_else(|| ns.to_string()),
        kind: backend_kind.clone(),
    };

    let name = backend.name;
    let weight = backend.weight.unwrap_or(1) as u32;

    let filters = match filters
        .into_iter()
        .flatten()
        .map(convert_backend_filter)
        .collect::<Result<_>>()
    {
        Ok(filters) => filters,
        Err(error) => {
            return Some(Backend::Invalid {
                weight: backend.weight.unwrap_or(1) as u32,
                message: format!("unsupported backend filter: {error}"),
            });
        }
    };

    let port = backend
        .port
        .and_then(|p| p.try_into().ok())
        .and_then(|p: u16| NonZeroU16::try_from(p).ok());

    match backend_kind {
        ResourceKind::Service => {
            // The gateway API dictates:
            //
            // Port is required when the referent is a Kubernetes Service.
            let port = match port {
                Some(port) => port,
                None => {
                    return Some(Backend::Invalid {
                        weight,
                        message: format!("missing port for backend Service {name}"),
                    })
                }
            };

            Some(Backend::Service(WeightedService {
                weight: weight as u32,
                authority: cluster.service_dns_authority(&backend_ref.namespace, &name, port),
                name,
                namespace: backend_ref.namespace.to_string(),
                port,
                filters,
                exists: resources.contains_key(&backend_ref),
            }))
        }
        ResourceKind::EgressNetwork => Some(Backend::EgressNetwork(WeightedEgressNetwork {
            weight: weight.into(),
            name,
            namespace: backend_ref.namespace.to_string(),
            port,
            filters,
            exists: resources.contains_key(&backend_ref),
        })),
    }
}

pub(crate) fn convert_filter(filter: gateway::GRPCRouteRulesFilters) -> Result<Filter> {
    if let Some(request_header_modifier) = filter.request_header_modifier {
        let filter = routes::grpc::request_header_modifier(request_header_modifier)?;
        return Ok(Filter::RequestHeaderModifier(filter));
    }
    if let Some(response_header_modifier) = filter.response_header_modifier {
        let filter = routes::grpc::response_header_modifier(response_header_modifier)?;
        return Ok(Filter::ResponseHeaderModifier(filter));
    }
    if let Some(_request_mirror) = filter.request_mirror {
        bail!("RequestMirror filter is not supported")
    }
    if let Some(_extension_ref) = filter.extension_ref {
        bail!("ExtensionRef filter is not supported")
    }
    bail!("unknown filter")
}

pub(crate) fn convert_backend_filter(
    filter: gateway::GRPCRouteRulesBackendRefsFilters,
) -> Result<Filter> {
    if let Some(request_header_modifier) = filter.request_header_modifier {
        let filter = routes::grpc::backend_request_header_modifier(request_header_modifier)?;
        return Ok(Filter::RequestHeaderModifier(filter));
    }
    if let Some(response_header_modifier) = filter.response_header_modifier {
        let filter = routes::grpc::backend_response_header_modifier(response_header_modifier)?;
        return Ok(Filter::ResponseHeaderModifier(filter));
    }
    if let Some(_request_mirror) = filter.request_mirror {
        bail!("RequestMirror filter is not supported")
    }
    if let Some(_extension_ref) = filter.extension_ref {
        bail!("ExtensionRef filter is not supported")
    }
    bail!("unknown filter")
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

pub(super) fn route_accepted_by_resource_port(
    route_status: Option<&gateway::GRPCRouteStatus>,
    resource_port: &ResourcePort,
) -> bool {
    let (kind, group) = match resource_port.kind {
        ResourceKind::Service => (Service::kind(&()), Service::group(&())),
        ResourceKind::EgressNetwork => (
            policy::EgressNetwork::kind(&()),
            policy::EgressNetwork::group(&()),
        ),
    };
    let mut group = &*group;
    if group.is_empty() {
        group = "core";
    }
    route_status
        .map(|status| status.parents.as_slice())
        .unwrap_or_default()
        .iter()
        .any(|parent_status| {
            let port_matches = match parent_status.parent_ref.port {
                Some(port) => port == resource_port.port.get() as i32,
                None => true,
            };
            let mut parent_group = parent_status.parent_ref.group.as_deref().unwrap_or("core");
            if parent_group.is_empty() {
                parent_group = "core";
            }
            resource_port.name == parent_status.parent_ref.name
                && Some(kind.as_ref()) == parent_status.parent_ref.kind.as_deref()
                && group == parent_group
                && port_matches
                && parent_status
                    .conditions
                    .iter()
                    .flatten()
                    .any(|condition| condition.type_ == "Accepted" && condition.status == "True")
        })
}

pub fn route_accepted_by_service(
    route_status: Option<&gateway::GRPCRouteStatus>,
    service: &str,
) -> bool {
    let mut service_group = &*Service::group(&());
    if service_group.is_empty() {
        service_group = "core";
    }
    route_status
        .map(|status| status.parents.as_slice())
        .unwrap_or_default()
        .iter()
        .any(|parent_status| {
            let mut parent_group = parent_status.parent_ref.group.as_deref().unwrap_or("core");
            if parent_group.is_empty() {
                parent_group = "core";
            }
            parent_status.parent_ref.name == service
                && parent_status.parent_ref.kind.as_deref() == Some(Service::kind(&()).as_ref())
                && parent_group == service_group
                && parent_status
                    .conditions
                    .iter()
                    .flatten()
                    .any(|condition| condition.type_ == "Accepted" && condition.status == "True")
        })
}

pub(crate) fn backend_kind(backend: &gateway::GRPCRouteRulesBackendRefs) -> Option<ResourceKind> {
    let group = backend.group.as_deref();
    // Backends default to `Service` if no kind is specified.
    let kind = backend.kind.as_deref().unwrap_or("Service");
    if super::is_service(group, kind) {
        Some(ResourceKind::Service)
    } else if super::is_egress_network(group, kind) {
        Some(ResourceKind::EgressNetwork)
    } else {
        None
    }
}
