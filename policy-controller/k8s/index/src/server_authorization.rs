use crate::{server::ServerSelector, ClusterInfo, Index, SharedIndex, SrvIndex};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::{anyhow, bail, Result};
use futures::prelude::*;
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, NetworkMatch,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{self, server_authorization::MeshTls},
    ResourceExt,
};
use std::collections::hash_map::Entry;
use tracing::{debug, instrument, trace, warn};

/// Indexes `ServerAuthorization` resources within a namespace.
#[derive(Debug, Default)]
pub struct AuthzIndex {
    index: HashMap<String, Authz>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct Authz {
    /// Selects `Server` instances in the same namespace.
    servers: ServerSelector,

    /// The current authorization policy to apply.
    clients: ClientAuthorization,
}

#[instrument(skip_all, name = "serverauthorizations")]
pub async fn index(
    idx: SharedIndex,
    events: impl Stream<Item = k8s::WatchEvent<k8s::policy::ServerAuthorization>>,
) {
    tokio::pin!(events);
    while let Some(ev) = events.next().await {
        match ev {
            k8s::WatchEvent::Applied(saz) => apply(&mut *idx.write(), saz),
            k8s::WatchEvent::Deleted(saz) => delete(&mut *idx.write(), saz),
            k8s::WatchEvent::Restarted(sazs) => restart(&mut *idx.write(), sazs),
        }
    }
}

/// Obtains or constructs an `Authz` and links it to the appropriate `Servers`.
#[instrument(skip_all, fields(
    ns = ?authz.metadata.namespace,
    name = %authz.name(),
))]
fn apply(index: &mut Index, authz: policy::ServerAuthorization) {
    let ns = index
        .namespaces
        .get_or_default(authz.namespace().expect("namespace required"));

    ns.authzs.apply(authz, &mut ns.servers, &index.cluster_info)
}

#[instrument(skip_all, fields(
    ns = ?authz.metadata.namespace,
    name = %authz.name(),
))]
fn delete(index: &mut Index, authz: policy::ServerAuthorization) {
    if let Some(ns) = index
        .namespaces
        .index
        .get_mut(authz.namespace().unwrap().as_str())
    {
        let name = authz.name();
        ns.servers.remove_server_authz(name.as_str());
        ns.authzs.delete(name.as_str());
    }
}

#[instrument(skip_all)]
fn restart(index: &mut Index, authzs: Vec<policy::ServerAuthorization>) {
    let mut prior = index
        .namespaces
        .index
        .iter()
        .map(|(n, ns)| {
            let authzs = ns.authzs.index.keys().cloned().collect::<HashSet<_>>();
            (n.clone(), authzs)
        })
        .collect::<HashMap<_, _>>();

    for authz in authzs.into_iter() {
        if let Some(ns) = prior.get_mut(authz.namespace().unwrap().as_str()) {
            ns.remove(authz.name().as_str());
        }

        apply(index, authz);
    }

    for (ns_name, authzs) in prior {
        if let Some(ns) = index.namespaces.index.get_mut(&ns_name) {
            for name in authzs.into_iter() {
                ns.servers.remove_server_authz(&name);
                ns.authzs.delete(&name);
            }
        }
    }
}

// === impl AuthzIndex ===

impl AuthzIndex {
    /// Enumerates authorizations in this namespace matching either the given server name or its
    /// labels.
    pub(crate) fn filter_for_server(
        &self,
        server_name: impl Into<String>,
        server_labels: k8s::Labels,
    ) -> impl Iterator<Item = (String, &ClientAuthorization)> {
        let server_name = server_name.into();
        self.index.iter().filter_map(move |(authz_name, a)| {
            let matches = match a.servers {
                ServerSelector::Name(ref n) => {
                    trace!(selector.name = %n, server.name = %server_name);
                    n == &server_name
                }
                ServerSelector::Selector(ref s) => {
                    trace!(selector = ?s, ?server_labels);
                    s.matches(&server_labels)
                }
            };
            debug!(authz = %authz_name, %matches);
            if matches {
                Some((authz_name.clone(), &a.clients))
            } else {
                None
            }
        })
    }

    /// Updates the authorization and server indexes with a new or updated authorization instance.
    fn apply(
        &mut self,
        authz: policy::ServerAuthorization,
        servers: &mut SrvIndex,
        cluster: &ClusterInfo,
    ) {
        let name = authz.name();
        let authz = match mk_server_authz(authz, cluster) {
            Ok(authz) => authz,
            Err(error) => {
                warn!(saz = %name, %error);
                return;
            }
        };

        match self.index.entry(name) {
            Entry::Vacant(entry) => {
                servers.add_server_authz(entry.key(), &authz.servers, authz.clients.clone());
                entry.insert(authz);
            }

            Entry::Occupied(mut entry) => {
                // If the authorization changed materially, then update it in all servers.
                if entry.get() != &authz {
                    servers.add_server_authz(entry.key(), &authz.servers, authz.clients.clone());
                    entry.insert(authz);
                }
            }
        }
    }

    fn delete(&mut self, name: &str) {
        self.index.remove(name);
        debug!("Removed authz");
    }
}

fn mk_server_authz(
    srv: policy::server_authorization::ServerAuthorization,
    cluster: &ClusterInfo,
) -> Result<Authz> {
    let policy::server_authorization::ServerAuthorization { metadata, spec, .. } = srv;

    let servers = {
        let policy::server_authorization::Server { name, selector } = spec.server;
        match (name, selector) {
            (Some(n), None) => ServerSelector::Name(n),
            (None, Some(sel)) => ServerSelector::Selector(sel.into()),
            (Some(_), Some(_)) => bail!("authorization selection is ambiguous"),
            (None, None) => bail!("authorization selects no servers"),
        }
    };

    let networks = if let Some(nets) = spec.client.networks {
        nets.into_iter()
            .map(|net| {
                debug!(net = %net.cidr, "Unauthenticated");
                Ok(NetworkMatch {
                    net: net.cidr,
                    except: net.except.unwrap_or_default(),
                })
            })
            .collect::<Result<Vec<NetworkMatch>>>()?
    } else {
        // If no networks are specified, the cluster networks are used as the default.
        cluster
            .networks
            .iter()
            .copied()
            .map(NetworkMatch::from)
            .collect()
    };

    let authentication = if spec.client.unauthenticated {
        ClientAuthentication::Unauthenticated
    } else {
        let mtls = spec
            .client
            .mesh_tls
            .ok_or_else(|| anyhow!("client mtls missing"))?;
        mk_mtls_authn(&metadata, mtls, cluster)?
    };

    Ok(Authz {
        servers,
        clients: ClientAuthorization {
            networks,
            authentication,
        },
    })
}

fn mk_mtls_authn(
    metadata: &k8s::ObjectMeta,
    mtls: MeshTls,
    cluster: &ClusterInfo,
) -> Result<ClientAuthentication> {
    if mtls.unauthenticated_tls {
        return Ok(ClientAuthentication::TlsUnauthenticated);
    }

    let ids = mtls
        .identities
        .into_iter()
        .flatten()
        .map(|id| match id.parse() {
            Ok(id) => id,
            Err(e) => match e {},
        });

    let sas = mtls
        .service_accounts
        .into_iter()
        .flatten()
        .filter_map(|sa| {
            let ns = sa.namespace.as_deref().or(metadata.namespace.as_deref())?;
            Some(IdentityMatch::Exact(
                cluster.service_account_identity(ns, &sa.name),
            ))
        });

    let identities = ids.chain(sas).collect::<Vec<_>>();
    if identities.is_empty() {
        bail!("authorization authorizes no clients");
    }

    Ok(ClientAuthentication::TlsAuthenticated(identities))
}
