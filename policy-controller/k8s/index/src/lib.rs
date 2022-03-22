//! The policy controller serves discovery requests from inbound proxies, indicating how the proxy
//! should admit connections into a Pod. It watches the following cluster resources:
//!
//! - A `Namespace` may be annotated with a default-allow policy that applies to all pods in the
//!   namespace (unless they are annotated with a default policy).
//! - Each `Pod` enumerate its ports. We maintain an index of each pod's ports, linked to `Server`
//!   objects.
//! - Each `Server` selects over pods in the same namespace.
//! - Each `ServerAuthorization` selects over `Server` instances in the same namespace.  When a
//!   `ServerAuthorization` is updated, we find all of the `Server` instances it selects and update
//!   their authorizations and publishes these updates on the server's broadcast channel.
//!
//! ```text
//! [ Pod ] -> [ Port ] <- [ Server ] <- [ ServerAuthorization ]
//! ```
//!
//! Lookups against this index are are initiated for a single pod & port.
//!
//! The Pod, Server, and ServerAuthorization indices are all scoped within a namespace index, as
//! these resources cannot reference resources in other namespaces. This scoping helps to narrow the
//! search space when processing updates and linking resources.

#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

mod defaults;
mod index;
mod pod;
mod server;
mod server_authorization;

#[cfg(test)]
mod tests;

use linkerd_policy_controller_core::IpNet;
use std::time;

pub use self::{
    defaults::DefaultPolicy,
    index::{Index, SharedIndex},
};

/// Holds cluster metadata.
#[derive(Clone, Debug)]
pub struct ClusterInfo {
    /// Networks including PodIPs in this cluster.
    ///
    /// Unfortunately, there's no way to discover this at runtime.
    pub networks: Vec<IpNet>,

    /// The namespace where the linkerd control plane is deployed
    pub control_plane_ns: String,

    /// The cluster's mesh identity trust domain.
    pub identity_domain: String,

    /// The cluster-wide default policy.
    pub default_policy: DefaultPolicy,

    /// The cluster-wide default protocol detection timeout.
    pub default_detect_timeout: time::Duration,
}

impl ClusterInfo {
    fn service_account_identity(&self, ns: &str, sa: &str) -> String {
        format!(
            "{}.{}.serviceaccount.identity.{}.{}",
            sa, ns, self.control_plane_ns, self.identity_domain
        )
    }
}
