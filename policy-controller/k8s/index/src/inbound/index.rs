//! Keeps track of `Pod`, `Server`, and `ServerAuthorization` resources to
//! provide a dynamic server configuration for all known ports on all pods.
//!
//! The `Index` type exposes a single public method: `Index::pod_server_rx`,
//! which is used to lookup pod/ports (i.e. by the gRPC API). Otherwise, it
//! implements `kubert::index::IndexNamespacedResource` for the indexed
//! kubernetes resources.

use super::{
    authorization_policy, http_route::RouteBinding, meshtls_authentication, network_authentication,
    server, server_authorization, workload,
};
use crate::{
    http_route::{gkn_for_gateway_http_route, gkn_for_linkerd_http_route, gkn_for_resource},
    ports::{PortHasher, PortMap, PortSet},
    ClusterInfo, DefaultPolicy,
};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::{anyhow, bail, Result};
use linkerd_policy_controller_core::{
    http_route::{GroupKindName, HttpRouteMatch, Method, PathMatch},
    inbound::{
        AuthorizationRef, ClientAuthentication, ClientAuthorization, HttpRoute, HttpRouteRef,
        HttpRouteRule, InboundServer, ProxyProtocol, ServerRef,
    },
    IdentityMatch, Ipv4Net, Ipv6Net, NetworkMatch,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s, policy::server::Port, policy::server::Selector, ResourceExt,
};
use parking_lot::RwLock;
use std::{
    collections::{hash_map::Entry, BTreeSet},
    num::NonZeroU16,
    sync::Arc,
};
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
    authentications: AuthenticationNsIndex,
}

/// Holds all `Pod`, `Server`, and `ServerAuthorization` indices by-namespace.
#[derive(Debug)]
struct NamespaceIndex {
    cluster_info: Arc<ClusterInfo>,
    by_ns: HashMap<String, Namespace>,
}

/// Holds all `NetworkAuthentication` and `MeshTLSAuthentication` indices by-namespace.
///
/// This is separate from `NamespaceIndex` because authorization policies may reference
/// authentication resources across namespaces.
#[derive(Debug, Default)]
struct AuthenticationNsIndex {
    by_ns: HashMap<String, AuthenticationIndex>,
}

/// Holds `Pod`, `ExternalWorkload`, `Server`, and `ServerAuthorization` indices for a single namespace.
#[derive(Debug)]
struct Namespace {
    pods: PodIndex,
    external_workloads: ExternalWorkloadIndex,
    policy: PolicyIndex,
}

/// Holds all pod data for a single namespace.
#[derive(Debug)]
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
    meta: workload::Meta,

    /// The pod's named container ports. Used by `Server` port selectors.
    ///
    /// A pod may have multiple ports with the same name. E.g., each container
    /// may have its own `admin-http` port.
    port_names: HashMap<String, PortSet>,

    /// All known TCP server ports. This may be updated by
    /// `Namespace::reindex`--when a port is selected by a `Server`--or by
    /// `Namespace::get_pod_server` when a client discovers a port that has no
    /// configured server (and i.e. uses the default policy).
    port_servers: PortMap<WorkloadPortServer>,

    /// The pod's probe ports and their respective paths.
    ///
    /// In order for the policy controller to authorize probes, it must be
    /// aware of the probe ports and the expected paths on which probes are
    /// expected.
    probes: PortMap<BTreeSet<String>>,
}

/// Holds the state of a single port on a workload (e.g. a pod or an external
/// workload).
#[derive(Debug)]
struct WorkloadPortServer {
    /// The name of the server resource that matches this port. Unset when no
    /// server resources match this pod/port (and, i.e., the default policy is
    /// used).
    name: Option<String>,

    /// A sender used to broadcast workload port server updates.
    watch: watch::Sender<InboundServer>,
}

/// Holds all external workload data for a single namespace
#[derive(Debug)]
struct ExternalWorkloadIndex {
    namespace: String,
    by_name: HashMap<String, ExternalWorkload>,
}

/// Holds data for a single external workload, with server watches for all known
/// ports.
///
/// The set of ports / servers is updated as clients discover server
/// configuration or as `Server` resources select a port.
#[derive(Debug)]
struct ExternalWorkload {
    meta: workload::Meta,

    // The workload's named container ports. Used by `Server` port selectors.
    //
    // A workload will not have multiple ports with the same name, e.g. two
    // `admin-http` ports pointing to different numerical values.
    port_names: HashMap<String, NonZeroU16>,

    /// All known TCP server ports.
    port_servers: PortMap<WorkloadPortServer>,
}

/// Holds the state of policy resources for a single namespace.
#[derive(Debug)]
struct PolicyIndex {
    namespace: String,
    cluster_info: Arc<ClusterInfo>,

    servers: HashMap<String, server::Server>,
    server_authorizations: HashMap<String, server_authorization::ServerAuthz>,

    authorization_policies: HashMap<String, authorization_policy::Spec>,
    http_routes: HashMap<GroupKindName, RouteBinding>,
}

#[derive(Debug, Default)]
struct AuthenticationIndex {
    meshtls: HashMap<String, meshtls_authentication::Spec>,
    network: HashMap<String, network_authentication::Spec>,
}

struct NsUpdate<K, T> {
    added: Vec<(K, T)>,
    removed: HashSet<K>,
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

    /// Obtains an external_workload:port's server receiver.
    ///
    /// An error is returned if the external workload is not found. If the port
    /// is not found, a default server is created.
    pub fn external_workload_server_rx(
        &mut self,
        namespace: &str,
        workload: &str,
        port: NonZeroU16,
    ) -> Result<watch::Receiver<InboundServer>> {
        let ns = self
            .namespaces
            .by_ns
            .get_mut(namespace)
            .ok_or_else(|| anyhow::anyhow!("namespace not found: {}", namespace))?;
        let external_workload =
            ns.external_workloads
                .by_name
                .get_mut(workload)
                .ok_or_else(|| {
                    anyhow::anyhow!("external workload {}.{} not found", workload, namespace)
                })?;
        Ok(external_workload
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
        R: ResourceExt<DynamicType = ()>,
        RouteBinding: TryFrom<R>,
        <RouteBinding as TryFrom<R>>::Error: std::fmt::Display,
    {
        let ns = route.namespace().expect("HttpRoute must have a namespace");
        let name = route.name_unchecked();
        let gkn = gkn_for_resource(&route);
        let _span = info_span!("apply", %ns, %name).entered();

        let route_binding = match route.try_into() {
            Ok(binding) => binding,
            Err(error) => {
                tracing::info!(%ns, %name, %error, "Ignoring HTTPRoute");
                return;
            }
        };

        self.ns_or_default_with_reindex(ns, |ns| ns.policy.update_http_route(gkn, route_binding))
    }

    fn reset_route<R>(&mut self, routes: Vec<R>, deleted: HashMap<String, HashSet<String>>)
    where
        R: ResourceExt<DynamicType = ()>,
        RouteBinding: TryFrom<R>,
        <RouteBinding as TryFrom<R>>::Error: std::fmt::Display,
    {
        let _span = info_span!("reset").entered();

        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        type Ns = NsUpdate<GroupKindName, RouteBinding>;
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for route in routes.into_iter() {
            let namespace = route.namespace().expect("HttpRoute must be namespaced");
            let name = route.name_unchecked();
            let gkn = gkn_for_resource(&route);
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
                .push((gkn, route_binding));
        }
        for (ns, names) in deleted.into_iter() {
            let removed = names
                .into_iter()
                .map(|name| GroupKindName {
                    group: R::group(&()),
                    kind: R::kind(&()),
                    name: name.into(),
                })
                .collect();
            updates_by_ns.entry(ns).or_default().removed = removed;
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
                    for gkn in removed.into_iter() {
                        ns.policy.http_routes.remove(&gkn);
                    }
                    for (gkn, route_binding) in added.into_iter() {
                        changed = ns.policy.update_http_route(gkn, route_binding) || changed;
                    }
                    changed
                });
            }
        }
    }

    fn delete_route(&mut self, ns: String, gkn: GroupKindName) {
        let _span = info_span!("delete", %ns, route = ?gkn).entered();
        self.ns_with_reindex(ns, |ns| ns.policy.http_routes.remove(&gkn).is_some())
    }
}

impl kubert::index::IndexNamespacedResource<k8s::Pod> for Index {
    fn apply(&mut self, pod: k8s::Pod) {
        let namespace = pod.namespace().unwrap();
        let name = pod.name_unchecked();
        let _span = info_span!("apply", ns = %namespace, %name).entered();

        let port_names = pod
            .spec
            .as_ref()
            .map(workload::pod_tcp_ports_by_name)
            .unwrap_or_default();
        let probes = pod
            .spec
            .as_ref()
            .map(workload::pod_http_probes)
            .unwrap_or_default();

        let meta = workload::Meta::from_metadata(pod.metadata);

        // Add or update the pod. If the pod was not already present in the
        // index with the same metadata, index it against the policy resources,
        // updating its watches.
        let ns = self.namespaces.get_or_default(namespace);
        match ns.pods.update(name, meta, port_names, probes) {
            Ok(None) => {}
            Ok(Some(pod)) => pod.reindex_servers(&ns.policy, &self.authentications),
            Err(error) => {
                tracing::error!(%error, "Illegal pod update");
            }
        }
    }

    fn delete(&mut self, ns: String, name: String) {
        tracing::debug!(%ns, %name, "delete");
        if let Entry::Occupied(mut ns) = self.namespaces.by_ns.entry(ns) {
            // Once the pod is removed, there's nothing else to update. Any open
            // watches will complete.  No other parts of the index need to be
            // updated.
            if ns.get_mut().pods.by_name.remove(&name).is_some() && ns.get().is_empty() {
                tracing::debug!(namespace = ns.key(), "Removing empty namespace index");
                ns.remove();
            }
        }
    }

    // Since apply only reindexes a single pod at a time, there's no need to
    // handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s::external_workload::ExternalWorkload> for Index {
    fn apply(&mut self, ext_workload: k8s::external_workload::ExternalWorkload) {
        let ns = ext_workload.namespace().unwrap();
        let name = ext_workload.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        // Extract ports and settings.
        // Note: external workloads do not have any probe paths to synthesise
        // default policies for.
        let port_names = workload::external_tcp_ports_by_name(&ext_workload.spec);
        let meta = workload::Meta::from_metadata(ext_workload.metadata);

        // Add or update the workload.
        //
        // If the resource is present in the index, but its metadata has
        // changed, then it means the watches need to get an update.
        let ns = self.namespaces.get_or_default(ns);
        match ns.external_workloads.update(name, meta, port_names) {
            // No update
            Ok(None) => {}
            // Update, so re-index
            Ok(Some(workload)) => workload.reindex_servers(&ns.policy, &self.authentications),
            Err(error) => {
                tracing::error!(%error, "Illegal external workload update");
            }
        }
    }

    fn delete(&mut self, ns: String, name: String) {
        tracing::debug!(%ns, %name, "delete");
        if let Entry::Occupied(mut ns) = self.namespaces.by_ns.entry(ns) {
            // Once the external workload is removed, there's nothing else to
            // update. Any open watches will complete. No other parts of the
            // index need to be updated.
            if ns
                .get_mut()
                .external_workloads
                .by_name
                .remove(&name)
                .is_some()
                && ns.get().is_empty()
            {
                ns.remove();
            }
        }
    }

    // Since apply only reindexes a single external workload at a time, there's no need to
    // handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s::policy::Server> for Index {
    fn apply(&mut self, srv: k8s::policy::Server) {
        let ns = srv.namespace().expect("server must be namespaced");
        let name = srv.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        let server = server::Server::from_resource(srv, &self.cluster_info);
        self.ns_or_default_with_reindex(ns, |ns| ns.policy.update_server(name, server))
    }

    fn delete(&mut self, ns: String, name: String) {
        let _span = info_span!("delete", %ns, %name).entered();
        self.ns_with_reindex(ns, |ns| ns.policy.servers.remove(&name).is_some())
    }

    fn reset(&mut self, srvs: Vec<k8s::policy::Server>, deleted: HashMap<String, HashSet<String>>) {
        let _span = info_span!("reset").entered();

        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        type Ns = NsUpdate<String, server::Server>;
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for srv in srvs.into_iter() {
            let namespace = srv.namespace().expect("server must be namespaced");
            let name = srv.name_unchecked();
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
                self.ns_with_reindex(namespace, |ns| {
                    ns.policy.servers.clear();
                    true
                });
            } else {
                // Otherwise, we take greater care to reindex only when the
                // state actually changed. The vast majority of resets will see
                // no actual data change.
                self.ns_or_default_with_reindex(namespace, |ns| {
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
        let name = saz.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        match server_authorization::ServerAuthz::from_resource(saz, &self.cluster_info) {
            Ok(meta) => self.ns_or_default_with_reindex(ns, move |ns| {
                ns.policy.update_server_authz(name, meta)
            }),
            Err(error) => tracing::error!(%error, "Illegal server authorization update"),
        }
    }

    fn delete(&mut self, ns: String, name: String) {
        let _span = info_span!("delete", %ns, %name).entered();
        self.ns_with_reindex(ns, |ns| {
            ns.policy.server_authorizations.remove(&name).is_some()
        })
    }

    fn reset(
        &mut self,
        sazs: Vec<k8s::policy::ServerAuthorization>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        let _span = info_span!("reset");

        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        type Ns = NsUpdate<String, server_authorization::ServerAuthz>;
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for saz in sazs.into_iter() {
            let namespace = saz
                .namespace()
                .expect("serverauthorization must be namespaced");
            let name = saz.name_unchecked();
            match server_authorization::ServerAuthz::from_resource(saz, &self.cluster_info) {
                Ok(saz) => updates_by_ns
                    .entry(namespace)
                    .or_default()
                    .added
                    .push((name, saz)),
                Err(error) => {
                    tracing::error!(ns = %namespace, %name, %error, "Illegal server authorization update")
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
                self.ns_with_reindex(namespace, |ns| {
                    ns.policy.server_authorizations.clear();
                    true
                });
            } else {
                // Otherwise, we take greater care to reindex only when the
                // state actually changed. The vast majority of resets will see
                // no actual data change.
                self.ns_or_default_with_reindex(namespace, |ns| {
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

impl kubert::index::IndexNamespacedResource<k8s::policy::AuthorizationPolicy> for Index {
    fn apply(&mut self, policy: k8s::policy::AuthorizationPolicy) {
        let ns = policy.namespace().unwrap();
        let name = policy.name_unchecked();
        let _span = info_span!("apply", %ns, saz = %name).entered();

        let spec = match authorization_policy::Spec::try_from(policy.spec) {
            Ok(spec) => spec,
            Err(error) => {
                tracing::warn!(%error, "Invalid authorization policy");
                return;
            }
        };

        self.ns_or_default_with_reindex(ns, |ns| ns.policy.update_authz_policy(name, spec))
    }

    fn delete(&mut self, ns: String, ap: String) {
        let _span = info_span!("delete", %ns, %ap).entered();
        tracing::trace!(name = %ap, "Delete");
        self.ns_with_reindex(ns, |ns| {
            ns.policy.authorization_policies.remove(&ap).is_some()
        })
    }

    fn reset(
        &mut self,
        policies: Vec<k8s::policy::AuthorizationPolicy>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        let _span = info_span!("reset");

        tracing::trace!(?deleted, ?policies, "Reset");
        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        type Ns = NsUpdate<String, authorization_policy::Spec>;
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for policy in policies.into_iter() {
            let namespace = policy
                .namespace()
                .expect("authorizationpolicy must be namespaced");
            let name = policy.name_unchecked();
            match authorization_policy::Spec::try_from(policy.spec) {
                Ok(spec) => updates_by_ns
                    .entry(namespace)
                    .or_default()
                    .added
                    .push((name, spec)),
                Err(error) => {
                    tracing::error!(ns = %namespace, %name, %error, "Illegal server authorization update")
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
                self.ns_with_reindex(namespace, |ns| {
                    ns.policy.authorization_policies.clear();
                    true
                });
            } else {
                // Otherwise, we take greater care to reindex only when the
                // state actually changed. The vast majority of resets will see
                // no actual data change.
                self.ns_or_default_with_reindex(namespace, |ns| {
                    let mut changed = !removed.is_empty();
                    for name in removed.into_iter() {
                        ns.policy.authorization_policies.remove(&name);
                    }
                    for (name, spec) in added.into_iter() {
                        changed = ns.policy.update_authz_policy(name, spec) || changed;
                    }
                    changed
                });
            }
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s::policy::MeshTLSAuthentication> for Index {
    fn apply(&mut self, authn: k8s::policy::MeshTLSAuthentication) {
        let ns = authn
            .namespace()
            .expect("MeshTLSAuthentication must have a namespace");
        let name = authn.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        let spec = match meshtls_authentication::Spec::try_from_resource(authn, &self.cluster_info)
        {
            Ok(spec) => spec,
            Err(error) => {
                tracing::warn!(%error, "Invalid MeshTLSAuthentication");
                return;
            }
        };

        if self.authentications.update_meshtls(ns, name, spec) {
            self.reindex_all();
        }
    }

    fn delete(&mut self, ns: String, name: String) {
        let _span = info_span!("delete", %ns, %name).entered();

        if let Entry::Occupied(mut ns) = self.authentications.by_ns.entry(ns) {
            tracing::debug!("Deleting MeshTLSAuthentication");
            ns.get_mut().network.remove(&name);
            if ns.get().is_empty() {
                ns.remove();
            }
            self.reindex_all();
        } else {
            tracing::warn!("Namespace already deleted!");
        }
    }

    fn reset(
        &mut self,
        authns: Vec<k8s::policy::MeshTLSAuthentication>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        let _span = info_span!("reset");

        let mut changed = false;

        for authn in authns.into_iter() {
            let namespace = authn
                .namespace()
                .expect("meshtlsauthentication must be namespaced");
            let name = authn.name_unchecked();
            let spec = match meshtls_authentication::Spec::try_from_resource(
                authn,
                &self.cluster_info,
            ) {
                Ok(spec) => spec,
                Err(error) => {
                    tracing::warn!(ns = %namespace, %name, %error, "Invalid MeshTLSAuthentication");
                    continue;
                }
            };
            changed = self.authentications.update_meshtls(namespace, name, spec) || changed;
        }
        for (namespace, names) in deleted.into_iter() {
            if let Entry::Occupied(mut ns) = self.authentications.by_ns.entry(namespace) {
                for name in names.into_iter() {
                    ns.get_mut().meshtls.remove(&name);
                }
                if ns.get().is_empty() {
                    ns.remove();
                }
            }
        }

        if changed {
            self.reindex_all();
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s::policy::NetworkAuthentication> for Index {
    fn apply(&mut self, authn: k8s::policy::NetworkAuthentication) {
        let ns = authn.namespace().unwrap();
        let name = authn.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        let spec = match network_authentication::Spec::try_from(authn.spec) {
            Ok(spec) => spec,
            Err(error) => {
                tracing::warn!(%error, "Invalid NetworkAuthentication");
                return;
            }
        };

        if self.authentications.update_network(ns, name, spec) {
            self.reindex_all();
        }
    }

    fn delete(&mut self, ns: String, name: String) {
        let _span = info_span!("delete", %ns, %name).entered();

        if let Entry::Occupied(mut ns) = self.authentications.by_ns.entry(ns) {
            tracing::debug!("Deleting MeshTLSAuthentication");

            ns.get_mut().network.remove(&name);
            if ns.get().is_empty() {
                ns.remove();
            }
            self.reindex_all();
        } else {
            tracing::warn!("Namespace already deleted!");
        }
    }

    fn reset(
        &mut self,
        authns: Vec<k8s::policy::NetworkAuthentication>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        let _span = info_span!("reset");

        let mut changed = false;

        for authn in authns.into_iter() {
            let namespace = authn
                .namespace()
                .expect("meshtlsauthentication must be namespaced");
            let name = authn.name_unchecked();
            let spec = match network_authentication::Spec::try_from(authn.spec) {
                Ok(spec) => spec,
                Err(error) => {
                    tracing::warn!(ns = %namespace, %name, %error, "Invalid NetworkAuthentication");
                    return;
                }
            };
            changed = self.authentications.update_network(namespace, name, spec) || changed;
        }
        for (namespace, names) in deleted.into_iter() {
            if let Entry::Occupied(mut ns) = self.authentications.by_ns.entry(namespace) {
                for name in names.into_iter() {
                    ns.get_mut().meshtls.remove(&name);
                }
                if ns.get().is_empty() {
                    ns.remove();
                }
            }
        }

        if changed {
            self.reindex_all();
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s::policy::HttpRoute> for Index {
    fn apply(&mut self, route: k8s::policy::HttpRoute) {
        self.apply_route(route)
    }

    fn delete(&mut self, ns: String, name: String) {
        let gkn = gkn_for_linkerd_http_route(name);
        self.delete_route(ns, gkn)
    }

    fn reset(
        &mut self,
        routes: Vec<k8s::policy::HttpRoute>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        self.reset_route(routes, deleted)
    }
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::HttpRoute> for Index {
    fn apply(&mut self, route: k8s_gateway_api::HttpRoute) {
        self.apply_route(route)
    }

    fn delete(&mut self, ns: String, name: String) {
        let gkn = gkn_for_gateway_http_route(name);
        self.delete_route(ns, gkn)
    }

    fn reset(
        &mut self,
        routes: Vec<k8s_gateway_api::HttpRoute>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        self.reset_route(routes, deleted)
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
                    tracing::debug!(namespace = ns.key(), "Removing empty namespace index");
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
            external_workloads: ExternalWorkloadIndex {
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
        self.pods.is_empty() && self.policy.is_empty() && self.external_workloads.is_empty()
    }

    #[inline]
    fn reindex(&mut self, authns: &AuthenticationNsIndex) {
        self.pods.reindex(&self.policy, authns);
        self.external_workloads.reindex(&self.policy, authns);
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
        meta: workload::Meta,
        port_names: HashMap<String, PortSet>,
        probes: PortMap<BTreeSet<String>>,
    ) -> Result<Option<&mut Pod>> {
        let pod = match self.by_name.entry(name.clone()) {
            Entry::Vacant(entry) => entry.insert(Pod {
                meta,
                port_names,
                port_servers: PortMap::default(),
                probes,
            }),

            Entry::Occupied(entry) => {
                let pod = entry.into_mut();

                // Pod labels and annotations may change at runtime, but the
                // port list may not
                if pod.port_names != port_names {
                    bail!("pod {} port names must not change", name);
                }

                // If there aren't meaningful changes, then don't bother doing
                // any more work.
                if pod.meta == meta {
                    tracing::debug!(pod = %name, "No changes");
                    return Ok(None);
                }
                tracing::debug!(pod = %name, "Updating");
                pod.meta = meta;
                pod
            }
        };
        Ok(Some(pod))
    }

    fn reindex(&mut self, policy: &PolicyIndex, authns: &AuthenticationNsIndex) {
        let _span = info_span!("reindex", ns = %self.namespace).entered();
        for (name, pod) in self.by_name.iter_mut() {
            let _span = info_span!("pod", pod = %name).entered();
            pod.reindex_servers(policy, authns);
        }
    }
}

// === impl Pod ===

impl Pod {
    /// Determines the policies for ports on this pod.
    fn reindex_servers(&mut self, policy: &PolicyIndex, authentications: &AuthenticationNsIndex) {
        // Keep track of the ports that are already known in the pod so that, after applying server
        // matches, we can ensure remaining ports are set to the default policy.
        let mut unmatched_ports = self.port_servers.keys().copied().collect::<PortSet>();

        // Keep track of which ports have been matched to servers to that we can detect when
        // multiple servers match a single port.
        //
        // We start with capacity for the known ports on the pod; but this can grow if servers
        // select additional ports.
        let mut matched_ports = PortMap::with_capacity_and_hasher(
            unmatched_ports.len(),
            std::hash::BuildHasherDefault::<PortHasher>::default(),
        );

        for (srvname, server) in policy.servers.iter() {
            if let Selector::Pod(pod_selector) = &server.selector {
                if pod_selector.matches(&self.meta.labels) {
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

                        let s = policy.inbound_server(
                            srvname.clone(),
                            server,
                            authentications,
                            self.probes
                                .get(&port)
                                .into_iter()
                                .flatten()
                                .map(|p| p.as_str()),
                        );
                        self.update_server(port, srvname, s);

                        matched_ports.insert(port, srvname.clone());
                        unmatched_ports.remove(&port);
                    }
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
    fn update_server(&mut self, port: NonZeroU16, name: &str, server: InboundServer) {
        match self.port_servers.entry(port) {
            Entry::Vacant(entry) => {
                tracing::trace!(port = %port, server = %name, "Creating server");
                let (watch, _) = watch::channel(server);
                entry.insert(WorkloadPortServer {
                    name: Some(name.to_string()),
                    watch,
                });
            }

            Entry::Occupied(mut entry) => {
                let ps = entry.get_mut();

                ps.watch.send_if_modified(|current| {
                    if ps.name.as_deref() == Some(name) && *current == server {
                        tracing::trace!(port = %port, server = %name, "Skipped redundant server update");
                        tracing::trace!(?server);
                        return false;
                    }

                    // If the port's server previously matched a different server,
                    // this can either mean that multiple servers currently match
                    // the pod:port, or that we're in the middle of an update. We
                    // make the opportunistic choice to assume the cluster is
                    // configured coherently so we take the update. The admission
                    // controller should prevent conflicts.
                    tracing::trace!(port = %port, server = %name, "Updating server");
                    if ps.name.as_deref() != Some(name) {
                        ps.name = Some(name.to_string());
                    }

                    *current = server;
                    true
                });
            }
        }

        tracing::debug!(port = %port, server = %name, "Updated server");
    }

    /// Updates a pod-port to use the given named server.
    fn set_default_server(&mut self, port: NonZeroU16, config: &ClusterInfo) {
        let server = PolicyIndex::default_inbound_server(
            port,
            &self.meta.settings,
            self.probes
                .get(&port)
                .into_iter()
                .flatten()
                .map(|p| p.as_str()),
            config,
        );
        match self.port_servers.entry(port) {
            Entry::Vacant(entry) => {
                tracing::debug!(%port, server = %config.default_policy, "Creating default server");
                let (watch, _) = watch::channel(server);
                entry.insert(WorkloadPortServer { name: None, watch });
            }

            Entry::Occupied(mut entry) => {
                let ps = entry.get_mut();
                ps.watch.send_if_modified(|current| {
                    // Avoid sending redundant updates.
                    if *current == server {
                        tracing::trace!(%port, server = %config.default_policy, "Default server already set");
                        return false;
                    }

                    tracing::debug!(%port, server = %config.default_policy, "Setting default server");
                    ps.name = None;
                    *current = server;
                    true
                });
            }
        }
    }

    /// Enumerates ports.
    ///
    /// A named port may refer to an arbitrary number of port numbers.
    fn select_ports(&mut self, port_ref: &Port) -> Vec<NonZeroU16> {
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

    fn port_server_or_default(
        &mut self,
        port: NonZeroU16,
        config: &ClusterInfo,
    ) -> &mut WorkloadPortServer {
        match self.port_servers.entry(port) {
            Entry::Occupied(entry) => entry.into_mut(),
            Entry::Vacant(entry) => {
                let (watch, _) = watch::channel(PolicyIndex::default_inbound_server(
                    port,
                    &self.meta.settings,
                    self.probes
                        .get(&port)
                        .into_iter()
                        .flatten()
                        .map(|p| p.as_str()),
                    config,
                ));
                entry.insert(WorkloadPortServer { name: None, watch })
            }
        }
    }
}

// === impl ExternalWorkloadIndex ===

impl ExternalWorkloadIndex {
    #[inline]
    fn is_empty(&self) -> bool {
        self.by_name.is_empty()
    }

    /// Indexes an external workload resource and computes any changes in the
    /// workload's state.
    ///
    /// If the workload is indexed for the first time, or some of its settings
    /// have changed (e.g. metadata) then indexing will trigger a namespace-wide
    /// server re-index to push new state to the clients.
    ///
    /// Otherwise, if nothing has changed, do not trigger re-indexing.
    fn update(
        &mut self,
        name: String,
        meta: workload::Meta,
        port_names: HashMap<String, NonZeroU16>,
    ) -> Result<Option<&mut ExternalWorkload>> {
        let workload = match self.by_name.entry(name.clone()) {
            Entry::Vacant(entry) => entry.insert(ExternalWorkload {
                meta,
                port_names,
                port_servers: PortMap::default(),
            }),
            Entry::Occupied(entry) => {
                let workload = entry.into_mut();

                if workload.meta == meta && workload.port_names == port_names {
                    tracing::debug!(external_workload = %name, "No changes");
                    return Ok(None);
                }

                if workload.meta != meta {
                    tracing::trace!(external_workload = %name, "Updating workload's metadata");
                    workload.meta = meta;
                }

                if workload.port_names != port_names {
                    tracing::trace!(external_workload = %name, "Updating workload's ports");
                    workload.port_names = port_names;
                }

                tracing::debug!(external_workload = %name, "Updating");
                workload
            }
        };
        Ok(Some(workload))
    }

    /// For each external workload in a namespace, re-compute the server and
    /// authorization policy states to determine if new changes need to be
    /// pushed to clients.
    fn reindex(&mut self, policy: &PolicyIndex, authns: &AuthenticationNsIndex) {
        let _span = info_span!("reindex", ns = %self.namespace).entered();
        for (name, ext_workload) in self.by_name.iter_mut() {
            let _span = info_span!("external_workload", external_workload = %name).entered();
            ext_workload.reindex_servers(policy, authns);
        }
    }
}

//
impl ExternalWorkload {
    /// Determines the policies for ports on this workload
    fn reindex_servers(&mut self, policy: &PolicyIndex, authentications: &AuthenticationNsIndex) {
        // Keep track of ports that are already known so that they may receive
        // default policies if they are still not selected by a server.
        //
        // TODO (matei): we could eagerly fill this out if we require users to
        // always document a workload's ports. i.e. right now a non-named port
        // will be lazily discovered only when traffic is sent, we could eagerly
        // set policies for it (but I feel like for now, this should be similar in
        // behaviour to a pod)
        //
        let mut unmatched_ports = self.port_servers.keys().copied().collect::<PortSet>();

        // Keep track of which ports have been matched with servers so that we
        // can detect when more than one server matches a single port.
        let mut matched_ports = PortMap::with_capacity_and_hasher(
            unmatched_ports.len(),
            std::hash::BuildHasherDefault::<PortHasher>::default(),
        );

        for (srvname, server) in policy.servers.iter() {
            if let Selector::ExternalWorkload(selector) = &server.selector {
                if selector.matches(&self.meta.labels) {
                    // Each server selects exactly one port on an
                    // external workload
                    //
                    // Note: an external workload has only one set of ports. A
                    // pod has a union, each container declares its own set.
                    let port = if let Some(srv_port) = self.selects_port(&server.port_ref) {
                        srv_port
                    } else {
                        // If server references a named port, and our workload
                        // contains no such port, then skip this server.
                        continue;
                    };

                    if let Some(prior) = matched_ports.get(&port) {
                        // If a different server has already matched this
                        tracing::warn!(
                        %port,
                        server = %prior,
                        conflict = %srvname,
                        "Port already matched by another server; skipping"
                        );
                        continue;
                    }

                    let s = policy.inbound_server(
                        srvname.clone(),
                        server,
                        authentications,
                        Vec::new().into_iter(),
                    );

                    self.update_server(port, srvname, s);
                    matched_ports.insert(port, srvname.clone());
                    unmatched_ports.remove(&port);
                }
            }
        }

        // Reset all other ports that were previously selected to defaults
        for port in unmatched_ports.into_iter() {
            self.set_default_server(port, &policy.cluster_info);
        }
    }

    /// Updates an external workload-port to use a given named server.
    ///
    /// We use name explicitly (and not derived from the 'server') to ensure we
    /// are not handling a default server.
    fn update_server(&mut self, port: NonZeroU16, name: &str, server: InboundServer) {
        match self.port_servers.entry(port) {
            Entry::Vacant(entry) => {
                tracing::trace!(port = %port, server = %name, "Creating server");
                let (watch, _) = watch::channel(server);
                entry.insert(WorkloadPortServer {
                    name: Some(name.to_string()),
                    watch,
                });
            }

            Entry::Occupied(mut entry) => {
                let ps = entry.get_mut();

                ps.watch.send_if_modified(|current| {
                    if ps.name.as_deref() == Some(name) && *current == server {
                        tracing::trace!(port = %port, server = %name, "Skipped redundant server update");
                        tracing::trace!(?server);
                        return false;
                    }

                    // If the port's server previously matched a different
                    // server, this can either mean that multiple servers
                    // currently match the external_workload:port, or that we're
                    // in the middle of an update. We make the opportunistic
                    // choice to assume the cluster is configured coherently so
                    // we take the update. The admission controller should
                    // prevent conflicts.
                    tracing::trace!(port = %port, server = %name, "Updating server");
                    if ps.name.as_deref() != Some(name) {
                        ps.name = Some(name.to_string());
                    }

                    *current = server;
                    true
                });
            }
        }

        tracing::debug!(port = %port, server = %name, "Updated server");
    }

    /// Updates a workload-port to use a given named server.
    fn set_default_server(&mut self, port: NonZeroU16, config: &ClusterInfo) {
        // Create a default server policy, without authorising any probe paths
        let server = PolicyIndex::default_inbound_server(
            port,
            &self.meta.settings,
            Vec::new().into_iter(),
            config,
        );
        match self.port_servers.entry(port) {
            Entry::Vacant(entry) => {
                tracing::debug!(%port, server = %config.default_policy, "Creating default server");
                let (watch, _) = watch::channel(server);
                entry.insert(WorkloadPortServer { name: None, watch });
            }

            Entry::Occupied(mut entry) => {
                // A server must have selected this before; override with
                // default policy
                let ps = entry.get_mut();
                ps.watch.send_if_modified(|current| {
                    // Avoid sending redundant updates
                    if *current == server {
                        tracing::trace!(%port, server = %config.default_policy, "Default server already set");
                        return false;
                    }

                    tracing::debug!(%port, server = %config.default_policy, "Setting default server");
                    ps.name = None;
                    *current = server;
                    true
                });
            }
        }
    }

    /// Returns an optional port when a server references a known external
    /// workload port.
    ///
    /// Unlike a pod, an external workload has only one set of ports. Names
    /// within the set are unique, and as a result, only one port will ever
    /// match a given name.
    fn selects_port(&mut self, port_ref: &Port) -> Option<NonZeroU16> {
        match port_ref {
            Port::Number(p) => Some(*p),
            Port::Name(name) => self.port_names.get(name).cloned(),
        }
    }

    fn port_server_or_default(
        &mut self,
        port: NonZeroU16,
        config: &ClusterInfo,
    ) -> &mut WorkloadPortServer {
        match self.port_servers.entry(port) {
            Entry::Occupied(entry) => entry.into_mut(),
            Entry::Vacant(entry) => {
                let (watch, _) = watch::channel(PolicyIndex::default_inbound_server(
                    port,
                    &self.meta.settings,
                    Vec::new().into_iter(),
                    config,
                ));
                entry.insert(WorkloadPortServer { name: None, watch })
            }
        }
    }
}

// === impl PolicyIndex ===

impl PolicyIndex {
    #[inline]
    fn is_empty(&self) -> bool {
        self.servers.is_empty()
            && self.server_authorizations.is_empty()
            && self.authorization_policies.is_empty()
            && self.http_routes.is_empty()
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

    fn default_inbound_server<'p>(
        port: NonZeroU16,
        settings: &workload::Settings,
        probe_paths: impl Iterator<Item = &'p str>,
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

        let authorizations = policy.default_authzs(config);

        let http_routes = config.default_inbound_http_routes(probe_paths);

        InboundServer {
            reference: ServerRef::Default(policy.as_str()),
            protocol,
            authorizations,
            http_routes,
        }
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
        gkn: &GroupKindName,
        authentications: &AuthenticationNsIndex,
    ) -> HashMap<AuthorizationRef, ClientAuthorization> {
        let mut authzs = HashMap::default();

        for (name, spec) in &self.authorization_policies {
            // Skip the policy if it doesn't apply to the route.
            match &spec.target {
                authorization_policy::Target::HttpRoute(n) if n.eq_ignore_ascii_case(gkn) => {}
                _ => {
                    tracing::trace!(
                        ns = %self.namespace,
                        authorizationpolicy = %name,
                        route = ?gkn,
                        target = ?spec.target,
                        "AuthorizationPolicy does not target HttpRoute",
                    );
                    continue;
                }
            }

            tracing::trace!(
                ns = %self.namespace,
                authorizationpolicy = %name,
                route = ?gkn,
                "AuthorizationPolicy targets HttpRoute",
            );
            tracing::trace!(authns = ?spec.authentications);

            let authz = match self.policy_client_authz(spec, authentications) {
                Ok(authz) => authz,
                Err(error) => {
                    tracing::info!(
                        route = ?gkn,
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
    ) -> HashMap<HttpRouteRef, HttpRoute> {
        let routes = self
            .http_routes
            .iter()
            .filter(|(_, route)| route.selects_server(server_name))
            .filter(|(_, route)| route.accepted_by_server(server_name))
            .map(|(gkn, route)| {
                let mut route = route.route.clone();
                route.authorizations = self.route_client_authzs(gkn, authentications);
                (HttpRouteRef::Linkerd(gkn.clone()), route)
            })
            .collect::<HashMap<_, _>>();
        if !routes.is_empty() {
            return routes;
        }
        self.cluster_info.default_inbound_http_routes(probe_paths)
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

    fn update_http_route(&mut self, gkn: GroupKindName, route: RouteBinding) -> bool {
        match self.http_routes.entry(gkn) {
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

// === impl AuthenticationNsIndex ===

impl AuthenticationNsIndex {
    fn update_meshtls(
        &mut self,
        namespace: String,
        name: String,
        spec: meshtls_authentication::Spec,
    ) -> bool {
        match self.by_ns.entry(namespace).or_default().meshtls.entry(name) {
            Entry::Vacant(entry) => {
                entry.insert(spec);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() == spec {
                    return false;
                }
                entry.insert(spec);
            }
        }

        true
    }

    fn update_network(
        &mut self,
        namespace: String,
        name: String,
        spec: network_authentication::Spec,
    ) -> bool {
        match self.by_ns.entry(namespace).or_default().network.entry(name) {
            Entry::Vacant(entry) => {
                entry.insert(spec);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() == spec {
                    return false;
                }
                entry.insert(spec);
            }
        }

        true
    }
}

// === impl AuthenticationIndex ===

impl AuthenticationIndex {
    #[inline]
    fn is_empty(&self) -> bool {
        self.meshtls.is_empty() && self.network.is_empty()
    }
}

// === imp NsUpdate ===

impl<K, T> Default for NsUpdate<K, T> {
    fn default() -> Self {
        Self {
            added: vec![],
            removed: Default::default(),
        }
    }
}

// === impl ClusterInfo ===

impl ClusterInfo {
    fn default_inbound_http_routes<'p>(
        &self,
        probe_paths: impl Iterator<Item = &'p str>,
    ) -> HashMap<HttpRouteRef, HttpRoute> {
        let mut routes = HashMap::with_capacity(2);

        // If no routes are defined for the server, use a default route that
        // matches all requests. Default authorizations are instrumented on
        // the server.
        routes.insert(HttpRouteRef::Default("default"), HttpRoute::default());

        // If there are no probe networks, there are no probe routes to
        // authorize.
        if self.probe_networks.is_empty() {
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
                networks: self
                    .probe_networks
                    .iter()
                    .copied()
                    .map(Into::into)
                    .collect(),
                authentication: ClientAuthentication::Unauthenticated,
            },
        ))
        .collect();

        let probe_route = HttpRoute {
            hostnames: Vec::new(),
            rules: vec![HttpRouteRule {
                matches,
                filters: Vec::new(),
            }],
            authorizations,
            creation_timestamp: None,
        };
        routes.insert(HttpRouteRef::Default("probe"), probe_route);

        routes
    }
}
