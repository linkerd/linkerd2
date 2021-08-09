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
    default_allow::DefaultAllows,
    namespace::{Namespace, NamespaceIndex},
    node::NodeIndex,
    server::SrvIndex,
};
use anyhow::{Context, Error};
use linkerd_policy_controller_core::{InboundServer, IpNet};
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};
use std::sync::Arc;
use tokio::{sync::watch, time};
use tracing::{debug, instrument, warn};

/// Watches a server's configuration for server/authorization changes.
type ServerRx = watch::Receiver<InboundServer>;

/// Publishes updates for a server's configuration for server/authorization changes.
type ServerTx = watch::Sender<InboundServer>;

/// Watches a pod port's for a new `ServerRx`.
type PodServerRx = watch::Receiver<ServerRx>;

/// Publishes a pod port's for a new `ServerRx`.
type PodServerTx = watch::Sender<ServerRx>;

/// Constructs an indexing task and a handle that supports by-pod lookups.
pub fn index(
    watches: impl Into<k8s::ResourceWatches>,
    ready: watch::Sender<bool>,
    cluster_networks: Vec<IpNet>,
    identity_domain: String,
    default_mode: DefaultAllow,
    detect_timeout: time::Duration,
) -> (
    lookup::Reader,
    impl std::future::Future<Output = anyhow::Error>,
) {
    let (writer, reader) = lookup::pair();

    // Watches Nodes, Pods, Servers, and Authorizations to update the lookup map
    // with an entry for each linkerd-injected pod.
    let idx = Index::new(
        writer,
        cluster_networks,
        identity_domain,
        default_mode,
        detect_timeout,
    );
    let task = idx.index(watches.into(), ready);

    (reader, task)
}

struct Index {
    /// Holds per-namespace pod/server/authorization indexes.
    namespaces: NamespaceIndex,

    /// Cached Node IPs.
    nodes: NodeIndex,

    identity_domain: String,

    default_allows: DefaultAllows,

    lookups: lookup::Writer,
}

/// Selects servers for an authorization.
#[derive(Clone, Debug, PartialEq, Eq)]
enum ServerSelector {
    Name(String),
    Selector(Arc<k8s::labels::Selector>),
}

#[derive(Debug)]
struct Errors(Vec<anyhow::Error>);

// === impl Index ===

impl Index {
    pub(crate) fn new(
        lookups: lookup::Writer,
        cluster_nets: Vec<IpNet>,
        identity_domain: String,
        default_allow: DefaultAllow,
        detect_timeout: time::Duration,
    ) -> Self {
        // Create a common set of receivers for all supported default policies.
        //
        // XXX We shouldn't spawn in the constructor if we can avoid it. Instead, it seems best if
        // we can avoid having to wire this into the pods at all and lazily bind the default policy
        // at discovery time?
        let default_allows = DefaultAllows::spawn(cluster_nets, detect_timeout);

        // Provide the cluster-wide default-allow policy to the namespace index so that it may be
        // used when a workload-level annotation is not set.
        let namespaces = NamespaceIndex::new(default_allow);

        Self {
            lookups,
            namespaces,
            identity_domain,
            default_allows,
            nodes: NodeIndex::default(),
        }
    }

    /// Drives indexing for all resource types.
    ///
    /// This is all driven on a single task, so it's not necessary for any of the indexing logic to
    /// worry about concurrent access for the internal indexing structures.
    ///
    /// All updates are atomically published to the shared `lookups` map after indexing occurs; but
    /// the indexing task is solely responsible for mutating it.
    #[instrument(skip(self, resources, ready_tx), fields(result))]
    pub(crate) async fn index(
        mut self,
        resources: k8s::ResourceWatches,
        ready_tx: watch::Sender<bool>,
    ) -> Error {
        let k8s::ResourceWatches {
            mut nodes_rx,
            mut pods_rx,
            mut servers_rx,
            mut authorizations_rx,
        } = resources;

        let mut ready = false;
        loop {
            let res = tokio::select! {
                // Track the kubelet IPs for all nodes.
                up = nodes_rx.recv() => match up {
                    k8s::Event::Applied(node) => self.apply_node(node).context("applying a node"),
                    k8s::Event::Deleted(node) => self.delete_node(&node.name()).context("deleting a node"),
                    k8s::Event::Restarted(nodes) => self.reset_nodes(nodes).context("resetting nodes"),
                },

                up = pods_rx.recv() => match up {
                    k8s::Event::Applied(pod) => self.apply_pod(pod).context("applying a pod"),
                    k8s::Event::Deleted(pod) => self.delete_pod(pod).context("deleting a pod"),
                    k8s::Event::Restarted(pods) => self.reset_pods(pods).context("resetting pods"),
                },

                up = servers_rx.recv() => match up {
                    k8s::Event::Applied(srv) => {
                        self.apply_server(srv);
                        Ok(())
                    }
                    k8s::Event::Deleted(srv) => self.delete_server(srv).context("deleting a server"),
                    k8s::Event::Restarted(srvs) => self.reset_servers(srvs).context("resetting servers"),
                },

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

            // Notify the readiness watch if readiness changes.
            let ready_now = nodes_rx.ready()
                && pods_rx.ready()
                && servers_rx.ready()
                && authorizations_rx.ready();
            if ready != ready_now {
                let _ = ready_tx.send(ready_now);
                ready = ready_now;
                debug!(%ready);
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
