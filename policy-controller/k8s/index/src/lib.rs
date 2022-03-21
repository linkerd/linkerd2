//! Linkerd Policy Controller
//!
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
//! Lookups against this index are are initiated for a single pod & port. The pod-port's state is
//! modeled as a nested watch -- the outer watch is updated as a `Server` selects/deselects a
//! pod-port; and the inner watch is updated as a `Server`'s authorizations are updated.
//!
//! The Pod, Server, and ServerAuthorization indices are all scoped within a namespace index, as
//! these resources cannot reference resources in other namespaces. This scoping helps to narrow the
//! search space when processing updates and linking resources.

#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

mod defaults;
mod lookup;
mod namespace;
pub mod pod;
pub mod server;
pub mod server_authorization;

#[cfg(test)]
mod tests;

pub use self::{defaults::DefaultPolicy, lookup::Reader};
use self::{
    defaults::DefaultPolicyWatches,
    namespace::{Namespace, NamespaceIndex},
    server::SrvIndex,
};
use linkerd_policy_controller_core::{InboundServer, IpNet};
use parking_lot::RwLock;
use std::sync::Arc;
use tokio::{sync::watch, time};

/// Watches a server's configuration for server/authorization changes.
type ServerRx = watch::Receiver<InboundServer>;

/// Publishes updates for a server's configuration for server/authorization changes.
type ServerTx = watch::Sender<InboundServer>;

/// Watches a pod's port for a new `ServerRx`.
type PodServerRx = watch::Receiver<ServerRx>;

/// Publishes a pod's port for a new `ServerRx`.
type PodServerTx = watch::Sender<ServerRx>;

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
}

pub type SharedIndex = Arc<RwLock<Index>>;

/// Holds all indexing state. Owned and updated by a single task that processes watch events,
/// publishing results to the shared lookup map for quick lookups in the API server.
pub struct Index {
    /// Holds per-namespace pod/server/authorization indexes.
    namespaces: NamespaceIndex,

    cluster_info: ClusterInfo,

    /// Holds watches for the cluster's default-allow policies. These watches are never updated but
    /// this state is held so we can used shared references when updating a pod-port's server watch
    /// with a default policy.
    default_policy_watches: DefaultPolicyWatches,

    /// A handle that supports updates to the lookup index.
    lookups: lookup::Writer,
}

// === impl Index ===

impl Index {
    pub fn new(
        cluster_info: ClusterInfo,
        default_policy: DefaultPolicy,
        detect_timeout: time::Duration,
    ) -> (lookup::Reader, SharedIndex) {
        // Create a common set of receivers for all supported default policies.
        let default_policy_watches =
            DefaultPolicyWatches::new(cluster_info.networks.clone(), detect_timeout);

        // Provide the cluster-wide default-allow policy to the namespace index so that it may be
        // used when a workload-level annotation is not set.
        let namespaces = NamespaceIndex::new(default_policy);

        let (writer, reader) = lookup::pair();
        let idx = Self {
            lookups: writer,
            namespaces,
            cluster_info,
            default_policy_watches,
        };
        (reader, Arc::new(RwLock::new(idx)))
    }

    pub fn get_ns(&self, ns: &str) -> Option<&Namespace> {
        self.namespaces.index.get(ns)
    }
}

impl ClusterInfo {
    fn service_account_identity(&self, ns: &str, sa: &str) -> String {
        format!(
            "{}.{}.serviceaccount.identity.{}.{}",
            sa, ns, self.control_plane_ns, self.identity_domain
        )
    }
}
