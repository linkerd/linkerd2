use std::{num::NonZeroU16, time};

use super::{
    is_service, is_unmeshed_network, parse_duration, parse_timeouts, ResourceKind, ResourceRef,
    ServiceInfo,
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
        RouteTimeouts, WeightedService,
    },
    routes::HttpRouteMatch,
};
use linkerd_policy_controller_k8s_api::{gateway, policy, Time};

pub(super) fn convert_route(
    ns: &str,
    route: HttpRouteResource,
    cluster: &ClusterInfo,
    service_info: &HashMap<ResourceRef, ServiceInfo>,
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
                .map(routes::http::host_match)
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
        HttpRouteResource::GatewayHttp(route) => {
            let timeouts = parse_timeouts(route.annotations())?;
            let retry = parse_http_retry(route.annotations())?;

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
                .map(|r| {
                    convert_gateway_rule(
                        ns,
                        r,
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
    }
}

fn convert_linkerd_rule(
    ns: &str,
    rule: policy::httproute::HttpRouteRule,
    cluster: &ClusterInfo,
    service_info: &HashMap<ResourceRef, ServiceInfo>,
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
        .filter_map(|b| convert_backend(ns, b, cluster, service_info))
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
    rule: gateway::HttpRouteRule,
    cluster: &ClusterInfo,
    service_info: &HashMap<ResourceRef, ServiceInfo>,
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

pub(super) fn convert_backend<BackendRef: Into<gateway::HttpBackendRef>>(
    ns: &str,
    backend: BackendRef,
    cluster: &ClusterInfo,
    services: &HashMap<ResourceRef, ServiceInfo>,
) -> Option<Backend> {
    let backend = backend.into();
    let filters = backend.filters;
    let backend = backend.backend_ref?;

    if is_backend_service(&backend.inner) {
        let name = backend.inner.name;
        let weight = backend.weight.unwrap_or(1);

        // The gateway API dictates:
        //
        // Port is required when the referent is a Kubernetes Service.
        let port = match backend
            .inner
            .port
            .and_then(|p| NonZeroU16::try_from(p).ok())
        {
            Some(port) => port,
            None => {
                return Some(Backend::Invalid {
                    weight: weight.into(),
                    message: format!("missing port for backend Service {name}"),
                })
            }
        };
        let service_ref = ResourceRef {
            kind: ResourceKind::Service,
            name: name.clone(),
            namespace: backend.inner.namespace.unwrap_or_else(|| ns.to_string()),
        };

        let filters = match filters
            .into_iter()
            .flatten()
            .map(convert_gateway_filter)
            .collect::<Result<_>>()
        {
            Ok(filters) => filters,
            Err(error) => {
                return Some(Backend::Invalid {
                    weight: backend.weight.unwrap_or(1).into(),
                    message: format!("unsupported backend filter: {error}"),
                });
            }
        };

        Some(Backend::Service(WeightedService {
            weight: weight.into(),
            authority: cluster.service_dns_authority(&service_ref.namespace, &name, port),
            name,
            namespace: service_ref.namespace.to_string(),
            port,
            filters,
            exists: services.contains_key(&service_ref),
        }))
    } else if is_backend_unmeshed_network(&backend.inner) {
        Some(Backend::Forward)
    } else {
        Some(Backend::Invalid {
            weight: backend.weight.unwrap_or(1).into(),
            message: format!(
                "unsupported backend type {group} {kind}",
                group = backend.inner.group.as_deref().unwrap_or("core"),
                kind = backend.inner.kind.as_deref().unwrap_or("<empty>"),
            ),
        })
    }
}

fn convert_linkerd_filter(filter: policy::httproute::HttpRouteFilter) -> Result<Filter> {
    let filter = match filter {
        policy::httproute::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = routes::http::header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        policy::httproute::HttpRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = routes::http::header_modifier(response_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        policy::httproute::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
    };
    Ok(filter)
}

pub(crate) fn convert_gateway_filter<RouteFilter: Into<gateway::HttpRouteFilter>>(
    filter: RouteFilter,
) -> Result<Filter> {
    let filter = filter.into();
    let filter = match filter {
        gateway::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = routes::http::header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        gateway::HttpRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = routes::http::header_modifier(response_header_modifier)?;
            Filter::ResponseHeaderModifier(filter)
        }

        gateway::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = routes::http::req_redirect(request_redirect)?;
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

#[inline]
fn is_backend_service(backend: &gateway::BackendObjectReference) -> bool {
    is_service(
        backend.group.as_deref(),
        // Backends default to `Service` if no kind is specified.
        backend.kind.as_deref().unwrap_or("Service"),
    )
}

#[inline]
fn is_backend_unmeshed_network(backend: &gateway::BackendObjectReference) -> bool {
    is_unmeshed_network(
        backend.group.as_deref(),
        // Backends default to `Service` if no kind is specified.
        backend.kind.as_deref().unwrap_or("Service"),
    )
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
