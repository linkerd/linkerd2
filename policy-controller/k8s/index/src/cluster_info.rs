use std::{num::NonZeroU16, sync::Arc};

use crate::{ports::PortSet, DefaultPolicy};
use linkerd_policy_controller_core::IpNet;
use tokio::time;

/// Holds cluster metadata.
#[derive(Clone, Debug)]
pub struct ClusterInfo {
    /// Networks including PodIPs in this cluster.
    ///
    /// Unfortunately, there's no way to discover this at runtime.
    pub networks: Vec<IpNet>,

    /// The namespace where the linkerd control plane is deployed
    pub control_plane_ns: String,

    /// E.g. "cluster.local"
    pub dns_domain: String,

    /// The cluster's mesh identity trust domain.
    pub identity_domain: String,

    /// The cluster-wide default policy.
    pub default_policy: DefaultPolicy,

    /// The cluster-wide default protocol detection timeout.
    pub default_detect_timeout: time::Duration,

    /// The default set of ports to be marked opaque.
    pub default_opaque_ports: PortSet,

    /// The networks that probes are expected to be from.
    pub probe_networks: Vec<IpNet>,

    /// The namespace that is designated for egress configuration
    /// affecting all workloads across the cluster
    pub global_external_network_namespace: Arc<String>,
}

impl ClusterInfo {
    pub(crate) fn service_account_identity(&self, ns: &str, sa: &str) -> String {
        format!(
            "{}.{}.serviceaccount.identity.{}.{}",
            sa, ns, self.control_plane_ns, self.identity_domain
        )
    }

    pub(crate) fn namespace_identity(&self, ns: &str) -> String {
        format!(
            "*.{}.serviceaccount.identity.{}.{}",
            ns, self.control_plane_ns, self.identity_domain
        )
    }

    pub(crate) fn service_dns_authority(&self, ns: &str, svc: &str, port: NonZeroU16) -> String {
        format!("{}.{}.svc.{}:{port}", svc, ns, self.dns_domain)
    }
}
