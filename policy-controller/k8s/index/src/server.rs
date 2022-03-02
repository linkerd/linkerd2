use crate::{authz::AuthzIndex, Index, Namespace, ServerRx, ServerTx};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use linkerd_policy_controller_core::{ClientAuthorization, InboundServer, ProxyProtocol};
use linkerd_policy_controller_k8s_api::{self as k8s, policy, ResourceExt};
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::{sync::watch, time};
use tracing::{debug, instrument, trace, warn};

/// Holds the state of all `Server`s in a namespace.
#[derive(Debug, Default)]
pub struct SrvIndex {
    index: HashMap<String, Server>,
}

/// The state of a `Server` instance and its authorizations.
#[derive(Debug)]
pub struct Server {
    /// Labels a `Server`.
    labels: k8s::Labels,

    /// Selects a port on matching pods.
    port: policy::server::Port,

    /// Selects pods by label.
    pod_selector: Arc<k8s::labels::Selector>,

    /// Indicates the server's protocol configuration.
    protocol: ProxyProtocol,

    /// Holds a copy of all authorization policies matching this server.
    authorizations: HashMap<String, ClientAuthorization>,

    /// Shares the server's state with pod-ports.
    rx: ServerRx,

    /// Broadcasts server updates to pod-port lookups.
    tx: ServerTx,
}

/// Selects servers for an authorization.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) enum ServerSelector {
    Name(String),
    Selector(Arc<k8s::labels::Selector>),
}

// === impl Index ===

impl Index {
    /// Builds a `Server`, linking it against authorizations and pod ports.
    #[instrument(
        skip(self, srv),
        fields(
            ns = ?srv.metadata.namespace,
            name = %srv.name(),
        )
    )]
    pub(crate) fn apply_server(&mut self, srv: policy::Server) {
        let ns_name = srv.namespace().expect("namespace must be set");
        let Namespace {
            ref mut pods,
            ref mut authzs,
            ref mut servers,
            default_policy: _,
        } = self.namespaces.get_or_default(ns_name);

        servers.apply(srv, authzs);

        // If we've updated the server->pod selection, then we need to re-index
        // all pods and servers.
        pods.link_servers(servers);
    }

    #[instrument(
        skip(self, srv),
        fields(
            ns = ?srv.metadata.namespace,
            name = %srv.name(),
        )
    )]
    pub(crate) fn delete_server(&mut self, srv: policy::Server) {
        self.rm_server(
            &*srv.namespace().expect("servers must be namespaced"),
            &*srv.name(),
        );
    }

    fn rm_server(&mut self, ns_name: &str, srv_name: &str) {
        let ns = match self.namespaces.index.get_mut(ns_name) {
            Some(ns) => ns,
            None => {
                warn!(name = %ns_name, "removing server from non-existent namespace");
                return;
            }
        };

        if ns.servers.index.remove(srv_name).is_none() {
            warn!(name = %srv_name, "unknown server");
        }

        // Reset the server config for all pods that were using this server.
        ns.pods.reset_server(srv_name);

        debug!("Removed server");
    }

    #[instrument(skip(self, srvs))]
    pub(crate) fn reset_servers(&mut self, srvs: Vec<policy::Server>) {
        let mut prior_servers = self
            .namespaces
            .index
            .iter()
            .map(|(n, ns)| {
                let servers = ns.servers.index.keys().cloned().collect::<HashSet<_>>();
                (n.clone(), servers)
            })
            .collect::<HashMap<_, _>>();

        for srv in srvs.into_iter() {
            let ns_name = srv.namespace().expect("namespace must be set");
            if let Some(ns) = prior_servers.get_mut(&ns_name) {
                ns.remove(srv.name().as_str());
            }

            self.apply_server(srv);
        }

        for (ns_name, ns_servers) in prior_servers.into_iter() {
            for srv_name in ns_servers.into_iter() {
                self.rm_server(ns_name.as_str(), &srv_name);
            }
        }
    }
}

// === impl SrvIndex ===

impl SrvIndex {
    pub fn iter(&self) -> impl Iterator<Item = (&String, &Server)> {
        self.index.iter()
    }

    /// Adds an authorization to servers matching `selector`.
    pub(crate) fn add_authz(
        &mut self,
        name: &str,
        selector: &ServerSelector,
        authz: ClientAuthorization,
    ) {
        for (srv_name, srv) in self.index.iter_mut() {
            if selector.selects(srv_name, &srv.labels) {
                debug!(server = %srv_name, authz = %name, "Adding authz to server");
                srv.insert_authz(name.to_string(), authz.clone());
            } else {
                debug!(server = %srv_name, authz = %name, "Removing authz from server");
                srv.remove_authz(name);
            }
        }
    }

    /// Removes an authorization by `name`.
    pub(crate) fn remove_authz(&mut self, name: &str) {
        for srv in self.index.values_mut() {
            srv.remove_authz(name);
        }
    }

    /// Iterates over servers that select the given `pod_labels`.
    pub(crate) fn iter_matching_pod(
        &self,
        pod_labels: k8s::Labels,
    ) -> impl Iterator<Item = (&str, &policy::server::Port, &ServerRx)> {
        self.index.iter().filter_map(move |(srv_name, server)| {
            let matches = server.pod_selector.matches(&pod_labels);
            trace!(server = %srv_name, %matches);
            if matches {
                Some((srv_name.as_str(), &server.port, &server.rx))
            } else {
                None
            }
        })
    }

    /// Update the index with a server instance.
    fn apply(&mut self, srv: policy::Server, ns_authzs: &AuthzIndex) {
        trace!(?srv, "Applying server");
        let srv_name = srv.name();
        let port = srv.spec.port;
        let protocol = Self::mk_protocol(srv.spec.proxy_protocol.as_ref());

        match self.index.entry(srv_name) {
            Entry::Vacant(entry) => {
                let labels = k8s::Labels::from(srv.metadata.labels);
                let authzs = ns_authzs
                    .filter_for_server(entry.key(), labels.clone())
                    .map(|(n, a)| (n, a.clone()))
                    .collect::<HashMap<_, _>>();
                debug!(authzs = ?authzs.keys());
                let (tx, rx) = watch::channel(InboundServer {
                    name: entry.key().clone(),
                    protocol: protocol.clone(),
                    authorizations: authzs.clone(),
                });
                entry.insert(Server {
                    //meta,
                    labels,
                    port,
                    pod_selector: srv.spec.pod_selector.into(),
                    protocol,

                    rx,
                    tx,
                    authorizations: authzs,
                });
            }

            Entry::Occupied(mut entry) => {
                trace!(srv = ?entry.get(), "Updating existing server");

                // If something about the server changed, we need to update the config to reflect
                // the change.
                let new_labels = if entry.get().labels != srv.metadata.labels {
                    Some(k8s::Labels::from(srv.metadata.labels))
                } else {
                    None
                };

                let new_protocol = if entry.get().protocol != protocol {
                    Some(protocol)
                } else {
                    None
                };

                trace!(?new_labels, ?new_protocol);
                if new_labels.is_some() || new_protocol.is_some() {
                    // NB: Only a single task applies index updates, so it's okay to borrow a
                    // version, modify, and send it. We don't need a lock because serialization is
                    // guaranteed.
                    let mut config = entry.get().rx.borrow().clone();

                    if let Some(labels) = new_labels {
                        let authzs = ns_authzs
                            .filter_for_server(entry.key(), labels.clone())
                            .map(|(n, a)| (n, a.clone()))
                            .collect::<HashMap<_, _>>();
                        debug!(authzs = ?authzs.keys());
                        config.authorizations = authzs.clone();
                        entry.get_mut().labels = labels;
                        entry.get_mut().authorizations = authzs;
                    }

                    if let Some(protocol) = new_protocol {
                        config.protocol = protocol.clone();
                        entry.get_mut().protocol = protocol;
                    }
                    entry
                        .get()
                        .tx
                        .send(config)
                        .expect("server update must succeed");
                }

                // If the pod/port selector didn't change, we don't need to refresh the index.
                if *entry.get().pod_selector == srv.spec.pod_selector && entry.get().port == port {
                    return;
                }

                entry.get_mut().pod_selector = srv.spec.pod_selector.into();
                entry.get_mut().port = port;
            }
        }
    }

    fn mk_protocol(p: Option<&policy::server::ProxyProtocol>) -> ProxyProtocol {
        match p {
            Some(policy::server::ProxyProtocol::Unknown) | None => ProxyProtocol::Detect {
                timeout: time::Duration::from_secs(10),
            },
            Some(policy::server::ProxyProtocol::Http1) => ProxyProtocol::Http1,
            Some(policy::server::ProxyProtocol::Http2) => ProxyProtocol::Http2,
            Some(policy::server::ProxyProtocol::Grpc) => ProxyProtocol::Grpc,
            Some(policy::server::ProxyProtocol::Opaque) => ProxyProtocol::Opaque,
            Some(policy::server::ProxyProtocol::Tls) => ProxyProtocol::Tls,
        }
    }
}

// === impl ServerSelector ===

impl ServerSelector {
    #[inline]
    fn selects(&self, srv_name: &str, srv_labels: &k8s::Labels) -> bool {
        match self {
            ServerSelector::Name(n) => n == srv_name,
            ServerSelector::Selector(s) => s.matches(srv_labels),
        }
    }
}

// === impl Server ===

impl Server {
    pub fn port(&self) -> &policy::server::Port {
        &self.port
    }

    pub fn pod_selector(&self) -> &k8s::labels::Selector {
        &*self.pod_selector
    }

    fn insert_authz(&mut self, name: impl Into<String>, authz: ClientAuthorization) {
        debug!("Adding authorization to server");
        self.authorizations.insert(name.into(), authz);
        let mut config = self.rx.borrow().clone();
        config.authorizations = self.authorizations.clone();
        self.tx.send(config).expect("config must send")
    }

    fn remove_authz(&mut self, name: &str) {
        if self.authorizations.remove(name).is_some() {
            debug!("Removing authorization from server");
            let mut config = self.rx.borrow().clone();
            config.authorizations = self.authorizations.clone();
            self.tx.send(config).expect("config must send")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::authz::AuthzIndex;
    use linkerd_policy_controller_core::ClientAuthentication;
    use linkerd_policy_controller_k8s_api::policy::server::{Port, ProxyProtocol};

    fn mk_server(
        ns: impl Into<String>,
        name: impl Into<String>,
        port: Port,
    ) -> k8s::policy::Server {
        k8s::policy::Server {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.into()),
                name: Some(name.into()),
                labels: None,
                ..Default::default()
            },
            spec: k8s::policy::ServerSpec {
                port,
                pod_selector: Default::default(),
                proxy_protocol: None,
            },
        }
    }

    fn with_proxy_protocol(mut srv: k8s::policy::Server, p: ProxyProtocol) -> k8s::policy::Server {
        srv.spec.proxy_protocol = Some(p);
        srv
    }

    fn with_srv_labels(
        mut srv: k8s::policy::Server,
        labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    ) -> k8s::policy::Server {
        srv.metadata.labels = Some(
            labels
                .into_iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect(),
        );
        srv
    }

    #[test]
    fn server_apply_update_protocol() {
        let mut idx = SrvIndex::default();
        let mut srv = {
            let srv = mk_server("ns-0", "srv-0", Port::Number(9999));
            with_proxy_protocol(srv, ProxyProtocol::Opaque)
        };
        idx.apply(srv.clone(), &AuthzIndex::default());

        srv.spec.proxy_protocol = Some(ProxyProtocol::Tls);
        idx.apply(srv.clone(), &AuthzIndex::default());

        let Server { protocol, .. } = idx.index.get("srv-0").unwrap();
        assert_eq!(
            SrvIndex::mk_protocol(srv.spec.proxy_protocol.as_ref()),
            protocol.to_owned()
        );
    }

    #[test]
    fn server_apply_update_labels() {
        let mut idx = SrvIndex::default();
        let srv = {
            let mut labels = HashMap::new();
            labels.insert("foo", "bar");
            let srv = mk_server("ns-0", "srv-0", Port::Number(9999));
            with_srv_labels(srv, labels)
        };
        idx.apply(srv.clone(), &AuthzIndex::default());

        let mut new_labels = HashMap::new();
        new_labels.insert("not-foo", "not-bar");
        let srv = with_srv_labels(srv, new_labels);
        idx.apply(srv.clone(), &AuthzIndex::default());

        let Server { labels, .. } = idx.index.get("srv-0").unwrap();
        assert_eq!(&k8s::Labels::from(srv.metadata.labels), labels);
    }

    #[test]
    fn server_add_authz_to_idx() {
        let mut idx = {
            let mut idx = SrvIndex::default();
            let srv = mk_server("ns-0", "srv-0", Port::Number(9999));
            idx.apply(srv, &AuthzIndex::default());
            idx
        };
        idx.add_authz(
            "authz-test",
            &ServerSelector::Name("srv-0".to_string()),
            ClientAuthorization {
                networks: vec![],
                authentication: ClientAuthentication::Unauthenticated,
            },
        );

        let srv = idx.index.get("srv-0").unwrap();
        assert!(
            srv.authorizations.get("authz-test").is_some(),
            "expected {} to be Some(...) got None",
            "authz-test"
        );
    }

    #[test]
    fn server_rm_authz_from_idx() {
        let mut idx = {
            let mut idx = SrvIndex::default();
            let srv = mk_server("ns-0", "srv-0", Port::Number(9999));
            idx.apply(srv, &AuthzIndex::default());
            idx
        };
        idx.add_authz(
            "authz-test",
            &ServerSelector::Name("srv-0".to_string()),
            ClientAuthorization {
                networks: vec![],
                authentication: ClientAuthentication::Unauthenticated,
            },
        );
        idx.remove_authz("authz-test");
        let srv = idx.index.get("srv-0").unwrap();
        assert!(
            srv.authorizations.get("authz-test").is_none(),
            "expected {} to be None, got: {:?}",
            "authz-test",
            srv.authorizations.get("authz-test"),
        );
    }
}
