use std::{num::NonZeroU16, time};

use super::{
    parse_duration, parse_timeouts, ResourceInfo, ResourceKind, ResourcePort, ResourceRef,
};
use crate::{
    routes::{self, HttpRouteResource},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use kube::ResourceExt;
use linkerd_policy_controller_core::{
    outbound::{
        Backend, Filter, HttpRetryCondition, OutboundRoute, OutboundRouteRule, RouteRetry,
        RouteTimeouts, WeightedEgressNetwork, WeightedService,
    },
    routes::HttpRouteMatch,
};
use linkerd_policy_controller_k8s_api::{gateway, policy, Resource, Service, Time};

pub(super) fn convert_route(
    ns: &str,
    route: HttpRouteResource,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ResourceRef, ResourceInfo>,
) -> Result<OutboundRoute<HttpRouteMatch, HttpRetryCondition>> {
    match route {
        HttpRouteResource::LinkerdHttp(route) => {
            let timeouts = parse_timeouts(route.annotations())?;
            let retry = parse_http_retry(route.annotations())?;

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
                .map(|r| {
                    convert_linkerd_rule(
                        ns,
                        r,
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
        HttpRouteResource::GatewayHttp(route) => {
            let timeouts = parse_timeouts(route.annotations())?;
            let retry = parse_http_retry(route.annotations())?;

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
                .map(|r| {
                    convert_gateway_rule(
                        ns,
                        r,
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
    }
}

fn convert_linkerd_rule(
    ns: &str,
    rule: policy::httproute::HttpRouteRule,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ResourceRef, ResourceInfo>,
    mut timeouts: RouteTimeouts,
    retry: Option<RouteRetry<HttpRetryCondition>>,
) -> Result<OutboundRouteRule<HttpRouteMatch, HttpRetryCondition>> {
    let matches = rule
        .matches
        .into_iter()
        .flatten()
        .map(routes::http::try_match)
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
        .map(convert_linkerd_filter)
        .collect::<Result<_>>()?;

    timeouts.request = timeouts.request.or_else(|| {
        rule.timeouts.as_ref().and_then(|timeouts| {
            let timeout = time::Duration::from(timeouts.request?);

            // zero means "no timeout", per GEP-1742
            if timeout == time::Duration::ZERO {
                return None;
            }

            Some(timeout)
        })
    });

    Ok(OutboundRouteRule {
        matches,
        backends,
        timeouts,
        retry,
        filters,
    })
}

fn convert_gateway_rule(
    ns: &str,
    rule: gateway::HTTPRouteRules,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ResourceRef, ResourceInfo>,
    timeouts: RouteTimeouts,
    retry: Option<RouteRetry<HttpRetryCondition>>,
) -> Result<OutboundRouteRule<HttpRouteMatch, HttpRetryCondition>> {
    let matches = rule
        .matches
        .into_iter()
        .flatten()
        .map(routes::http::try_match)
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

pub(super) fn convert_backend(
    ns: &str,
    backend: gateway::HTTPRouteRulesBackendRefs,
    cluster: &ClusterInfo,
    resources: &HashMap<ResourceRef, ResourceInfo>,
) -> Option<Backend> {
    let backend_kind = match backend_kind(&backend) {
        Some(backend_kind) => backend_kind,
        None => {
            return Some(Backend::Invalid {
                weight: backend
                    .backend_ref
                    .as_ref()
                    .and_then(|br| br.weight)
                    .unwrap_or(1) as u32,
                message: format!(
                    "unsupported backend type {group} {kind}",
                    group = backend
                        .backend_ref
                        .as_ref()
                        .and_then(|br| br.inner.group.as_deref())
                        .unwrap_or("core"),
                    kind = backend
                        .backend_ref
                        .as_ref()
                        .and_then(|br| br.inner.kind.as_deref())
                        .unwrap_or("<empty>"),
                ),
            });
        }
    };

    let filters = backend.filters;

    let backend_ref = ResourceRef {
        name: backend
            .backend_ref
            .as_ref()
            .map(|br| br.inner.name.clone())
            .unwrap_or_default(),
        namespace: backend
            .backend_ref
            .as_ref()
            .and_then(|br| br.inner.namespace.clone())
            .unwrap_or_else(|| ns.to_string()),
        kind: backend_kind.clone(),
    };

    let name = backend_ref.name.clone();
    let weight = backend
        .backend_ref
        .as_ref()
        .and_then(|br| br.weight)
        .unwrap_or(1) as u32;

    let filters = match filters
        .into_iter()
        .flatten()
        .map(convert_gateway_backend_filter)
        .collect::<Result<_>>()
    {
        Ok(filters) => filters,
        Err(error) => {
            return Some(Backend::Invalid {
                weight,
                message: format!("unsupported backend filter: {error}"),
            });
        }
    };

    let port = backend
        .backend_ref
        .and_then(|br| br.inner.port)
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
                weight,
                authority: cluster.service_dns_authority(&backend_ref.namespace, &name, port),
                name,
                namespace: backend_ref.namespace.to_string(),
                port,
                filters,
                exists: resources.contains_key(&backend_ref),
            }))
        }
        ResourceKind::EgressNetwork => Some(Backend::EgressNetwork(WeightedEgressNetwork {
            weight,
            name,
            namespace: backend_ref.namespace.to_string(),
            port,
            filters,
            exists: resources.contains_key(&backend_ref),
        })),
    }
}

fn convert_linkerd_filter(filter: policy::httproute::HttpRouteFilter) -> Result<Filter> {
    let filter = match filter {
        policy::httproute::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = routes::http::request_header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        policy::httproute::HttpRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = routes::http::response_header_modifier(response_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        policy::httproute::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
    };
    Ok(filter)
}

pub(crate) fn convert_gateway_filter(filter: gateway::HTTPRouteRulesFilters) -> Result<Filter> {
    let filter = match filter {
        gateway::HTTPRouteRulesFilters::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = routes::http::request_header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        gateway::HTTPRouteRulesFilters::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = routes::http::response_header_modifier(response_header_modifier)?;
            Filter::ResponseHeaderModifier(filter)
        }

        gateway::HTTPRouteRulesFilters::RequestRedirect { request_redirect } => {
            let filter = routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
        gateway::HTTPRouteRulesFilters::RequestMirror { .. } => {
            bail!("RequestMirror filter is not supported")
        }
        gateway::HTTPRouteRulesFilters::URLRewrite { .. } => {
            bail!("URLRewrite filter is not supported")
        }
        gateway::HTTPRouteRulesFilters::ExtensionRef { .. } => {
            bail!("ExtensionRef filter is not supported")
        }
    };
    Ok(filter)
}

pub(crate) fn convert_gateway_backend_filter(
    filter: gateway::HTTPRouteRulesBackendRefsFilters,
) -> Result<Filter> {
    let filter = match filter {
        gateway::HTTPRouteRulesBackendRefsFilters::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = routes::http::backend_request_header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        gateway::HTTPRouteRulesBackendRefsFilters::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = routes::http::backend_response_header_modifier(response_header_modifier)?;
            Filter::ResponseHeaderModifier(filter)
        }

        gateway::HTTPRouteRulesBackendRefsFilters::RequestRedirect { request_redirect } => {
            let filter = routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
        gateway::HTTPRouteRulesBackendRefsFilters::RequestMirror { .. } => {
            bail!("RequestMirror filter is not supported")
        }
        gateway::HTTPRouteRulesBackendRefsFilters::URLRewrite { .. } => {
            bail!("URLRewrite filter is not supported")
        }
        gateway::HTTPRouteRulesBackendRefsFilters::ExtensionRef { .. } => {
            bail!("ExtensionRef filter is not supported")
        }
    };
    Ok(filter)
}

pub fn parse_http_retry(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<RouteRetry<HttpRetryCondition>>> {
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

    fn to_code(s: &str) -> Option<u32> {
        let code = s.parse::<u32>().ok()?;
        if (100..600).contains(&code) {
            Some(code)
        } else {
            None
        }
    }

    let conditions = annotations
        .get("retry.linkerd.io/http")
        .map(|v| {
            v.split(',')
                .map(|cond| {
                    if cond.eq_ignore_ascii_case("5xx") {
                        return Ok(HttpRetryCondition {
                            status_min: 500,
                            status_max: 599,
                        });
                    }
                    if cond.eq_ignore_ascii_case("gateway-error") {
                        return Ok(HttpRetryCondition {
                            status_min: 502,
                            status_max: 504,
                        });
                    }

                    if let Some(code) = to_code(cond) {
                        return Ok(HttpRetryCondition {
                            status_min: code,
                            status_max: code,
                        });
                    }
                    if let Some((start, end)) = cond.split_once('-') {
                        if let (Some(s), Some(e)) = (to_code(start), to_code(end)) {
                            if s <= e {
                                return Ok(HttpRetryCondition {
                                    status_min: s,
                                    status_max: e,
                                });
                            }
                        }
                    }

                    bail!("invalid retry condition: {v}");
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
    route_status: Option<&gateway::HTTPRouteStatus>,
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
        .map(|status| status.inner.parents.as_slice())
        .unwrap_or_default()
        .iter()
        .any(|parent_status| {
            let port_matches = match parent_status.parent_ref.port {
                Some(port) => port == resource_port.port.get(),
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
                    .any(|condition| condition.type_ == "Accepted" && condition.status == "True")
        })
}

pub fn route_accepted_by_service(
    route_status: Option<&gateway::HTTPRouteStatus>,
    service: &str,
) -> bool {
    let mut service_group = &*Service::group(&());
    if service_group.is_empty() {
        service_group = "core";
    }
    route_status
        .map(|status| status.inner.parents.as_slice())
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
                    .any(|condition| condition.type_ == "Accepted" && condition.status == "True")
        })
}

pub(crate) fn backend_kind(backend: &gateway::HTTPRouteRulesBackendRefs) -> Option<ResourceKind> {
    let group = backend
        .backend_ref
        .as_ref()
        .and_then(|br| br.inner.group.as_deref());
    // Backends default to `Service` if no kind is specified.
    let kind = backend
        .backend_ref
        .as_ref()
        .and_then(|br| br.inner.kind.as_deref())
        .unwrap_or("Service");
    if super::is_service(group, kind) {
        Some(ResourceKind::Service)
    } else if super::is_egress_network(group, kind) {
        Some(ResourceKind::EgressNetwork)
    } else {
        None
    }
}
