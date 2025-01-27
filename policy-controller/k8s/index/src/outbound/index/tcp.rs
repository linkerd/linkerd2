use std::num::NonZeroU16;

use super::{ParentFlavor, ParentRef, ParentState};
use crate::ClusterInfo;
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use linkerd_policy_controller_core::outbound::{Backend, WeightedEgressNetwork, WeightedService};
use linkerd_policy_controller_core::outbound::{TcpRoute, TcpRouteRule};
use linkerd_policy_controller_k8s_api::{gateway, Time};

pub(super) fn convert_route(
    ns: &str,
    route: gateway::TcpRoute,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ParentRef, ParentState>,
) -> Result<TcpRoute> {
    if route.spec.rules.len() != 1 {
        bail!("TCPRoute needs to have one rule");
    }

    let rule = route.spec.rules.first().expect("already checked");

    let backends = rule
        .backend_refs
        .clone()
        .into_iter()
        .filter_map(|b| convert_backend(ns, b, cluster, resource_info))
        .collect();

    let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

    Ok(TcpRoute {
        rule: TcpRouteRule { backends },
        creation_timestamp,
    })
}

pub(super) fn convert_backend(
    ns: &str,
    backend: gateway::BackendRef,
    cluster: &ClusterInfo,
    resources: &HashMap<ParentRef, ParentState>,
) -> Option<Backend> {
    let backend_kind = match super::backend_kind(&backend.inner) {
        Some(backend_kind) => backend_kind,
        None => {
            return Some(Backend::Invalid {
                weight: backend.weight.unwrap_or(1).into(),
                message: format!(
                    "unsupported backend type {group} {kind}",
                    group = backend.inner.group.as_deref().unwrap_or("core"),
                    kind = backend.inner.kind.as_deref().unwrap_or("<empty>"),
                ),
            });
        }
    };

    let backend_ref = ParentRef {
        name: backend.inner.name.clone(),
        namespace: backend.inner.namespace.unwrap_or_else(|| ns.to_string()),
        kind: backend_kind.clone(),
    };

    let name = backend.inner.name;
    let weight = backend.weight.unwrap_or(1);

    let port = backend
        .inner
        .port
        .and_then(|p| NonZeroU16::try_from(p).ok());

    match backend_kind {
        ParentFlavor::Service => {
            // The gateway API dictates:
            //
            // Port is required when the referent is a Kubernetes Service.
            let port = match port {
                Some(port) => port,
                None => {
                    return Some(Backend::Invalid {
                        weight: weight.into(),
                        message: format!("missing port for backend Service {name}"),
                    })
                }
            };

            Some(Backend::Service(WeightedService {
                weight: weight.into(),
                authority: cluster.service_dns_authority(&backend_ref.namespace, &name, port),
                name,
                namespace: backend_ref.namespace.to_string(),
                port,
                filters: vec![],
                exists: resources.contains_key(&backend_ref),
            }))
        }
        ParentFlavor::EgressNetwork => Some(Backend::EgressNetwork(WeightedEgressNetwork {
            weight: weight.into(),
            name,
            namespace: backend_ref.namespace.to_string(),
            port,
            filters: vec![],
            exists: resources.contains_key(&backend_ref),
        })),
    }
}
