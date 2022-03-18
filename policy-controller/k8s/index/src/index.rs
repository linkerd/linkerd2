//! This module handles all of the indexing logic without dealing with the specifics of how the
//! resources are laid out in the Kubernetes API (e.g annotation handling is done in the various
//! resource-specific modules).
//!
//! The `Index` type exposes a single public method: `Index::pod_server_rx`, which is used to lookup
//! pod/ports by discovery clients. Its other methods, as well as the `NamespaceIndex` type, are
//! only exposed within the crate to facilitate indexing via `kubert::index`.

use crate::{defaults::DefaultPolicy, ClusterInfo};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::{bail, Result};
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, InboundServer, IpNet, ProxyProtocol,
};
use linkerd_policy_controller_k8s_api::{self as k8s, policy::server::Port};
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::sync::watch;

pub type SharedIndex = Arc<RwLock<Index>>;

/// Holds all indexing state. Owned and updated by a single task that processes watch events,
/// publishing results to the shared lookup map for quick lookups in the API server.
#[derive(Debug)]
pub struct Index {
    /// Holds per-namespace pod/server/authorization indexes.
    namespaces: HashMap<String, NamespaceIndex>,

    cluster_info: Arc<ClusterInfo>,
}

/// Holds the state of a single namespace.
#[derive(Debug)]
pub(crate) struct NamespaceIndex {
    /// Holds per-pod port indexes.
    pods: HashMap<String, PodIndex>,

    /// Holds servers by-name
    servers: HashMap<String, Server>,

    /// Holds server authorizations by-name
    server_authorizations: HashMap<String, ServerAuthorization>,

    cluster_info: Arc<ClusterInfo>,
}

/// Per-pod settings, as configured by the pod's annotations.
#[derive(Debug, Default, PartialEq)]
pub(crate) struct PodSettings {
    pub require_id_ports: HashSet<u16>,
    pub opaque_ports: HashSet<u16>,
    pub default_policy: Option<DefaultPolicy>,
}

/// Selects `Server`s for a `ServerAuthoriation`
#[derive(Clone, Debug, PartialEq)]
pub(crate) enum ServerSelector {
    Name(String),
    Selector(k8s::labels::Selector),
}

/// A pod's port index.
#[derive(Debug)]
struct PodIndex {
    /// The pod's labels. Used by `Server` pod selectors.
    labels: k8s::Labels,

    settings: PodSettings,

    /// The pod's named container ports. Used by `Server` port selectors.
    ///
    /// A pod may have multiple ports with the same name. E.g., each container may have its own
    /// `admin-http` port.
    port_names: HashMap<String, HashSet<u16>>,

    /// All known TCP server ports. This may be updated by `NamespaceIndex::reindex`--when a port is
    /// selected by a `Server`--or by `NamespaceIndex::get_pod_server` when a client discovers a
    /// port that has no configured server (and i.e. uses the default policy).
    port_servers: HashMap<u16, PodPortServer>,
}

#[derive(Debug)]
struct PodPortServer {
    /// The name of the server resource that matches this port. Unset when no server resources match
    /// this pod/port (and, i.e., the default policy is used).
    name: Option<String>,

    /// A sender used to broadcast pod port server updates.
    tx: watch::Sender<InboundServer>,

    /// A receiver that is updated when the pod's server is updated.
    rx: watch::Receiver<InboundServer>,
}

/// The important parts of a `Server` resource.
#[derive(Debug, PartialEq)]
struct Server {
    name: String,
    labels: k8s::Labels,
    pod_selector: k8s::labels::Selector,
    port_ref: Port,
    protocol: ProxyProtocol,
}

/// The important parts of a `ServerAuthorization` resource.
#[derive(Clone, Debug, PartialEq)]
struct ServerAuthorization {
    authz: ClientAuthorization,
    server_selector: ServerSelector,
}

// === impl Index ===

impl Index {
    pub fn shared(cluster_info: impl Into<Arc<ClusterInfo>>) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            cluster_info: cluster_info.into(),
            namespaces: HashMap::default(),
        }))
    }

    /// Obtains a pod:port's server receiver.
    ///
    /// This receiver is updated as servers and authorizations change.
    ///
    /// The receiver closes when the pod is removed from the index.
    pub fn pod_server_rx(
        &mut self,
        namespace: &str,
        pod: &str,
        port: u16,
    ) -> Result<watch::Receiver<InboundServer>> {
        let ns = self
            .namespaces
            .get_mut(namespace)
            .ok_or_else(|| anyhow::anyhow!("namespace not found: {}", namespace))?;
        let pod = ns
            .pods
            .get_mut(pod)
            .ok_or_else(|| anyhow::anyhow!("pod {}.{} not found", pod, namespace))?;
        Ok(pod.get_or_default(port, &*self.cluster_info).rx.clone())
    }

    pub(crate) fn cluster_info(&self) -> &ClusterInfo {
        &*self.cluster_info
    }

    pub(crate) fn entry(&mut self, ns: String) -> Entry<'_, String, NamespaceIndex> {
        self.namespaces.entry(ns)
    }

    pub(crate) fn ns_or_default(&mut self, ns: impl ToString) -> &mut NamespaceIndex {
        self.namespaces
            .entry(ns.to_string())
            .or_insert_with(|| NamespaceIndex {
                cluster_info: self.cluster_info.clone(),
                pods: HashMap::default(),
                servers: HashMap::default(),
                server_authorizations: HashMap::default(),
            })
    }
}

impl NamespaceIndex {
    /// Returns true if the index does not include any resources.
    pub(crate) fn is_empty(&self) -> bool {
        self.pods.is_empty() && self.servers.is_empty() && self.server_authorizations.is_empty()
    }

    /// Adds or updates a Pod.
    ///
    /// Labels may be updated but port names may not be updated after a pod is created.
    ///
    /// Returns true if the Pod was updated and false if it already existed and was unchanged.
    pub(crate) fn apply_pod(
        &mut self,
        name: impl ToString,
        labels: k8s::Labels,
        port_names: HashMap<String, HashSet<u16>>,
        settings: PodSettings,
    ) -> Result<()> {
        let pod = match self.pods.entry(name.to_string()) {
            Entry::Vacant(entry) => entry.insert(PodIndex {
                labels,
                port_names,
                settings,
                port_servers: HashMap::default(),
            }),

            Entry::Occupied(entry) => {
                let pod = entry.into_mut();

                // Pod labels and annotations may change at runtime, but the port list may not
                if pod.port_names != port_names {
                    bail!("pod {} port names must not change", name.to_string());
                }

                // If there aren't meaningful changes, then don't bother doing any more work.
                if pod.settings == settings && pod.labels == labels {
                    tracing::debug!(pod = %name.to_string(), "no changes");
                    return Ok(());
                }
                tracing::debug!(pod = %name.to_string(), "updating");
                pod.settings = settings;
                pod.labels = labels;
                pod
            }
        };

        // Snapshot the current list of server ports so we can track which ones are matched to
        // servers. This mainly handles the case where a pod's labels/default policy changes at
        // runtime.
        let mut snapshot = pod.port_servers.keys().copied().collect::<HashSet<_>>();
        tracing::trace!(?snapshot);

        // Determine which servers match ports on this pod and update the port servers. This will
        // populate a list of
        for server in self.servers.values() {
            if server.pod_selector.matches(&pod.labels) {
                for port in pod.select_ports(&server.port_ref).into_iter() {
                    tracing::debug!(pod = %name.to_string(), server = %server.name, %port, "updating server");
                    let s = mk_inbound_server(
                        server,
                        mk_client_authzs(server, &self.server_authorizations),
                    );
                    pod.update_server(port, server.name.clone(), s);
                    snapshot.remove(&port);
                }
            }
        }

        // If there are remaining ports that are not matched by a server, ensure they have the pod's
        // default server applies.
        if !snapshot.is_empty() {
            for port in snapshot.into_iter() {
                tracing::debug!(pod = %name.to_string(), %port, "setting defaul server");
                pod.set_default_server(port, &self.cluster_info);
            }
        }
        Ok(())
    }

    /// Deletes a Pod from the index.
    pub(crate) fn delete_pod(&mut self, name: &str) {
        // Once the pod is removed, there's nothing else to update. Any open watches will complete.
        self.pods.remove(name);
    }

    /// Adds or updates a Server.
    ///
    /// Returns true if the Server was updated and false if it already existed and was unchanged.
    pub(crate) fn apply_server(
        &mut self,
        name: impl ToString,
        labels: k8s::Labels,
        pod_selector: k8s::labels::Selector,
        port_ref: Port,
        protocol: Option<ProxyProtocol>,
    ) {
        let server = Server {
            name: name.to_string(),
            labels,
            pod_selector,
            port_ref,
            protocol: protocol.unwrap_or(ProxyProtocol::Detect {
                timeout: self.cluster_info.default_detect_timeout,
            }),
        };

        let server = match self.servers.entry(name.to_string()) {
            Entry::Vacant(entry) => entry.insert(server),
            Entry::Occupied(entry) => {
                let srv = entry.into_mut();
                if *srv == server {
                    tracing::debug!(server = %server.name, "no changes");
                    return;
                }
                tracing::debug!(server = %server.name, "updating");
                *srv = server;
                srv
            }
        };

        for pod in self.pods.values_mut() {
            if server.pod_selector.matches(&pod.labels) {
                // If the server selects the pod, then update all matching ports on the pod.
                let s = mk_inbound_server(
                    server,
                    mk_client_authzs(server, &self.server_authorizations),
                );
                for port in pod.select_ports(&server.port_ref).into_iter() {
                    pod.update_server(port, name.to_string(), s.clone());
                }
            } else {
                // If the server used to select the pod but no longer does, then we need to revert
                // it to the pod's default server.
                //
                // We need to create a new vector of server ports so we can access `pod` mutably.
                #[allow(clippy::needless_collect)]
                let server_ports = pod
                    .port_servers
                    .iter()
                    .filter_map(|(port, ps)| {
                        if ps.name.as_ref() == Some(&server.name) {
                            Some(port)
                        } else {
                            None
                        }
                    })
                    .copied()
                    .collect::<Vec<_>>();
                for port in server_ports.into_iter() {
                    pod.set_default_server(port, &self.cluster_info);
                }
            }
        }
    }

    /// Deletes a Server from the index, reverting all pods that use it to use their default server.
    ///
    /// Returns true if the Server was deleted and false if it did not exist.
    pub(crate) fn delete_server(&mut self, name: &str) {
        if self.servers.remove(name).is_none() {
            return;
        }

        for pod in self.pods.values_mut() {
            for (port, ps) in pod.port_servers.iter_mut() {
                if ps.name.as_deref() == Some(name) {
                    ps.name = None;
                    let server = default_inbound_server(*port, &pod.settings, &*self.cluster_info);
                    ps.tx.send(server).expect("receiver is held by the index");
                }
            }
        }
    }

    /// Adds or updates a ServerAuthorization.
    ///
    /// Returns true if the ServerAuthorization was updated and false if it already existed and was
    /// unchanged.
    pub(crate) fn apply_server_authorization(
        &mut self,
        name: impl ToString,
        server_selector: ServerSelector,
        authz: ClientAuthorization,
    ) {
        let server_authz = ServerAuthorization {
            authz,
            server_selector: server_selector.clone(),
        };
        match self.server_authorizations.entry(name.to_string()) {
            Entry::Vacant(entry) => {
                entry.insert(server_authz);
            }
            Entry::Occupied(entry) => {
                let saz = entry.into_mut();
                if *saz == server_authz {
                    return;
                }
                *saz = server_authz;
            }
        }

        for (srvname, server) in self.servers.iter() {
            if server_selector.selects(server) {
                let update = mk_inbound_server(
                    server,
                    mk_client_authzs(server, &self.server_authorizations),
                );
                for pod in self.pods.values_mut() {
                    if server.pod_selector.matches(&pod.labels) {
                        for port in pod.select_ports(&server.port_ref).into_iter() {
                            pod.update_server(port, srvname.to_string(), update.clone());
                        }
                    }
                }
            }
        }
    }

    /// Deletes a ServerAuthorization from the index.
    pub(crate) fn delete_server_authorization(&mut self, name: &str) {
        let saz = match self.server_authorizations.remove(name) {
            Some(saz) => saz,
            None => return,
        };

        // Update all pods that use servers that were formerly selected by this authorization.
        for (srvname, server) in self.servers.iter() {
            if saz.server_selector.selects(server) {
                let update = mk_inbound_server(
                    server,
                    mk_client_authzs(server, &self.server_authorizations),
                );
                for pod in self.pods.values_mut() {
                    if server.pod_selector.matches(&pod.labels) {
                        for port in pod.select_ports(&server.port_ref).into_iter() {
                            pod.update_server(port, srvname.to_string(), update.clone());
                        }
                    }
                }
            }
        }
    }
}

// === impl PodIndex ===

impl PodIndex {
    fn update_server(&mut self, port: u16, name: impl ToString, server: InboundServer) {
        match self.port_servers.entry(port) {
            Entry::Vacant(entry) => {
                let (tx, rx) = watch::channel(server);
                entry.insert(PodPortServer {
                    name: Some(name.to_string()),
                    tx,
                    rx,
                });
            }

            Entry::Occupied(mut entry) => {
                let ps = entry.get_mut();

                // Avoid sending redundant updates.
                if *ps.rx.borrow() == server {
                    return;
                }

                // If the port's server previously matched a different server, this can either mean
                // that multiple servers currently match the pod:port, or that we're in the middle
                // of an update. We make the opportunistic choice to assume the cluster is
                // configured coherently so we take the update. The admission controller should
                // prevent conflicts.
                ps.name = Some(name.to_string());
                ps.tx.send(server).expect("a receiver is held by the index");
            }
        }
    }

    fn set_default_server(&mut self, port: u16, config: &ClusterInfo) {
        let server = default_inbound_server(port, &self.settings, config);
        match self.port_servers.entry(port) {
            Entry::Vacant(entry) => {
                let (tx, rx) = watch::channel(server);
                entry.insert(PodPortServer { name: None, tx, rx });
            }

            Entry::Occupied(mut entry) => {
                let ps = entry.get_mut();

                // Avoid sending redundant updates.
                if *ps.rx.borrow() == server {
                    return;
                }

                ps.name = None;
                ps.tx.send(server).expect("a receiver is held by the index");
            }
        }
    }

    fn select_ports(&mut self, port_ref: &Port) -> Vec<u16> {
        match port_ref {
            Port::Number(p) => Some(*p).into_iter().collect(),
            Port::Name(name) => self
                .port_names
                .get(name)
                .cloned()
                .into_iter()
                .flatten()
                .collect(),
        }
    }

    fn get_or_default(&mut self, port: u16, config: &ClusterInfo) -> &mut PodPortServer {
        match self.port_servers.entry(port) {
            Entry::Occupied(entry) => entry.into_mut(),
            Entry::Vacant(entry) => {
                let (tx, rx) = watch::channel(default_inbound_server(port, &self.settings, config));
                entry.insert(PodPortServer { name: None, tx, rx })
            }
        }
    }
}

// === impl ServerSelector ===

impl ServerSelector {
    fn selects(&self, server: &Server) -> bool {
        match self {
            Self::Name(n) => *n == server.name,
            Self::Selector(selector) => selector.matches(&server.labels),
        }
    }
}

// === helpers ===

fn default_inbound_server(
    port: u16,
    settings: &PodSettings,
    config: &ClusterInfo,
) -> InboundServer {
    let protocol = if settings.opaque_ports.contains(&port) {
        ProxyProtocol::Opaque
    } else {
        ProxyProtocol::Detect {
            timeout: config.default_detect_timeout,
        }
    };

    let mut policy = settings.default_policy.unwrap_or(config.default_policy);
    if settings.require_id_ports.contains(&port) {
        if let DefaultPolicy::Allow {
            ref mut authenticated_only,
            ..
        } = policy
        {
            *authenticated_only = true;
        }
    }

    let mut authorizations = HashMap::default();
    if let DefaultPolicy::Allow {
        authenticated_only,
        cluster_only,
    } = policy
    {
        let authentication = if authenticated_only {
            ClientAuthentication::TlsAuthenticated(vec![IdentityMatch::Suffix(vec![])])
        } else {
            ClientAuthentication::Unauthenticated
        };
        let networks = if cluster_only {
            config.networks.iter().copied().map(Into::into).collect()
        } else {
            vec![
                "0.0.0.0/0".parse::<IpNet>().unwrap().into(),
                "::/0".parse::<IpNet>().unwrap().into(),
            ]
        };
        authorizations.insert(
            format!("default:{}", policy),
            ClientAuthorization {
                authentication,
                networks,
            },
        );
    };

    InboundServer {
        name: format!("default:{}", policy),
        protocol,
        authorizations,
    }
}

fn mk_client_authzs(
    server: &Server,
    server_authzs: &HashMap<String, ServerAuthorization>,
) -> HashMap<String, ClientAuthorization> {
    server_authzs
        .iter()
        .filter_map(move |(name, saz)| {
            if saz.server_selector.selects(server) {
                Some((name.to_string(), saz.authz.clone()))
            } else {
                None
            }
        })
        .collect()
}

fn mk_inbound_server(
    server: &Server,
    authorizations: HashMap<String, ClientAuthorization>,
) -> InboundServer {
    InboundServer {
        name: server.name.clone(),
        protocol: server.protocol.clone(),
        authorizations,
    }
}
