//! Linkerd Policy Controller
//!
//! The policy controller serves discovery requests from inbound proxies, indicating how the proxy
//! should admit connections into a Pod. It watches the following cluster resources:
//!
//! - A `Node`'s `podCIDRs` field can be used to determine the IP of the node's Kubelet instance.
//!   Traffic is always permitted to a pod from its Kubelet's IP.
//! - A `Namespace` may be annotated with a default-allow policy that applies to all pods in the
//!   namespace (unless they are annotated with a default policy).
//! - Each `Pod` enumerate its ports. We maintain an index of each pod's ports, linked to `Server`
//!   objects.
//! - Each `Server` selects over pods in the same namespace.
//! - Each `ServerAuthorization` selects over `Server` instances in the same namespace.  When a
//!   `ServerAuthorization` is updated, we find all of the `Server` instances it selects and update
//!   their authorizations and publishes these updates on the server's broadcast channel.
//!
//! ```ignore
//! [Node] <- [ Pod ]
//!           |-> [ Port ] <- [ Server ] <- [ ServerAuthorization ]
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

mod authz;
mod default_allow;
mod lookup;
mod namespace;
mod node;
mod pod;
mod server;
#[cfg(test)]
mod tests;

pub use self::{default_allow::DefaultAllow, lookup::Reader};
use self::{
    default_allow::DefaultAllowRxs,
    namespace::{Namespace, NamespaceIndex},
    node::NodeIndex,
    server::SrvIndex,
};
use anyhow::Context;
use linkerd_policy_controller_core::{InboundServer, IpNet};
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};
use std::{future::Future, pin::Pin};
use tokio::{sync::watch, time};
use tracing::{debug, warn};

/// Watches a server's configuration for server/authorization changes.
type ServerRx = watch::Receiver<InboundServer>;

/// Publishes updates for a server's configuration for server/authorization changes.
type ServerTx = watch::Sender<InboundServer>;

/// Watches a pod's port for a new `ServerRx`.
type PodServerRx = watch::Receiver<ServerRx>;

/// Publishes a pod's port for a new `ServerRx`.
type PodServerTx = watch::Sender<ServerRx>;

/// Holds all indexing state. Owned and updated by a single task that processes watch events,
/// publishing results to the shared lookup map for quick lookups in the API server.
pub struct Index {
    /// Holds per-namespace pod/server/authorization indexes.
    namespaces: NamespaceIndex,

    /// Cached Node IPs.
    nodes: NodeIndex,

    /// The cluster's mesh identity trust domain.
    identity_domain: String,

    /// Networks including PodIPs in this cluster.
    ///
    /// TODO(ver) this can be discovered dynamically by the node index, but this would complicate
    /// notifications.
    cluster_networks: Vec<IpNet>,

    /// Holds watches for the cluster's default-allow policies. These watches are never updated but
    /// this state is held so we can used shared references when updating a pod-port's server watch
    /// with a default policy.
    default_allows: DefaultAllowRxs,

    /// A handle that supports updates to the lookup index.
    lookups: lookup::Writer,

    /// Holds the `DefaultAllowRxs` senders so that the receivers never signal an ending. This doesn't
    /// actually need to be polled.
    _default_allows_txs: Pin<Box<dyn Future<Output = ()> + Send + 'static>>,
}

#[derive(Debug)]
struct Errors(Vec<anyhow::Error>);

// === impl Index ===

impl Index {
    pub fn new(
        cluster_networks: Vec<IpNet>,
        identity_domain: String,
        default_allow: DefaultAllow,
        detect_timeout: time::Duration,
    ) -> (lookup::Reader, Self) {
        // Create a common set of receivers for all supported default policies.
        let (default_allows, _default_allows_txs) =
            DefaultAllowRxs::new(cluster_networks.clone(), detect_timeout);

        // Provide the cluster-wide default-allow policy to the namespace index so that it may be
        // used when a workload-level annotation is not set.
        let namespaces = NamespaceIndex::new(default_allow);

        let (writer, reader) = lookup::pair();
        let idx = Self {
            lookups: writer,
            namespaces,
            identity_domain,
            cluster_networks,
            default_allows,
            nodes: NodeIndex::default(),
            _default_allows_txs: Box::pin(_default_allows_txs),
        };
        (reader, idx)
    }

    /// Drives indexing for all resource types.
    ///
    /// This is all driven on a single task, so it's not necessary for any of the indexing logic to
    /// worry about concurrent access for the internal indexing structures.
    ///
    /// All updates are atomically published to the shared `lookups` map after indexing occurs; but
    /// the indexing task is solely responsible for mutating it.
    pub async fn run(
        mut self,
        resources: impl Into<k8s::ResourceWatches>,
        ready_tx: watch::Sender<bool>,
    ) {
        let k8s::ResourceWatches {
            mut nodes_rx,
            mut pods_rx,
            mut servers_rx,
            mut authorizations_rx,
        } = resources.into();

        let mut initialized = false;
        loop {
            let res = tokio::select! {
                // Track the kubelet IPs for all nodes.
                up = nodes_rx.recv() => match up {
                    k8s::Event::Applied(node) => self.apply_node(node).context("applying a node"),
                    k8s::Event::Deleted(node) => self.delete_node(&node.name()).context("deleting a node"),
                    k8s::Event::Restarted(nodes) => self.reset_nodes(nodes).context("resetting nodes"),
                },

                // Track pods against the appropriate server.
                up = pods_rx.recv() => match up {
                    k8s::Event::Applied(pod) => self.apply_pod(pod).context("applying a pod"),
                    k8s::Event::Deleted(pod) => self.delete_pod(pod).context("deleting a pod"),
                    k8s::Event::Restarted(pods) => self.reset_pods(pods).context("resetting pods"),
                },

                // Track servers and link them with pods.
                up = servers_rx.recv() => match up {
                    k8s::Event::Applied(srv) => {
                        self.apply_server(srv);
                        Ok(())
                    }
                    k8s::Event::Deleted(srv) => self.delete_server(srv).context("deleting a server"),
                    k8s::Event::Restarted(srvs) => self.reset_servers(srvs).context("resetting servers"),
                },

                // Track authorizations and update relevant servers.
                up = authorizations_rx.recv() => match up {
                    k8s::Event::Applied(authz) => self.apply_authz(authz).context("applying an authorization"),
                    k8s::Event::Deleted(authz) => {
                        self.delete_authz(authz);
                        Ok(())
                    }
                    k8s::Event::Restarted(authzs) => self.reset_authzs(authzs).context("resetting authorizations"),
                },
            };

            if let Err(error) = res {
                warn!(?error);
            }

            // Notify the readiness watch once all watches have updated.
            if !initialized
                && nodes_rx.is_initialized()
                && pods_rx.is_initialized()
                && servers_rx.is_initialized()
                && authorizations_rx.is_initialized()
            {
                let _ = ready_tx.send(true);
                initialized = true;
                debug!("Ready");
            }
        }
    }
}

// === impl Errors ===

impl std::fmt::Display for Errors {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0[0])?;
        for e in &self.0[1..] {
            write!(f, "; and {}", e)?;
        }
        Ok(())
    }
}

impl std::error::Error for Errors {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        Some(&*self.0[0])
    }
}

impl Errors {
    fn ok_if_empty(errors: Vec<anyhow::Error>) -> anyhow::Result<()> {
        if errors.is_empty() {
            Ok(())
        } else {
            Err(Self(errors).into())
        }
    }
}
