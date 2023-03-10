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

use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::{anyhow, bail, Result};
use linkerd_policy_controller_core::{
    http_route::{inbound, HttpRouteMatch, Method, PathMatch},
    AuthorizationRef, ClientAuthentication, ClientAuthorization, IdentityMatch, InboundServer,
    Ipv4Net, Ipv6Net, NetworkMatch, ServerRef,
};
use linkerd_policy_controller_k8s_api::ResourceExt;
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, num::NonZeroU16, sync::Arc};
use tokio::sync::watch;
use tracing::info_span;

mod authentication;
pub mod authorization_policy;
mod cluster_info;
mod defaults;
pub mod http_route;
mod index;
pub mod outbound_index;
mod pod;
mod server;
mod server_authorization;

#[cfg(test)]
mod tests;

pub use self::{
    cluster_info::ClusterInfo,
    defaults::DefaultPolicy,
    pod::{parse_portset, PortSet},
};
use crate::{
    authentication::AuthenticationNsIndex, http_route::InboundRouteBinding, pod::PodIndex,
};

/// Holds all indexing state. Owned and updated by a single task that processes
/// watch events, publishing results to the shared lookup map for quick lookups
/// in the API server.
///
/// Keeps track of `Pod`, `Server`, and `ServerAuthorization` resources to
/// provide a dynamic server configuration for all known ports on all pods.
///
/// The `Index` type exposes a public method: `Index::pod_server_rx`,
/// which is used to lookup pod/ports (i.e. by the gRPC API). Otherwise, it
/// implements `kubert::index::IndexNamespacedResource` for the indexed
/// kubernetes resources.
#[derive(Debug)]
pub struct Index {
    cluster_info: Arc<ClusterInfo>,
    namespaces: NamespaceIndex,
    authentications: AuthenticationNsIndex,
}

pub type SharedIndex = Arc<RwLock<Index>>;

/// Holds all `Pod`, `Server`, and `ServerAuthorization` indices by-namespace.
#[derive(Debug)]
struct NamespaceIndex {
    cluster_info: Arc<ClusterInfo>,
    by_ns: HashMap<String, Namespace>,
}

/// Holds `Pod`, `Server`, and `ServerAuthorization` indices for a single namespace.
#[derive(Debug)]
struct Namespace {
    pods: pod::PodIndex,
    policy: PolicyIndex,
}

/// Holds the state of policy resources for a single namespace.
#[derive(Debug)]
struct PolicyIndex {
    namespace: String,
    cluster_info: Arc<ClusterInfo>,

    servers: HashMap<String, server::Server>,
    server_authorizations: HashMap<String, server_authorization::ServerAuthz>,

    authorization_policies: HashMap<String, authorization_policy::Spec>,
    http_routes: HashMap<String, InboundRouteBinding>,
}

struct NsUpdate<T> {
    added: Vec<(String, T)>,
    removed: HashSet<String>,
}

// === impl Index ===

impl Index {
    pub fn shared(cluster_info: impl Into<Arc<ClusterInfo>>) -> SharedIndex {
        let cluster_info = cluster_info.into();
        Arc::new(RwLock::new(Self {
            cluster_info: cluster_info.clone(),
            namespaces: NamespaceIndex {
                cluster_info,
                by_ns: HashMap::default(),
            },
            authentications: AuthenticationNsIndex::default(),
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
        port: NonZeroU16,
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
            .watch
            .subscribe())
    }

    fn ns_with_reindex(&mut self, namespace: String, f: impl FnOnce(&mut Namespace) -> bool) {
        self.namespaces
            .get_with_reindex(namespace, &self.authentications, f)
    }

    fn ns_or_default_with_reindex(
        &mut self,
        namespace: String,
        f: impl FnOnce(&mut Namespace) -> bool,
    ) {
        self.namespaces
            .get_or_default_with_reindex(namespace, &self.authentications, f)
    }

    fn reindex_all(&mut self) {
        tracing::debug!("Reindexing all namespaces");
        for ns in self.namespaces.by_ns.values_mut() {
            ns.reindex(&self.authentications);
        }
    }

    fn apply_route<R>(&mut self, route: R)
    where
        R: ResourceExt,
        InboundRouteBinding: TryFrom<R>,
        <InboundRouteBinding as TryFrom<R>>::Error: std::fmt::Display,
    {
        let ns = route.namespace().expect("HttpRoute must have a namespace");
        let name = route.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        let route_binding = match route.try_into() {
            Ok(binding) => binding,
            Err(error) => {
                tracing::info!(%ns, %name, %error, "Ignoring HTTPRoute");
                return;
            }
        };

        self.ns_or_default_with_reindex(ns, |ns| ns.policy.update_http_route(name, route_binding))
    }

    fn reset_route<R>(&mut self, routes: Vec<R>, deleted: HashMap<String, HashSet<String>>)
    where
        R: ResourceExt,
        InboundRouteBinding: TryFrom<R>,
        <InboundRouteBinding as TryFrom<R>>::Error: std::fmt::Display,
    {
        let _span = info_span!("reset").entered();

        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        type Ns = NsUpdate<InboundRouteBinding>;
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for route in routes.into_iter() {
            let namespace = route.namespace().expect("HttpRoute must be namespaced");
            let name = route.name_unchecked();
            let route_binding = match route.try_into() {
                Ok(binding) => binding,
                Err(error) => {
                    tracing::info!(ns = %namespace, %name, %error, "Ignoring HTTPRoute");
                    continue;
                }
            };
            updates_by_ns
                .entry(namespace)
                .or_default()
                .added
                .push((name, route_binding));
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
                self.ns_with_reindex(namespace, |ns| {
                    ns.policy.http_routes.clear();
                    true
                });
            } else {
                // Otherwise, we take greater care to reindex only when the
                // state actually changed. The vast majority of resets will see
                // no actual data change.
                self.ns_or_default_with_reindex(namespace, |ns| {
                    let mut changed = !removed.is_empty();
                    for name in removed.into_iter() {
                        ns.policy.http_routes.remove(&name);
                    }
                    for (name, route_binding) in added.into_iter() {
                        changed = ns.policy.update_http_route(name, route_binding) || changed;
                    }
                    changed
                });
            }
        }
    }

    fn delete_route(&mut self, ns: String, name: String) {
        let _span = info_span!("delete", %ns, %name).entered();
        self.ns_with_reindex(ns, |ns| ns.policy.http_routes.remove(&name).is_some())
    }
}

// === impl NemspaceIndex ===

impl NamespaceIndex {
    fn get_or_default(&mut self, ns: String) -> &mut Namespace {
        self.by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace::new(ns, self.cluster_info.clone()))
    }

    /// Gets the given namespace and, if it exists, passes it to the given
    /// function. If the function returns true, all pods in the namespace are
    /// reindexed; or, if the function returns false and the namespace is empty,
    /// it is removed from the index.
    fn get_with_reindex(
        &mut self,
        namespace: String,
        authns: &AuthenticationNsIndex,
        f: impl FnOnce(&mut Namespace) -> bool,
    ) {
        if let Entry::Occupied(mut ns) = self.by_ns.entry(namespace) {
            if f(ns.get_mut()) {
                if ns.get().is_empty() {
                    ns.remove();
                } else {
                    ns.get_mut().reindex(authns);
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
        authns: &AuthenticationNsIndex,
        f: impl FnOnce(&mut Namespace) -> bool,
    ) {
        let ns = self.get_or_default(namespace);
        if f(ns) {
            ns.reindex(authns);
        }
    }
}

// === impl Namespace ===

impl Namespace {
    fn new(namespace: String, cluster_info: Arc<ClusterInfo>) -> Self {
        Namespace {
            pods: PodIndex {
                namespace: namespace.clone(),
                by_name: HashMap::default(),
            },
            policy: PolicyIndex {
                namespace,
                cluster_info,
                servers: HashMap::default(),
                server_authorizations: HashMap::default(),
                authorization_policies: HashMap::default(),
                http_routes: HashMap::default(),
            },
        }
    }

    /// Returns true if the index does not include any resources.
    #[inline]
    fn is_empty(&self) -> bool {
        self.pods.is_empty() && self.policy.is_empty()
    }

    #[inline]
    fn reindex(&mut self, authns: &AuthenticationNsIndex) {
        self.pods.reindex(&self.policy, authns);
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

    fn update_authz_policy(&mut self, name: String, spec: authorization_policy::Spec) -> bool {
        match self.authorization_policies.entry(name) {
            Entry::Vacant(entry) => {
                entry.insert(spec);
            }
            Entry::Occupied(entry) => {
                let ap = entry.into_mut();
                if *ap == spec {
                    return false;
                }
                *ap = spec;
            }
        }
        true
    }

    fn inbound_server<'p>(
        &self,
        name: String,
        server: &server::Server,
        authentications: &AuthenticationNsIndex,
        probe_paths: impl Iterator<Item = &'p str>,
    ) -> InboundServer {
        tracing::trace!(%name, ?server, "Creating inbound server");
        let authorizations = self.client_authzs(&name, server, authentications);
        let http_routes = self.http_routes(&name, authentications, probe_paths);

        InboundServer {
            reference: ServerRef::Server(name),
            authorizations,
            protocol: server.protocol.clone(),
            http_routes,
        }
    }

    fn client_authzs(
        &self,
        server_name: &str,
        server: &server::Server,
        authentications: &AuthenticationNsIndex,
    ) -> HashMap<AuthorizationRef, ClientAuthorization> {
        let mut authzs = HashMap::default();
        for (name, saz) in self.server_authorizations.iter() {
            if saz.server_selector.selects(server_name, &server.labels) {
                authzs.insert(
                    AuthorizationRef::ServerAuthorization(name.to_string()),
                    saz.authz.clone(),
                );
            }
        }

        for (name, spec) in self.authorization_policies.iter() {
            // Skip the policy if it doesn't apply to the server.
            match &spec.target {
                authorization_policy::Target::Server(name) => {
                    if name != server_name {
                        tracing::trace!(
                            ns = %self.namespace,
                            authorizationpolicy = %name,
                            server = %server_name,
                            target = %name,
                            "AuthorizationPolicy does not target server",
                        );
                        continue;
                    }
                }
                authorization_policy::Target::Namespace => {}
                authorization_policy::Target::HttpRoute(_) => {
                    // Policies which target HttpRoutes will be attached to
                    // the route authorizations and should not be included in
                    // the server authorizations.
                    continue;
                }
            }

            tracing::trace!(
                ns = %self.namespace,
                authorizationpolicy = %name,
                server = %server_name,
                "AuthorizationPolicy targets server",
            );
            tracing::trace!(authns = ?spec.authentications);

            let authz = match self.policy_client_authz(spec, authentications) {
                Ok(authz) => authz,
                Err(error) => {
                    tracing::info!(
                        server = %server_name,
                        authorizationpolicy = %name,
                        %error,
                        "Illegal AuthorizationPolicy; ignoring",
                    );
                    continue;
                }
            };

            let reference = AuthorizationRef::AuthorizationPolicy(name.to_string());
            authzs.insert(reference, authz);
        }

        authzs
    }

    fn route_client_authzs(
        &self,
        route_name: &str,
        authentications: &AuthenticationNsIndex,
    ) -> HashMap<AuthorizationRef, ClientAuthorization> {
        let mut authzs = HashMap::default();

        for (name, spec) in &self.authorization_policies {
            // Skip the policy if it doesn't apply to the route.
            match &spec.target {
                authorization_policy::Target::HttpRoute(n) if n == route_name => {}
                _ => {
                    tracing::trace!(
                        ns = %self.namespace,
                        authorizationpolicy = %name,
                        route = %route_name,
                        target = ?spec.target,
                        "AuthorizationPolicy does not target HttpRoute",
                    );
                    continue;
                }
            }

            tracing::trace!(
                ns = %self.namespace,
                authorizationpolicy = %name,
                route = %route_name,
                "AuthorizationPolicy targets HttpRoute",
            );
            tracing::trace!(authns = ?spec.authentications);

            let authz = match self.policy_client_authz(spec, authentications) {
                Ok(authz) => authz,
                Err(error) => {
                    tracing::info!(
                        route = %route_name,
                        authorizationpolicy = %name,
                        %error,
                        "Illegal AuthorizationPolicy; ignoring",
                    );
                    continue;
                }
            };

            let reference = AuthorizationRef::AuthorizationPolicy(name.to_string());
            authzs.insert(reference, authz);
        }

        authzs
    }

    fn http_routes<'p>(
        &self,
        server_name: &str,
        authentications: &AuthenticationNsIndex,
        probe_paths: impl Iterator<Item = &'p str>,
    ) -> HashMap<inbound::HttpRouteRef, inbound::HttpRoute> {
        let routes = self
            .http_routes
            .iter()
            .filter(|(_, route)| route.selects_server(server_name))
            .map(|(name, route)| {
                let mut route = route.route.clone();
                route.authorizations = self.route_client_authzs(name, authentications);
                (inbound::HttpRouteRef::Linkerd(name.clone()), route)
            })
            .collect::<HashMap<_, _>>();
        if !routes.is_empty() {
            return routes;
        }
        default_inbound_http_routes(&self.cluster_info, probe_paths)
    }

    fn policy_client_authz(
        &self,
        spec: &authorization_policy::Spec,
        all_authentications: &AuthenticationNsIndex,
    ) -> Result<ClientAuthorization> {
        use authorization_policy::AuthenticationTarget;

        let mut identities = None;
        for tgt in spec.authentications.iter() {
            match tgt {
                AuthenticationTarget::MeshTLS {
                    ref namespace,
                    ref name,
                } => {
                    let namespace = namespace.as_deref().unwrap_or(&self.namespace);
                    let _span = tracing::trace_span!("mesh_tls", ns = %namespace, %name).entered();
                    tracing::trace!("Finding MeshTLSAuthentication...");
                    let authn = all_authentications
                        .by_ns
                        .get(namespace)
                        .and_then(|ns| ns.meshtls.get(name))
                        .ok_or_else(|| {
                            anyhow!(
                                "could not find MeshTLSAuthentication {} in namespace {}",
                                name,
                                namespace
                            )
                        })?;
                    tracing::trace!(ids = ?authn.matches, "Found MeshTLSAuthentication");
                    if identities.is_some() {
                        bail!("policy must not include multiple MeshTLSAuthentications");
                    }
                    let ids = authn.matches.clone();
                    identities = Some(ids);
                }
                AuthenticationTarget::ServiceAccount {
                    ref namespace,
                    ref name,
                } => {
                    // There can only be a single required ServiceAccount. This is
                    // enforced by the admission controller.
                    if identities.is_some() {
                        bail!("policy must not include multiple ServiceAccounts");
                    }
                    let namespace = namespace.as_deref().unwrap_or(&self.namespace);
                    let id = self.cluster_info.service_account_identity(namespace, name);
                    identities = Some(vec![IdentityMatch::Exact(id)])
                }
                _network => {}
            }
        }

        let mut networks = None;
        for tgt in spec.authentications.iter() {
            if let AuthenticationTarget::Network {
                ref namespace,
                ref name,
            } = tgt
            {
                let namespace = namespace.as_deref().unwrap_or(&self.namespace);
                tracing::trace!(ns = %namespace, %name, "Finding NetworkAuthentication");
                if let Some(ns) = all_authentications.by_ns.get(namespace) {
                    if let Some(authn) = ns.network.get(name).as_ref() {
                        tracing::trace!(ns = %namespace, %name, nets = ?authn.matches, "Found NetworkAuthentication");
                        // There can only be a single required NetworkAuthentication. This is
                        // enforced by the admission controller.
                        if networks.is_some() {
                            bail!("policy must not include multiple NetworkAuthentications");
                        }
                        let nets = authn.matches.clone();
                        networks = Some(nets);
                        continue;
                    }
                }
                bail!(
                    "could not find NetworkAuthentication {} in namespace {}",
                    name,
                    namespace
                );
            }
        }

        Ok(ClientAuthorization {
            // If MTLS identities are configured, use them. Otherwise, do not require
            // authentication.
            authentication: identities
                .map(ClientAuthentication::TlsAuthenticated)
                .unwrap_or(ClientAuthentication::Unauthenticated),

            // If networks are configured, use them. Otherwise, this applies to all networks.
            networks: networks.unwrap_or_else(|| {
                vec![
                    NetworkMatch {
                        net: Ipv4Net::default().into(),
                        except: vec![],
                    },
                    NetworkMatch {
                        net: Ipv6Net::default().into(),
                        except: vec![],
                    },
                ]
            }),
        })
    }

    fn update_http_route(&mut self, name: String, route: InboundRouteBinding) -> bool {
        match self.http_routes.entry(name) {
            Entry::Vacant(entry) => {
                entry.insert(route);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() == route {
                    return false;
                }
                entry.insert(route);
            }
        }
        true
    }
}

// === imp NsUpdate ===

impl<T> Default for NsUpdate<T> {
    fn default() -> Self {
        Self {
            added: vec![],
            removed: Default::default(),
        }
    }
}

fn default_inbound_http_routes<'p>(
    cluster_info: &ClusterInfo,
    probe_paths: impl Iterator<Item = &'p str>,
) -> HashMap<inbound::HttpRouteRef, inbound::HttpRoute> {
    let mut routes = HashMap::with_capacity(2);

    // If no routes are defined for the server, use a default route that
    // matches all requests. Default authorizations are instrumented on
    // the server.
    routes.insert(
        inbound::HttpRouteRef::Default("default"),
        inbound::HttpRoute::default(),
    );

    // If there are no probe networks, there are no probe routes to
    // authorize.
    if cluster_info.probe_networks.is_empty() {
        return routes;
    }

    // Generate an `Exact` path match for each probe path defined on the
    // pod.
    let matches: Vec<HttpRouteMatch> = probe_paths
        .map(|path| HttpRouteMatch {
            path: Some(PathMatch::Exact(path.to_string())),
            headers: vec![],
            query_params: vec![],
            method: Some(Method::GET),
        })
        .collect();

    // If there are no matches, then are no probe routes to authorize.
    if matches.is_empty() {
        return routes;
    }

    // Probes are authorized on the configured probe networks only.
    let authorizations = std::iter::once((
        AuthorizationRef::Default("probe"),
        ClientAuthorization {
            networks: cluster_info
                .probe_networks
                .iter()
                .copied()
                .map(Into::into)
                .collect(),
            authentication: ClientAuthentication::Unauthenticated,
        },
    ))
    .collect();

    let probe_route = inbound::HttpRoute {
        hostnames: Vec::new(),
        rules: vec![inbound::HttpRouteRule {
            matches,
            filters: Vec::new(),
        }],
        authorizations,
        creation_timestamp: None,
    };
    routes.insert(inbound::HttpRouteRef::Default("probe"), probe_route);

    routes
}
