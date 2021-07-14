use crate::{authz::AuthzIndex, Index, Namespace, ServerRx, ServerSelector, ServerTx};
use anyhow::{anyhow, bail, Result};
use linkerd_policy_controller_core::{ClientAuthorization, InboundServer, ProxyProtocol};
use linkerd_policy_controller_k8s_api::{self as k8s, policy, ResourceExt};
use std::{
    collections::{hash_map::Entry as HashEntry, BTreeMap, HashMap, HashSet},
    sync::Arc,
};
use tokio::{sync::watch, time};
use tracing::{debug, instrument, trace};

#[derive(Debug, Default)]
pub(crate) struct SrvIndex {
    index: HashMap<String, Server>,
}

#[derive(Debug)]
struct Server {
    meta: ServerMeta,
    authorizations: BTreeMap<String, ClientAuthorization>,
    rx: ServerRx,
    tx: ServerTx,
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct ServerMeta {
    labels: k8s::Labels,
    port: policy::server::Port,
    pod_selector: Arc<k8s::labels::Selector>,
    protocol: ProxyProtocol,
}

// === impl SrvIndex ===

impl SrvIndex {
    pub fn add_authz(&mut self, name: &str, selector: &ServerSelector, authz: ClientAuthorization) {
        for (srv_name, srv) in self.index.iter_mut() {
            let matches = match selector {
                ServerSelector::Name(ref n) => n == srv_name,
                ServerSelector::Selector(ref s) => s.matches(&srv.meta.labels),
            };
            if matches {
                debug!(server = %srv_name, authz = %name, "Adding authz to server");
                srv.add_authz(name.to_string(), authz.clone());
            } else {
                debug!(server = %srv_name, authz = %name, "Removing authz from server");
                srv.remove_authz(name);
            }
        }
    }

    pub fn remove_authz(&mut self, name: &str) {
        for srv in self.index.values_mut() {
            srv.remove_authz(name);
        }
    }

    pub fn iter_matching(
        &self,
        labels: k8s::Labels,
    ) -> impl Iterator<Item = (&str, &policy::server::Port, &ServerRx)> {
        self.index.iter().filter_map(move |(srv_name, server)| {
            let matches = server.meta.pod_selector.matches(&labels);
            trace!(server = %srv_name, %matches);
            if matches {
                Some((srv_name.as_str(), &server.meta.port, &server.rx))
            } else {
                None
            }
        })
    }

    /// Update the index with a server instance.
    fn apply(&mut self, srv: policy::Server, ns_authzs: &AuthzIndex) {
        let srv_name = srv.name();
        let port = srv.spec.port;
        let protocol = mk_protocol(srv.spec.proxy_protocol.as_ref());

        match self.index.entry(srv_name) {
            HashEntry::Vacant(entry) => {
                let labels = k8s::Labels::from(srv.metadata.labels);
                let authzs = ns_authzs
                    .filter_selected(entry.key(), labels.clone())
                    .map(|(n, a)| (n, a.clone()))
                    .collect::<BTreeMap<_, _>>();
                let meta = ServerMeta {
                    labels,
                    port,
                    pod_selector: srv.spec.pod_selector.into(),
                    protocol: protocol.clone(),
                };
                debug!(authzs = ?authzs.keys());
                let (tx, rx) = watch::channel(InboundServer {
                    protocol,
                    authorizations: authzs.clone(),
                });
                entry.insert(Server {
                    meta,
                    rx,
                    tx,
                    authorizations: authzs,
                });
            }

            HashEntry::Occupied(mut entry) => {
                // If something about the server changed, we need to update the config to reflect
                // the change.
                let new_labels = if entry.get().meta.labels.as_ref() != &srv.metadata.labels {
                    Some(k8s::Labels::from(srv.metadata.labels))
                } else {
                    None
                };

                let new_protocol = if entry.get().meta.protocol == protocol {
                    Some(protocol)
                } else {
                    None
                };

                trace!(?new_labels, ?new_protocol);
                if new_labels.is_some() || new_protocol.is_some() {
                    // NB: Only a single task applies server updates, so it's
                    // okay to borrow a version, modify, and send it.  We don't
                    // need a lock because serialization is guaranteed.
                    let mut config = entry.get().rx.borrow().clone();

                    if let Some(labels) = new_labels {
                        let authzs = ns_authzs
                            .filter_selected(entry.key(), labels.clone())
                            .map(|(n, a)| (n, a.clone()))
                            .collect::<BTreeMap<_, _>>();
                        debug!(authzs = ?authzs.keys());
                        config.authorizations = authzs.clone();
                        entry.get_mut().meta.labels = labels;
                        entry.get_mut().authorizations = authzs;
                    }

                    if let Some(protocol) = new_protocol {
                        config.protocol = protocol.clone();
                        entry.get_mut().meta.protocol = protocol;
                    }
                    entry
                        .get()
                        .tx
                        .send(config)
                        .expect("server update must succeed");
                }

                // If the pod/port selector didn't change, we don't need to
                // refresh the index.
                if *entry.get().meta.pod_selector == srv.spec.pod_selector
                    && entry.get().meta.port == port
                {
                    return;
                }

                entry.get_mut().meta.pod_selector = srv.spec.pod_selector.into();
                entry.get_mut().meta.port = port;
            }
        }
    }
}

// === impl Server ===

impl Server {
    fn add_authz(&mut self, name: impl Into<String>, authz: ClientAuthorization) {
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

// === impl Index ===

impl Index {
    /// Builds a `Server`, linking it against authorizations and pod ports.
    #[instrument(
        skip(self, srv),
        fields(
            ns = ?srv.metadata.namespace,
            name = ?srv.metadata.name,
        )
    )]
    pub(crate) fn apply_server(&mut self, srv: policy::Server) {
        let ns_name = srv.namespace().expect("namespace must be set");
        let Namespace {
            ref mut pods,
            ref mut authzs,
            ref mut servers,
            default_allow: _,
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
            name = ?srv.metadata.name,
        )
    )]
    pub(crate) fn delete_server(&mut self, srv: policy::Server) -> Result<()> {
        let ns_name = srv.namespace().expect("servers must be namespaced");
        self.rm_server(ns_name.as_str(), srv.name().as_str())
    }

    fn rm_server(&mut self, ns_name: &str, srv_name: &str) -> Result<()> {
        let ns =
            self.namespaces.index.get_mut(ns_name).ok_or_else(|| {
                anyhow!("removing server from non-existent namespace {}", ns_name)
            })?;

        if ns.servers.index.remove(srv_name).is_none() {
            bail!("removing non-existent server {}", srv_name);
        }

        // Reset the server config for all pods that were using this server.
        ns.pods.reset_server(srv_name);

        debug!("Removed server");
        Ok(())
    }

    #[instrument(skip(self, srvs))]
    pub(crate) fn reset_servers(&mut self, srvs: Vec<policy::Server>) -> Result<()> {
        let mut prior_servers = self
            .namespaces
            .index
            .iter()
            .map(|(n, ns)| {
                let servers = ns.servers.index.keys().cloned().collect::<HashSet<_>>();
                (n.clone(), servers)
            })
            .collect::<HashMap<_, _>>();

        let mut result = Ok(());
        for srv in srvs.into_iter() {
            let ns_name = srv.namespace().expect("namespace must be set");
            if let Some(ns) = prior_servers.get_mut(&ns_name) {
                ns.remove(srv.name().as_str());
            }

            self.apply_server(srv);
        }

        for (ns_name, ns_servers) in prior_servers.into_iter() {
            for srv_name in ns_servers.into_iter() {
                if let Err(e) = self.rm_server(ns_name.as_str(), &srv_name) {
                    result = Err(e);
                }
            }
        }

        result
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
