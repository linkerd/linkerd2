//! This module handles all of the indexing logic without dealing with the specifics of how the
//! resources are laid out in the Kubernetes API. This makes the set of inputs and outputs explicit.
//!
//! The `Index` type exposes a single public method: `Index::pod_server_rx`, which is used to lookup
//! pod/ports by discovery clients.
//!
//! Its other methods, as well as the `Namespace` type, are only exposed within the crate to
//! facilitate indexing via `kubert::index` handlers, which are implemented in the `pod`, `server`,
//! `server_authorization`, etc. modules.

use crate::{defaults::DefaultPolicy, ClusterInfo};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::{bail, Result};
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, InboundServer, IpNet, NetworkMatch,
    ProxyProtocol,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{server::Port, MeshTLSAuthenticationSpec, NetworkAuthenticationSpec},
};
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::sync::watch;

pub type SharedIndex = Arc<RwLock<Index>>;

/// Holds all indexing state. Owned and updated by a single task that processes watch events,
/// publishing results to the shared lookup map for quick lookups in the API server.
#[derive(Debug)]
pub struct Index {
    /// Holds per-namespace pod/server/authorization indexes.
    namespaces: NamespaceIndex,

    /// Holds Authentication resources by-namespace,
    authentications: HashMap<String, NsAuthenticationIndex>,

    cluster_info: Arc<ClusterInfo>,
}

#[derive(Debug)]
struct NamespaceIndex {
    cluster_info: Arc<ClusterInfo>,

    namespaces: HashMap<String, Namespace>,
}

/// Holds the state of a single namespace.
#[derive(Debug)]
struct Namespace {
    /// Holds per-pod port indexes.
    pods: HashMap<String, PodIndex>,

    policy: PolicyIndex,
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

#[derive(Clone, Debug, PartialEq)]
pub(crate) enum AuthorizationPolicyTarget {
    Server(String),
}

#[derive(Clone, Debug, PartialEq)]
pub(crate) enum AuthenticationTarget {
    Network {
        namespace: Option<String>,
        name: String,
    },
    MeshTLS {
        namespace: Option<String>,
        name: String,
    },
}

#[derive(Clone, Debug, PartialEq)]
pub(crate) enum Authentication {
    Network(Arc<[NetworkMatch]>),
    MeshTLS(Arc<[IdentityMatch]>),
}

/// A pod's port index.
#[derive(Debug)]
struct PodIndex {
    /// The pod's name. Used for logging.
    name: String,

    /// Holds pod metadata/config that can change.
    meta: PodMeta,

    /// The pod's named container ports. Used by `Server` port selectors.
    ///
    /// A pod may have multiple ports with the same name. E.g., each container may have its own
    /// `admin-http` port.
    port_names: HashMap<String, HashSet<u16>>,

    /// All known TCP server ports. This may be updated by `Namespace::reindex`--when a port is
    /// selected by a `Server`--or by `Namespace::get_pod_server` when a client discovers a
    /// port that has no configured server (and i.e. uses the default policy).
    port_servers: HashMap<u16, PodPortServer>,
}

/// Holds pod metadata/config that can change.
#[derive(Debug, PartialEq)]
struct PodMeta {
    /// The pod's labels. Used by `Server` pod selectors.
    labels: k8s::Labels,

    // Pod-specific settings (i.e., derived from annotations).
    settings: PodSettings,
}

/// Holds the state of a single port on a pod.
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

/// Holds the state of policy resources for a single namespace.
#[derive(Debug)]
struct PolicyIndex {
    /// Holds servers by-name
    servers: HashMap<String, Server>,

    authorization_policies: HashMap<String, AuthorizationPolicy>,

    server_authorizations: HashMap<String, ServerAuthorization>,

    cluster_info: Arc<ClusterInfo>,
}

/// Holds all of the authentication targets for a namespace.
///
/// This is its own data structure so that `Namespace` indexing may read data from all other
/// namespaces (instead of resources only from the indexed namespace).
#[derive(Debug, Default)]
struct NsAuthenticationIndex {
    network: HashMap<String, Arc<[NetworkMatch]>>,
    meshtls: HashMap<String, Arc<[IdentityMatch]>>,
}

/// The parts of a `Server` resource that can change.
#[derive(Debug, PartialEq)]
struct Server {
    labels: k8s::Labels,
    pod_selector: k8s::labels::Selector,
    port_ref: Port,
    protocol: ProxyProtocol,
}

/// The parts of a `ServerAuthorization` resource that can chagne.
#[derive(Debug, PartialEq)]
struct ServerAuthorization {
    authz: ClientAuthorization,
    server_selector: ServerSelector,
}

#[derive(Clone, Debug, PartialEq)]
struct AuthorizationPolicy {
    target: AuthorizationPolicyTarget,
    authentications: Vec<AuthenticationTarget>,
}

// === impl Index ===

impl Index {
    pub fn shared(cluster_info: impl Into<Arc<ClusterInfo>>) -> SharedIndex {
        let cluster_info = cluster_info.into();
        Arc::new(RwLock::new(Self {
            cluster_info: cluster_info.clone(),
            authentications: HashMap::new(),
            namespaces: NamespaceIndex {
                cluster_info,
                namespaces: HashMap::new(),
            },
        }))
    }

    /// Obtains a pod:port's server receiver.
    ///
    /// An error is returned if the pod is not found. If the port is not found, a default is server
    /// is created.
    pub fn pod_server_rx(
        &mut self,
        namespace: &str,
        pod: &str,
        port: u16,
    ) -> Result<watch::Receiver<InboundServer>> {
        let ns = self
            .namespaces
            .namespaces
            .get_mut(namespace)
            .ok_or_else(|| anyhow::anyhow!("namespace not found: {}", namespace))?;
        let pod = ns
            .pods
            .get_mut(pod)
            .ok_or_else(|| anyhow::anyhow!("pod {}.{} not found", pod, namespace))?;
        Ok(pod
            .port_server_or_default(port, &*self.cluster_info)
            .rx
            .clone())
    }

    pub(crate) fn cluster_info(&self) -> &ClusterInfo {
        &*self.cluster_info
    }

    /// Adds or updates a Pod.
    ///
    /// Labels may be updated but port names may not be updated after a pod is created.
    ///
    /// Returns true if the Pod was updated and false if it already existed and was unchanged.
    pub(crate) fn apply_pod(
        &mut self,
        namespace: String,
        name: String,
        labels: k8s::Labels,
        port_names: HashMap<String, HashSet<u16>>,
        settings: PodSettings,
    ) -> Result<()> {
        let meta = PodMeta { labels, settings };
        let ns = self.namespaces.get_or_default(namespace.clone());
        let pod = match ns.pods.entry(name.to_string()) {
            Entry::Vacant(entry) => entry.insert(PodIndex {
                name,
                meta,
                port_names,
                port_servers: HashMap::default(),
            }),

            Entry::Occupied(entry) => {
                let pod = entry.into_mut();

                // Pod labels and annotations may change at runtime, but the port list may not
                if pod.port_names != port_names {
                    bail!("pod {} port names must not change", name);
                }

                // If there aren't meaningful changes, then don't bother doing any more work.
                if pod.meta == meta {
                    tracing::debug!(pod = %name, "No changes");
                    return Ok(());
                }
                tracing::debug!(pod = %name, "Updating");
                pod.meta = meta;
                pod
            }
        };

        pod.reindex_servers(&namespace, &self.authentications, &ns.policy);

        Ok(())
    }

    /// Deletes a Pod from the index.
    pub(crate) fn delete_pod(&mut self, namespace: String, name: &str) {
        if let Entry::Occupied(mut ns) = self.namespaces.namespaces.entry(namespace) {
            // Once the pod is removed, there's nothing else to update. Any open watches will complete.
            // No other parts of the index need to be updated.
            if ns.get_mut().pods.remove(name).is_some() && ns.get().is_empty() {
                ns.remove();
            }
        }
    }

    /// Adds or updates a Server.
    ///
    /// Returns true if the Server was updated and false if it already existed and was unchanged.
    pub(crate) fn apply_server(
        &mut self,
        namespace: String,
        name: String,
        labels: k8s::Labels,
        pod_selector: k8s::labels::Selector,
        port_ref: Port,
        protocol: Option<ProxyProtocol>,
    ) {
        self.ns_with_default_reindexed(namespace, |ns| {
            let server = Server {
                labels,
                pod_selector,
                port_ref,
                protocol: protocol.unwrap_or(ProxyProtocol::Detect {
                    timeout: ns.policy.cluster_info.default_detect_timeout,
                }),
            };

            match ns.policy.servers.entry(name.to_string()) {
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
        })
    }

    /// Deletes a Server from the index, reverting all pods that use it to use their default server.
    ///
    /// Returns true if the Server was deleted and false if it did not exist.
    pub(crate) fn delete_server(&mut self, namespace: String, name: &str) {
        self.ns_with_reindexed(namespace, |ns| ns.policy.servers.remove(name).is_some())
    }

    /// Adds or updates a ServerAuthorization.
    ///
    /// Returns true if the ServerAuthorization was updated and false if it already existed and was
    /// unchanged.
    pub(crate) fn apply_server_authorization(
        &mut self,
        namespace: String,
        name: String,
        server_selector: ServerSelector,
        authz: ClientAuthorization,
    ) {
        self.ns_with_default_reindexed(namespace, move |ns| {
            let server_authz = ServerAuthorization {
                authz,
                server_selector,
            };
            match ns.policy.server_authorizations.entry(name) {
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
        })
    }

    /// Deletes a ServerAuthorization from the index.
    pub(crate) fn delete_server_authorization(&mut self, namespace: String, name: &str) {
        self.ns_with_reindexed(namespace, |ns| {
            ns.policy.server_authorizations.remove(name).is_some()
        })
    }

    pub(crate) fn apply_authorization_policy(
        &mut self,
        namespace: String,
        name: String,
        target: AuthorizationPolicyTarget,
        authentications: Vec<AuthenticationTarget>,
    ) {
        self.ns_with_default_reindexed(namespace, |ns| {
            let authz = AuthorizationPolicy {
                target,
                authentications,
            };
            match ns.policy.authorization_policies.entry(name) {
                Entry::Vacant(entry) => {
                    entry.insert(authz);
                }
                Entry::Occupied(entry) => {
                    let ap = entry.into_mut();
                    if *ap == authz {
                        return false;
                    }
                    *ap = authz;
                }
            }
            true
        })
    }

    pub(crate) fn delete_authorization_policy(&mut self, namespace: String, name: &str) {
        self.ns_with_reindexed(namespace, |ns| {
            ns.policy.authorization_policies.remove(name).is_some()
        })
    }

    pub(crate) fn apply_meshtls_authentication(
        &mut self,
        namespace: String,
        name: String,
        identities: Vec<IdentityMatch>,
    ) {
        self.authn_with_all_reindexed(namespace, |authns| {
            match authns.meshtls.entry(name) {
                Entry::Vacant(entry) => {
                    entry.insert(identities.into());
                }
                Entry::Occupied(entry) => {
                    let ap = entry.into_mut();
                    if **ap == *identities {
                        return false;
                    }
                    *ap = identities.into();
                }
            }
            true
        })
    }

    pub(crate) fn delete_meshtls_authentication(&mut self, namespace: String, name: &str) {
        self.authn_with_all_reindexed(namespace, |authns| authns.meshtls.remove(name).is_some())
    }

    pub(crate) fn apply_network_authentication(
        &mut self,
        namespace: String,
        name: String,
        networks: Vec<NetworkMatch>,
    ) {
        self.authn_with_all_reindexed(namespace, |authns| {
            match authns.network.entry(name) {
                Entry::Vacant(entry) => {
                    entry.insert(networks.into());
                }
                Entry::Occupied(entry) => {
                    let na = entry.into_mut();
                    if **na == *networks {
                        return false;
                    }
                    *na = networks.into();
                }
            }
            true
        })
    }

    pub(crate) fn delete_network_authentication(&mut self, namespace: String, name: &str) {
        self.authn_with_all_reindexed(namespace, |authns| authns.network.remove(name).is_some())
    }

    fn authn_with_all_reindexed(
        &mut self,
        namespace: String,
        f: impl FnOnce(&mut NsAuthenticationIndex) -> bool,
    ) {
        if f(self.authentications.entry(namespace).or_default()) {
            self.namespaces.reindex_all(&self.authentications);
        }
    }

    fn ns_with_reindexed(&mut self, namespace: String, f: impl FnOnce(&mut Namespace) -> bool) {
        self.namespaces
            .ns_with_reindexed(&self.authentications, namespace, f)
    }

    fn ns_with_default_reindexed(
        &mut self,
        namespace: String,
        f: impl FnOnce(&mut Namespace) -> bool,
    ) {
        self.namespaces
            .ns_with_default_reindexed(&self.authentications, namespace, f)
    }
}

impl NamespaceIndex {
    fn get_or_default(&mut self, ns: String) -> &mut Namespace {
        self.namespaces
            .entry(ns)
            .or_insert_with(|| Namespace::new(self.cluster_info.clone()))
    }

    fn ns_with_default_reindexed(
        &mut self,
        authentications: &HashMap<String, NsAuthenticationIndex>,
        namespace: String,
        f: impl FnOnce(&mut Namespace) -> bool,
    ) {
        let ns = self
            .namespaces
            .entry(namespace.clone())
            .or_insert_with(|| Namespace::new(self.cluster_info.clone()));
        if f(ns) {
            for pod in ns.pods.values_mut() {
                pod.reindex_servers(&namespace, authentications, &ns.policy);
            }
        }
    }

    fn ns_with_reindexed(
        &mut self,
        authentications: &HashMap<String, NsAuthenticationIndex>,
        namespace: String,
        f: impl FnOnce(&mut Namespace) -> bool,
    ) {
        if let Entry::Occupied(mut ns) = self.namespaces.entry(namespace.clone()) {
            if f(ns.get_mut()) {
                if ns.get().is_empty() {
                    ns.remove();
                } else {
                    let ns = ns.into_mut();
                    for pod in ns.pods.values_mut() {
                        pod.reindex_servers(&namespace, authentications, &ns.policy);
                    }
                }
            }
        }
    }

    fn reindex_all(&mut self, authentications: &HashMap<String, NsAuthenticationIndex>) {
        for (nsname, ns) in self.namespaces.iter_mut() {
            for pod in ns.pods.values_mut() {
                pod.reindex_servers(nsname, authentications, &ns.policy);
            }
        }
    }
}

impl Namespace {
    fn new(cluster_info: Arc<ClusterInfo>) -> Self {
        Namespace {
            pods: HashMap::default(),
            policy: PolicyIndex {
                cluster_info,
                servers: HashMap::default(),
                server_authorizations: HashMap::default(),
                authorization_policies: HashMap::default(),
            },
        }
    }
    /// Returns true if the index does not include any resources.
    pub(crate) fn is_empty(&self) -> bool {
        self.pods.is_empty()
            && self.policy.servers.is_empty()
            && self.policy.server_authorizations.is_empty()
    }
}

// === impl PodIndex ===

impl PodIndex {
    /// Determines the policies for ports on this pod.
    fn reindex_servers(
        &mut self,
        namespace: &str,
        authentications: &HashMap<String, NsAuthenticationIndex>,
        policy: &PolicyIndex,
    ) {
        // Keep track of which ports were already indexed to determine whether it needs to be reset
        // to the default policy.
        let mut ports = self.port_servers.keys().copied().collect::<HashSet<_>>();

        for (srvname, server) in policy.servers.iter() {
            if server.pod_selector.matches(&self.meta.labels) {
                for port in self.select_ports(&server.port_ref).into_iter() {
                    let s = policy.mk_inbound_server(
                        namespace,
                        srvname.clone(),
                        server,
                        authentications,
                    );
                    self.update_server(port, srvname, s);
                    ports.remove(&port);
                }
            }
        }

        // Reset all remaining ports to the default policy.
        for port in ports.into_iter() {
            self.set_default_server(port, &policy.cluster_info);
        }
    }

    /// Updates a pod-port to use the given named server.
    ///
    /// The name is used explicity (and not derived from the `server` itself) to ensure that we're
    /// not handling a default server.
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

    /// Updates a pod-port to use the given named server.
    fn set_default_server(&mut self, port: u16, config: &ClusterInfo) {
        let server = Self::default_inbound_server(port, &self.meta.settings, config);
        tracing::debug!(pod = %self.name, %port, server = %config.default_policy, "Setting default server");
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
                .cloned()
                .into_iter()
                .flatten()
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
}

// === impl PolicyIndex ===

impl PolicyIndex {
    fn mk_client_authzs(
        &self,
        namespace: &str,
        server_name: &str,
        server: &Server,
        authentications: &HashMap<String, NsAuthenticationIndex>,
    ) -> HashMap<String, ClientAuthorization> {
        let mut authzs = HashMap::default();
        for (name, saz) in self.server_authorizations.iter() {
            if saz.server_selector.selects(server_name, &server.labels) {
                authzs.insert(format!("serverauthorization:{}", name), saz.authz.clone());
            }
        }

        for (name, ap) in self.authorization_policies.iter() {
            if ap.target.server() != Some(server_name) {
                continue;
            }

            let networks = ap
                .authentications
                .iter()
                .filter_map(|t| {
                    if let AuthenticationTarget::Network {
                        namespace: ns,
                        name,
                    } = t
                    {
                        let authn = authentications
                            .get(ns.as_deref().unwrap_or(namespace))?
                            .network
                            .get(name)?;
                        Some(authn.to_vec())
                    } else {
                        None
                    }
                })
                .flatten()
                .collect::<Vec<NetworkMatch>>();

            let identities = ap
                .authentications
                .iter()
                .filter_map(|t| {
                    if let AuthenticationTarget::MeshTLS {
                        namespace: ns,
                        name,
                    } = t
                    {
                        let authn = authentications
                            .get(ns.as_deref().unwrap_or(namespace))?
                            .meshtls
                            .get(name)?;
                        Some(authn.to_vec())
                    } else {
                        None
                    }
                })
                .flatten()
                .collect::<Vec<IdentityMatch>>();

            let authz = ClientAuthorization {
                networks,
                authentication: if identities.is_empty() {
                    ClientAuthentication::Unauthenticated
                } else {
                    ClientAuthentication::TlsAuthenticated(identities)
                },
            };
            authzs.insert(format!("authorizationpolicy:{}", name), authz);
        }

        authzs
    }

    fn mk_inbound_server(
        &self,
        namespace: &str,
        name: String,
        server: &Server,
        authentications: &HashMap<String, NsAuthenticationIndex>,
    ) -> InboundServer {
        let authorizations = self.mk_client_authzs(namespace, &name, server, authentications);
        InboundServer {
            name,
            authorizations,
            protocol: server.protocol.clone(),
        }
    }
}

// === impl ServerSelector ===

impl ServerSelector {
    fn selects(&self, name: &str, labels: &k8s::Labels) -> bool {
        match self {
            Self::Name(n) => *n == name,
            Self::Selector(selector) => selector.matches(labels),
        }
    }
}

// === impl AuthorizationPolicyTarget ===

impl AuthorizationPolicyTarget {
    fn server(&self) -> Option<&str> {
        match self {
            Self::Server(n) => Some(n),
        }
    }
}
