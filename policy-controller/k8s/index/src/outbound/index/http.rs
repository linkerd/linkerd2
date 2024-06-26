use std::{num::NonZeroU16, time};

use super::{is_service, ServiceInfo, ServiceRef};
use crate::{
    routes::{self, HttpRouteResource},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use linkerd_policy_controller_core::{
    outbound::{Backend, Filter, OutboundRoute, OutboundRouteRule, WeightedService},
    routes::HttpRouteMatch,
};
use linkerd_policy_controller_k8s_api::{gateway, policy, Time};

pub(crate) fn convert_route(
    ns: &str,
    route: HttpRouteResource,
    cluster: &ClusterInfo,
    service_info: &HashMap<ServiceRef, ServiceInfo>,
) -> Result<OutboundRoute<HttpRouteMatch>> {
    match route {
        HttpRouteResource::LinkerdHttp(route) => {
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
                .map(|r| convert_linkerd_rule(ns, r, cluster, service_info))
                .collect::<Result<_>>()?;

            let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

            Ok(OutboundRoute {
                hostnames,
                rules,
                creation_timestamp,
            })
        }
        HttpRouteResource::GatewayHttp(route) => {
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
                .map(|r| convert_gateway_rule(ns, r, cluster, service_info))
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
    service_info: &HashMap<ServiceRef, ServiceInfo>,
) -> Result<OutboundRouteRule<HttpRouteMatch>> {
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

    let request_timeout = rule.timeouts.as_ref().and_then(|timeouts| {
        let timeout = time::Duration::from(timeouts.request?);

        // zero means "no timeout", per GEP-1742
        if timeout == time::Duration::from_nanos(0) {
            return None;
        }

        Some(timeout)
    });

    let backend_request_timeout =
        rule.timeouts
            .as_ref()
            .and_then(|timeouts: &policy::httproute::HttpRouteTimeouts| {
                let timeout = time::Duration::from(timeouts.backend_request?);

                // zero means "no timeout", per GEP-1742
                if timeout == time::Duration::from_nanos(0) {
                    return None;
                }

                Some(timeout)
            });

    Ok(OutboundRouteRule {
        matches,
        backends,
        request_timeout,
        backend_request_timeout,
        filters,
    })
}

fn convert_gateway_rule(
    ns: &str,
    rule: gateway::HttpRouteRule,
    cluster: &ClusterInfo,
    service_info: &HashMap<ServiceRef, ServiceInfo>,
) -> Result<OutboundRouteRule<HttpRouteMatch>> {
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
        request_timeout: None,
        backend_request_timeout: None,
        filters,
    })
}

pub(crate) fn convert_backend<BackendRef: Into<gateway::HttpBackendRef>>(
    ns: &str,
    backend: BackendRef,
    cluster: &ClusterInfo,
    services: &HashMap<ServiceRef, ServiceInfo>,
) -> Option<Backend> {
    let backend = backend.into();
    let filters = backend.filters;
    let backend = backend.backend_ref?;
    if !is_backend_service(&backend.inner) {
        return Some(Backend::Invalid {
            weight: backend.weight.unwrap_or(1).into(),
            message: format!(
                "unsupported backend type {group} {kind}",
                group = backend.inner.group.as_deref().unwrap_or("core"),
                kind = backend.inner.kind.as_deref().unwrap_or("<empty>"),
            ),
        });
    }

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
    let service_ref = ServiceRef {
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
