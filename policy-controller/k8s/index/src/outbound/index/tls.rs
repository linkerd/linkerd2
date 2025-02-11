use std::num::NonZeroU16;

use super::{ResourceInfo, ResourceKind, ResourcePort, ResourceRef};
use crate::{routes, ClusterInfo};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use linkerd_policy_controller_core::outbound::{
    Backend, TcpRouteRule, TlsRoute, WeightedEgressNetwork, WeightedService,
};
use linkerd_policy_controller_k8s_api::{
    gateway::tlsroutes as gateway, policy, Resource, Service, Time,
};

pub(super) fn convert_route(
    ns: &str,
    route: gateway::TLSRoute,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ResourceRef, ResourceInfo>,
) -> Result<TlsRoute> {
    if route.spec.rules.len() != 1 {
        bail!("TLSRoute needs to have one rule");
    }

    let rule = route.spec.rules.first().expect("already checked");

    let hostnames = route
        .spec
        .hostnames
        .into_iter()
        .flatten()
        .map(routes::host_match)
        .collect();

    let backends = rule
        .backend_refs
        .clone()
        .into_iter()
        .flatten()
        .filter_map(|b| convert_backend(ns, b, cluster, resource_info))
        .collect();

    let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

    Ok(TlsRoute {
        hostnames,
        rule: TcpRouteRule { backends },
        creation_timestamp,
    })
}

pub(super) fn convert_backend(
    ns: &str,
    backend: gateway::TLSRouteRulesBackendRefs,
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

    let backend_ref = ResourceRef {
        name: backend.name.clone(),
        namespace: backend.namespace.unwrap_or_else(|| ns.to_string()),
        kind: backend_kind.clone(),
    };

    let name = backend.name;
    let weight = backend.weight.unwrap_or(1) as u32;

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
                weight,
                authority: cluster.service_dns_authority(&backend_ref.namespace, &name, port),
                name,
                namespace: backend_ref.namespace.to_string(),
                port,
                filters: vec![],
                exists: resources.contains_key(&backend_ref),
            }))
        }
        ResourceKind::EgressNetwork => Some(Backend::EgressNetwork(WeightedEgressNetwork {
            weight,
            name,
            namespace: backend_ref.namespace.to_string(),
            port,
            filters: vec![],
            exists: resources.contains_key(&backend_ref),
        })),
    }
}

pub(super) fn route_accepted_by_resource_port(
    route_status: Option<&gateway::TLSRouteStatus>,
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
    route_status: Option<&gateway::TLSRouteStatus>,
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

pub(crate) fn backend_kind(backend: &gateway::TLSRouteRulesBackendRefs) -> Option<ResourceKind> {
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
