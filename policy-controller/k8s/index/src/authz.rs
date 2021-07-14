use crate::{Index, ServerSelector, SrvIndex};
use anyhow::{anyhow, bail, Result};
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, IpNet, NetworkMatch,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{self, authz::MeshTls},
    ResourceExt,
};
use std::collections::{hash_map::Entry as HashEntry, HashMap, HashSet};
use tracing::{debug, instrument, trace};

#[derive(Debug, Default)]
pub(crate) struct AuthzIndex {
    index: HashMap<String, Authz>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct Authz {
    servers: ServerSelector,
    clients: ClientAuthorization,
}

// === impl AuthzIndex ===

impl AuthzIndex {
    /// Updates the authorization and server indexes with a new or updated authorization instance.
    fn apply(
        &mut self,
        authz: policy::ServerAuthorization,
        servers: &mut SrvIndex,
        domain: &str,
    ) -> Result<()> {
        let name = authz.name();
        let authz = mk_authz(authz, domain)?;

        match self.index.entry(name) {
            HashEntry::Vacant(entry) => {
                servers.add_authz(entry.key(), &authz.servers, authz.clients.clone());
                entry.insert(authz);
            }

            HashEntry::Occupied(mut entry) => {
                // If the authorization changed materially, then update it in all servers.
                if entry.get() != &authz {
                    servers.add_authz(entry.key(), &authz.servers, authz.clients.clone());
                    entry.insert(authz);
                }
            }
        }

        Ok(())
    }

    fn delete(&mut self, name: &str) {
        self.index.remove(name);
        debug!("Removed authz");
    }

    pub fn filter_selected(
        &self,
        name: impl Into<String>,
        labels: k8s::Labels,
    ) -> impl Iterator<Item = (String, &ClientAuthorization)> {
        let name = name.into();
        self.index.iter().filter_map(move |(authz_name, a)| {
            let matches = match a.servers {
                ServerSelector::Name(ref n) => {
                    trace!(r#ref = %n, %name);
                    n == &name
                }
                ServerSelector::Selector(ref s) => {
                    trace!(selector = ?s, ?labels);
                    s.matches(&labels)
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
}

// === impl Index ===

impl Index {
    /// Constructs an `Authz` and adds it to `Servers` it selects.
    #[instrument(
        skip(self, authz),
        fields(
            ns = ?authz.metadata.namespace,
            name = ?authz.metadata.name,
        )
    )]
    pub(crate) fn apply_authz(&mut self, authz: policy::ServerAuthorization) -> Result<()> {
        let ns = self
            .namespaces
            .get_or_default(authz.namespace().expect("namespace required"));

        ns.authzs
            .apply(authz, &mut ns.servers, &*self.identity_domain)
    }

    #[instrument(
        skip(self, authz),
        fields(
            ns = ?authz.metadata.namespace,
            name = ?authz.metadata.name,
        )
    )]
    pub(crate) fn delete_authz(&mut self, authz: policy::ServerAuthorization) {
        if let Some(ns) = self
            .namespaces
            .index
            .get_mut(authz.namespace().unwrap().as_str())
        {
            let name = authz.name();
            ns.servers.remove_authz(name.as_str());
            ns.authzs.delete(name.as_str());
        }
    }

    #[instrument(skip(self, authzs))]
    pub(crate) fn reset_authzs(&mut self, authzs: Vec<policy::ServerAuthorization>) -> Result<()> {
        let mut prior = self
            .namespaces
            .index
            .iter()
            .map(|(n, ns)| {
                let authzs = ns.authzs.index.keys().cloned().collect::<HashSet<_>>();
                (n.clone(), authzs)
            })
            .collect::<HashMap<_, _>>();

        let mut result = Ok(());
        for authz in authzs.into_iter() {
            if let Some(ns) = prior.get_mut(authz.namespace().unwrap().as_str()) {
                ns.remove(authz.name().as_str());
            }

            if let Err(e) = self.apply_authz(authz) {
                result = Err(e);
            }
        }

        for (ns_name, authzs) in prior {
            if let Some(ns) = self.namespaces.index.get_mut(&ns_name) {
                for name in authzs.into_iter() {
                    ns.servers.remove_authz(&name);
                    ns.authzs.delete(&name);
                }
            }
        }

        result
    }
}

fn mk_authz(srv: policy::authz::ServerAuthorization, domain: &str) -> Result<Authz> {
    let policy::authz::ServerAuthorization { metadata, spec, .. } = srv;

    let servers = {
        let policy::authz::Server { name, selector } = spec.server;
        match (name, selector) {
            (Some(n), None) => ServerSelector::Name(n),
            (None, Some(sel)) => ServerSelector::Selector(sel.into()),
            (Some(_), Some(_)) => bail!("authorization selection is ambiguous"),
            (None, None) => bail!("authorization selects no servers"),
        }
    };

    let networks = if let Some(nets) = spec.client.networks {
        nets.into_iter()
            .map(|policy::authz::Network { cidr, except }| {
                let net = cidr.parse::<IpNet>()?;
                debug!(%net, "Unauthenticated");
                let except = except
                    .into_iter()
                    .map(|cidr| cidr.parse().map_err(Into::into))
                    .collect::<Result<Vec<IpNet>>>()?;
                Ok(NetworkMatch { net, except })
            })
            .collect::<Result<Vec<NetworkMatch>>>()?
    } else {
        // TODO this should only be cluster-local IPs.
        vec![
            IpNet::V4(Default::default()).into(),
            IpNet::V6(Default::default()).into(),
        ]
    };

    let authentication = if spec.client.unauthenticated {
        ClientAuthentication::Unauthenticated
    } else {
        let mtls = spec
            .client
            .mesh_tls
            .ok_or_else(|| anyhow!("client mtls missing"))?;
        mk_mtls_authn(&metadata, mtls, domain)?
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
    domain: &str,
) -> Result<ClientAuthentication> {
    if mtls.unauthenticated_tls {
        return Ok(ClientAuthentication::TlsUnauthenticated);
    }

    let mut identities = Vec::new();

    for id in mtls.identities.into_iter() {
        if id == "*" {
            debug!(suffix = %id, "Authenticated");
            identities.push(IdentityMatch::Suffix(vec![]));
        } else if id.starts_with("*.") {
            debug!(suffix = %id, "Authenticated");
            let mut parts = id.split('.');
            let star = parts.next();
            debug_assert_eq!(star, Some("*"));
            identities.push(IdentityMatch::Suffix(
                parts.map(|p| p.to_string()).collect::<Vec<_>>(),
            ));
        } else {
            debug!(%id, "Authenticated");
            identities.push(IdentityMatch::Name(id));
        }
    }

    for sa in mtls.service_accounts.into_iter() {
        let name = sa.name;
        let ns = sa
            .namespace
            .unwrap_or_else(|| metadata.namespace.clone().unwrap());
        debug!(ns = %ns, serviceaccount = %name, "Authenticated");
        let n = format!("{}.{}.serviceaccount.identity.linkerd.{}", name, ns, domain);
        identities.push(IdentityMatch::Name(n));
    }

    if identities.is_empty() {
        bail!("authorization authorizes no clients");
    }

    Ok(ClientAuthentication::TlsAuthenticated(identities))
}
