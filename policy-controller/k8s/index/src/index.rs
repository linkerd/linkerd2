//! Keeps track of `Pod`, `Server`, and `ServerAuthorization` resources to
//! provide a dynamic server configuration for all known ports on all pods.
//!
//! The `Index` type exposes a single public method: `Index::pod_server_rx`,
//! which is used to lookup pod/ports (i.e. by the gRPC API). Otherwise, it
//! implements `kubert::index::IndexNamespacedResource` for the indexed
//! kubernetes resources.

use crate::{defaults::DefaultPolicy, pod, server, server_authorization, ClusterInfo};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::{bail, Result};
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, InboundServer, Ipv4Net, Ipv6Net,
    ProxyProtocol,
};
use linkerd_policy_controller_k8s_api::{self as k8s, policy::server::Port, ResourceExt};
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::sync::watch;
use tracing::info_span;

pub type SharedIndex = Arc<RwLock<Index>>;

/// Holds all indexing state. Owned and updated by a single task that processes
/// watch events, publishing results to the shared lookup map for quick lookups
/// in the API server.
#[derive(Debug)]
pub struct Index {
    cluster_info: Arc<ClusterInfo>,
    namespaces: NamespaceIndex,
}

/// Holds all `Pod`, `Server`, and `ServerAuthorization` indices by-namespace.
#[derive(Debug)]
struct NamespaceIndex {
    cluster_info: Arc<ClusterInfo>,
    by_ns: HashMap<String, Namespace>,
}

/// Holds `Pod`, `Server`, and `ServerAuthorization` indices for a single namespace.
#[derive(Debug)]
struct Namespace {
    pods: PodIndex,
    policy: PolicyIndex,
}

/// Holds all pod data for a single namespace.
#[derive(Debug, Default)]
struct PodIndex {
    namespace: String,
    by_name: HashMap<String, Pod>,
}

/// Holds a single pod's data with the server watches for all known ports.
///
/// The set of ports/servers is updated as clients discover server configuration
/// or as `Server` resources select a port.
#[derive(Debug)]
struct Pod {
    meta: pod::Meta,

    /// The pod's named container ports. Used by `Server` port selectors.
    ///
    /// A pod may have multiple ports with the same name. E.g., each container
    /// may have its own `admin-http` port.
    port_names: HashMap<String, pod::PortSet>,

    /// All known TCP server ports. This may be updated by
    /// `Namespace::reindex`--when a port is selected by a `Server`--or by
    /// `Namespace::get_pod_server` when a client discovers a port that has no
    /// configured server (and i.e. uses the default policy).
    port_servers: pod::PortMap<PodPortServer>,
}

/// Holds the state of a single port on a pod.
#[derive(Debug)]
struct PodPortServer {
    /// The name of the server resource that matches this port. Unset when no
    /// server resources match this pod/port (and, i.e., the default policy is
    /// used).
    name: Option<String>,

    /// A sender used to broadcast pod port server updates.
    tx: watch::Sender<InboundServer>,

    /// A receiver that is updated when the pod's server is updated.
    rx: watch::Receiver<InboundServer>,
}

/// Holds the state of policy resources for a single namespace.
#[derive(Debug)]
struct PolicyIndex {
    cluster_info: Arc<ClusterInfo>,
    servers: HashMap<String, server::Server>,
    server_authorizations: HashMap<String, server_authorization::ServerAuthz>,
}

// === impl Index ===

impl Index {
    pub fn shared(cluster_info: impl Into<Arc<ClusterInfo>>) -> SharedIndex {
        let cluster_info = cluster_info.into();
        Arc::new(RwLock::new(Self {
            cluster_info: cluster_info.clone(),
            namespaces: NamespaceIndex {
                cluster_info,
                by_ns: HashMap::new(),
            },
        }))
    }

    /// Obtains a pod:port's server receiver.
    ///
    /// An error is returned if the pod is not found. If the port is not found,
    /// a default is server is created.
    pub fn pod_server_rx(
        &mut self,
        namespace: &str,
        pod: &str,
        port: u16,
    ) -> Result<watch::Receiver<InboundServer>> {
        let ns = self
            .namespaces
            .by_ns
            .get_mut(namespace)
            .ok_or_else(|| anyhow::anyhow!("namespace not found: {}", namespace))?;
        let pod = ns
            .pods
            .by_name
            .get_mut(pod)
            .ok_or_else(|| anyhow::anyhow!("pod {}.{} not found", pod, namespace))?;
        Ok(pod
            .port_server_or_default(port, &self.cluster_info)
            .rx
            .clone())
    }
}

impl kubert::index::IndexNamespacedResource<k8s::Pod> for Index {
    fn apply(&mut self, pod: k8s::Pod) {
        let namespace = pod.namespace().unwrap();
        let name = pod.name();
        let _span = info_span!("apply", ns = %namespace, pod = %name).entered();

        let port_names = pod::tcp_port_names(pod.spec);
        let meta = pod::Meta::from_metadata(pod.metadata);

        // Add or update the pod. If the pod was not already present in the
        // index with the same metadata, index it against the policy resources,
        // updating its watches.
        let ns = self.namespaces.get_or_default(namespace);
        match ns.pods.update(name, meta, port_names) {
            Ok(None) => {}
            Ok(Some(pod)) => pod.reindex_servers(&ns.policy),
            Err(error) => {
                tracing::error!(%error, "Illegal pod update");
            }
        }
    }

    fn delete(&mut self, ns: String, pod: String) {
        let _span = info_span!("delete", %ns, %pod).entered();

        if let Entry::Occupied(mut ns) = self.namespaces.by_ns.entry(ns) {
            // Once the pod is removed, there's nothing else to update. Any open
            // watches will complete.  No other parts of the index need to be
            // updated.
            if ns.get_mut().pods.by_name.remove(&pod).is_some() && ns.get().is_empty() {
                ns.remove();
            }
        }
    }

    // Since apply only reindexes a single pod at a time, there's no need to
    // handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s::policy::Server> for Index {
    fn apply(&mut self, srv: k8s::policy::Server) {
        let ns = srv.namespace().expect("server must be namespaced");
        let name = srv.name();
        let _span = info_span!("apply", %ns, srv = %name).entered();

        let server = server::Server::from_resource(srv, &self.cluster_info);
        self.namespaces
            .get_or_default_with_reindex(ns, |ns| ns.policy.update_server(name, server))
    }

    fn delete(&mut self, ns: String, srv: String) {
        let _span = info_span!("delete", %ns, %srv).entered();
        self.namespaces
            .get_with_reindex(ns, |ns| ns.policy.servers.remove(&srv).is_some())
    }

    fn reset(&mut self, srvs: Vec<k8s::policy::Server>, deleted: HashMap<String, HashSet<String>>) {
        let _span = info_span!("reset").entered();

        #[derive(Default)]
        struct Ns {
            added: Vec<(String, server::Server)>,
            removed: HashSet<String>,
        }

        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for srv in srvs.into_iter() {
            let namespace = srv.namespace().expect("server must be namespaced");
            let name = srv.name();
            let server = server::Server::from_resource(srv, &self.cluster_info);
            updates_by_ns
                .entry(namespace)
                .or_default()
                .added
                .push((name, server));
        }
        for (ns, names) in deleted.into_iter() {
            updates_by_ns.entry(ns).or_default().removed = names;
        }

        for (namespace, Ns { added, removed }) in updates_by_ns.into_iter() {
            if added.is_empty() {
                // If there are no live resources in the namespace, we do not
                // want to create a default namespace instance, we just want to
                // clear out all resources for the namespace (and then drop the
                // whole namespace, if necessary).
                self.namespaces.get_with_reindex(namespace, |ns| {
                    ns.policy.servers.clear();
                    true
                });
            } else {
                // Otherwise, we take greater care to reindex only when the
                // state actually changed. The vast majority of resets will see
                // no actual data change.
                self.namespaces
                    .get_or_default_with_reindex(namespace, |ns| {
                        let mut changed = !removed.is_empty();
                        for name in removed.into_iter() {
                            ns.policy.servers.remove(&name);
                        }
                        for (name, server) in added.into_iter() {
                            changed = ns.policy.update_server(name, server) || changed;
                        }
                        changed
                    });
            }
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s::policy::ServerAuthorization> for Index {
    fn apply(&mut self, saz: k8s::policy::ServerAuthorization) {
        let ns = saz.namespace().unwrap();
        let name = saz.name();
        let _span = info_span!("apply", %ns, saz = %name).entered();

        match server_authorization::ServerAuthz::from_resource(saz, &self.cluster_info) {
            Ok(meta) => self.namespaces.get_or_default_with_reindex(ns, move |ns| {
                ns.policy.update_server_authz(name, meta)
            }),
            Err(error) => tracing::error!(%error, "Illegal server authorization update"),
        }
    }

    fn delete(&mut self, ns: String, saz: String) {
        let _span = info_span!("delete", %ns, %saz).entered();
        self.namespaces.get_with_reindex(ns, |ns| {
            ns.policy.server_authorizations.remove(&saz).is_some()
        })
    }

    fn reset(
        &mut self,
        sazs: Vec<k8s::policy::ServerAuthorization>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        let _span = info_span!("reset");

        #[derive(Default)]
        struct Ns {
            added: Vec<(String, server_authorization::ServerAuthz)>,
            removed: HashSet<String>,
        }

        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for saz in sazs.into_iter() {
            let namespace = saz
                .namespace()
                .expect("serverauthorization must be namespaced");
            let name = saz.name();
            match server_authorization::ServerAuthz::from_resource(saz, &self.cluster_info) {
                Ok(saz) => updates_by_ns
                    .entry(namespace)
                    .or_default()
                    .added
                    .push((name, saz)),
                Err(error) => {
                    tracing::error!(ns = %namespace, saz = %name, %error, "Illegal server authorization update")
                }
            }
        }
        for (ns, names) in deleted.into_iter() {
            updates_by_ns.entry(ns).or_default().removed = names;
        }

        for (namespace, Ns { added, removed }) in updates_by_ns.into_iter() {
            if added.is_empty() {
                // If there are no live resources in the namespace, we do not
                // want to create a default namespace instance, we just want to
                // clear out all resources for the namespace (and then drop the
                // whole namespace, if necessary).
                self.namespaces.get_with_reindex(namespace, |ns| {
                    ns.policy.server_authorizations.clear();
                    true
                });
            } else {
                // Otherwise, we take greater care to reindex only when the
                // state actually changed. The vast majority of resets will see
                // no actual data change.
                self.namespaces
                    .get_or_default_with_reindex(namespace, |ns| {
                        let mut changed = !removed.is_empty();
                        for name in removed.into_iter() {
                            ns.policy.server_authorizations.remove(&name);
                        }
                        for (name, saz) in added.into_iter() {
                            changed = ns.policy.update_server_authz(name, saz) || changed;
                        }
                        changed
                    });
            }
        }
    }
}

// === impl NamespaceIndex ===

impl NamespaceIndex {
    fn get_or_default(&mut self, ns: String) -> &mut Namespace {
        self.by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace::new(ns, self.cluster_info.clone()))
    }

    /// Gets the given namespace and, if it exists, passes it to the given
    /// function. When the function returns `true`, all pods in the namespace are
    /// reindexed or, if the namespace is empty, the namespace is removed
    /// entirely.
    fn get_with_reindex(&mut self, namespace: String, f: impl FnOnce(&mut Namespace) -> bool) {
        if let Entry::Occupied(mut ns) = self.by_ns.entry(namespace) {
            if f(ns.get_mut()) {
                if ns.get().is_empty() {
                    ns.remove();
                } else {
                    ns.get_mut().reindex();
                }
            }
        }
    }

    /// Gets the given namespace (or creates it) and passes it to the given
    /// function. If the function returns true, all pods in the namespace are
    /// reindexed.
    fn get_or_default_with_reindex(
        &mut self,
        namespace: String,
        f: impl FnOnce(&mut Namespace) -> bool,
    ) {
        let ns = self.get_or_default(namespace);
        if f(ns) {
            ns.reindex();
        }
    }
}

// === impl Namespace ===

impl Namespace {
    fn new(namespace: String, cluster_info: Arc<ClusterInfo>) -> Self {
        Namespace {
            pods: PodIndex {
                namespace,
                by_name: HashMap::default(),
            },
            policy: PolicyIndex {
                cluster_info,
                servers: HashMap::default(),
                server_authorizations: HashMap::default(),
            },
        }
    }

    /// Returns true if the index does not include any resources.
    #[inline]
    fn is_empty(&self) -> bool {
        self.pods.is_empty() && self.policy.is_empty()
    }

    #[inline]
    fn reindex(&mut self) {
        self.pods.reindex(&self.policy);
    }
}

// === impl PodIndex ===

impl PodIndex {
    #[inline]
    fn is_empty(&self) -> bool {
        self.by_name.is_empty()
    }

    fn update(
        &mut self,
        name: String,
        meta: pod::Meta,
        port_names: HashMap<String, pod::PortSet>,
    ) -> Result<Option<&mut Pod>> {
        let pod = match self.by_name.entry(name) {
            Entry::Vacant(entry) => {
                tracing::debug!(?meta, ?port_names, "Creating");
                let pod = Pod {
                    meta,
                    port_names,
                    port_servers: pod::PortMap::default(),
                };
                entry.insert(pod)
            }

            Entry::Occupied(entry) => {
                let pod = entry.into_mut();

                // Pod labels and annotations may change at runtime, but the
                // port list may not
                if pod.port_names != port_names {
                    bail!("pod port names must not change");
                }

                // If there aren't meaningful changes, then don't bother doing
                // any more work.
                if pod.meta == meta {
                    tracing::trace!("No changes");
                    return Ok(None);
                }
                tracing::debug!(?meta, "Updating");
                pod.meta = meta;
                pod
            }
        };
        Ok(Some(pod))
    }

    fn reindex(&mut self, policy: &PolicyIndex) {
        let _span = info_span!("reindex", ns = %self.namespace).entered();
        for (name, pod) in self.by_name.iter_mut() {
            let _span = info_span!("pod", pod = %name).entered();
            pod.reindex_servers(policy);
        }
    }
}

// === impl Pod ===

impl Pod {
    /// Determines the policies for ports on this pod.
    fn reindex_servers(&mut self, policy: &PolicyIndex) {
        tracing::debug!("Indexing servers for pod");

        // Keep track of the ports that are already known in the pod so that, after applying server
        // matches, we can ensure remaining ports are set to the default policy.
        let mut unmatched_ports = self.port_servers.keys().copied().collect::<pod::PortSet>();

        // Keep track of which ports have been matched to servers to that we can detect when
        // multiple servers match a single port.
        //
        // We start with capacity for the known ports on the pod; but this can grow if servers
        // select additional ports.
        let mut matched_ports = pod::PortMap::with_capacity_and_hasher(
            unmatched_ports.len(),
            std::hash::BuildHasherDefault::<pod::PortHasher>::default(),
        );

        for (srvname, server) in policy.servers.iter() {
            if server.pod_selector.matches(&self.meta.labels) {
                for port in self.select_ports(&server.port_ref).into_iter() {
                    // If the port is already matched to a server, then log a warning and skip
                    // updating it so it doesn't flap between servers.
                    if let Some(prior) = matched_ports.get(&port) {
                        tracing::warn!(
                            port = %port,
                            server = %prior,
                            conflict = %srvname,
                            "Port already matched by another server; skipping"
                        );
                        continue;
                    }

                    let s = policy.inbound_server(srvname.clone(), server);
                    self.update_server(port, srvname, s);

                    matched_ports.insert(port, srvname.clone());
                    unmatched_ports.remove(&port);
                }
            }
        }

        // Reset all remaining ports to the default policy.
        for port in unmatched_ports.into_iter() {
            self.set_default_server(port, &policy.cluster_info);
        }
    }

    /// Updates a pod-port to use the given named server.
    ///
    /// The name is used explicity (and not derived from the `server` itself) to
    /// ensure that we're not handling a default server.
    fn update_server(&mut self, port: u16, name: &str, server: InboundServer) {
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
                if ps.name.as_deref() == Some(name) && *ps.rx.borrow() == server {
                    tracing::trace!(port = %port, server = %name, "Skipped redundant server update");
                    return;
                }

                // If the port's server previously matched a different server,
                // this can either mean that multiple servers currently match
                // the pod:port, or that we're in the middle of an update. We
                // make the opportunistic choice to assume the cluster is
                // configured coherently so we take the update. The admission
                // controller should prevent conflicts.
                ps.name = Some(name.to_string());
                ps.tx.send(server).expect("a receiver is held by the index");
            }
        }

        tracing::debug!(port = %port, server = %name, "Updated server");
    }

    /// Updates a pod-port to use the given named server.
    fn set_default_server(&mut self, port: u16, config: &ClusterInfo) {
        let server = Self::default_inbound_server(port, &self.meta.settings, config);
        tracing::debug!(%port, server = %config.default_policy, "Setting default server");
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

    /// Enumerates ports.
    ///
    /// A named port may refer to an arbitrary number of port numbers.
    fn select_ports(&mut self, port_ref: &Port) -> Vec<u16> {
        match port_ref {
            Port::Number(p) => Some(*p).into_iter().collect(),
            Port::Name(name) => self
                .port_names
                .get(name)
                .into_iter()
                .flatten()
                .cloned()
                .collect(),
        }
    }

    fn port_server_or_default(&mut self, port: u16, config: &ClusterInfo) -> &mut PodPortServer {
        match self.port_servers.entry(port) {
            Entry::Occupied(entry) => entry.into_mut(),
            Entry::Vacant(entry) => {
                let (tx, rx) = watch::channel(Self::default_inbound_server(
                    port,
                    &self.meta.settings,
                    config,
                ));
                entry.insert(PodPortServer { name: None, tx, rx })
            }
        }
    }

    fn default_inbound_server(
        port: u16,
        settings: &pod::Settings,
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
                vec![Ipv4Net::default().into(), Ipv6Net::default().into()]
            };
            authorizations.insert(
                format!("default:{}", policy),
                ClientAuthorization {
                    authentication,
                    networks,
                },
            );
        };

        tracing::trace!(port, ?settings, %policy, ?protocol, ?authorizations, "default server");
        InboundServer {
            name: format!("default:{}", policy),
            protocol,
            authorizations,
        }
    }
}

// === impl PolicyIndex ===

impl PolicyIndex {
    #[inline]
    fn is_empty(&self) -> bool {
        self.servers.is_empty() && self.server_authorizations.is_empty()
    }

    fn update_server(&mut self, name: String, server: server::Server) -> bool {
        match self.servers.entry(name.clone()) {
            Entry::Vacant(entry) => {
                entry.insert(server);
            }
            Entry::Occupied(entry) => {
                let srv = entry.into_mut();
                if *srv == server {
                    tracing::debug!(server = %name, "no changes");
                    return false;
                }
                tracing::debug!(server = %name, "updating");
                *srv = server;
            }
        }
        true
    }

    fn update_server_authz(
        &mut self,
        name: String,
        server_authz: server_authorization::ServerAuthz,
    ) -> bool {
        match self.server_authorizations.entry(name) {
            Entry::Vacant(entry) => {
                entry.insert(server_authz);
            }
            Entry::Occupied(entry) => {
                let saz = entry.into_mut();
                if *saz == server_authz {
                    return false;
                }
                *saz = server_authz;
            }
        }
        true
    }

    fn inbound_server(&self, name: String, server: &server::Server) -> InboundServer {
        let authorizations = self.client_authzs(&name, server);
        InboundServer {
            name,
            authorizations,
            protocol: server.protocol.clone(),
        }
    }

    fn client_authzs(
        &self,
        server_name: &str,
        server: &server::Server,
    ) -> HashMap<String, ClientAuthorization> {
        self.server_authorizations
            .iter()
            .filter_map(|(name, saz)| {
                if saz.server_selector.selects(server_name, &server.labels) {
                    Some((name.to_string(), saz.authz.clone()))
                } else {
                    None
                }
            })
            .collect()
    }
}
