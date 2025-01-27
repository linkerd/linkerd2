use super::tcp::convert_backend;
use super::{ParentRef, ParentState};
use crate::{routes, ClusterInfo};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use linkerd_policy_controller_core::outbound::{TcpRouteRule, TlsRoute};
use linkerd_policy_controller_k8s_api::{gateway, Time};

pub(super) fn convert_route(
    ns: &str,
    route: gateway::TlsRoute,
    cluster: &ClusterInfo,
    resource_info: &HashMap<ParentRef, ParentState>,
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
        .filter_map(|b| convert_backend(ns, b, cluster, resource_info))
        .collect();

    let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

    Ok(TlsRoute {
        hostnames,
        rule: TcpRouteRule { backends },
        creation_timestamp,
    })
}
