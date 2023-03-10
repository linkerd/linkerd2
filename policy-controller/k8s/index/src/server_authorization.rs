use crate::{ClusterInfo, NsUpdate};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::Result;
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, NetworkMatch,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s, policy::server_authorization::MeshTls, ResourceExt,
};
use tracing::info_span;

/// The parts of a `ServerAuthorization` resource that can change.
#[derive(Debug, PartialEq)]
pub(crate) struct ServerAuthz {
    pub authz: ClientAuthorization,
    pub server_selector: ServerSelector,
}

/// Selects `Server`s for a `ServerAuthoriation`
#[derive(Clone, Debug, PartialEq)]
pub(crate) enum ServerSelector {
    Name(String),
    Selector(k8s::labels::Selector),
}

impl kubert::index::IndexNamespacedResource<k8s::policy::ServerAuthorization> for crate::Index {
    fn apply(&mut self, saz: k8s::policy::ServerAuthorization) {
        let ns = saz.namespace().unwrap();
        let name = saz.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        match ServerAuthz::from_resource(saz, &self.cluster_info) {
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
        type Ns = NsUpdate<ServerAuthz>;
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for saz in sazs.into_iter() {
            let namespace = saz
                .namespace()
                .expect("serverauthorization must be namespaced");
            let name = saz.name_unchecked();
            match ServerAuthz::from_resource(saz, &self.cluster_info) {
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

impl ServerAuthz {
    pub(crate) fn from_resource(
        saz: k8s::policy::ServerAuthorization,
        cluster: &ClusterInfo,
    ) -> Result<Self> {
        let authz = {
            let namespace = saz
                .metadata
                .namespace
                .as_deref()
                .expect("resource must be namespaced");
            client_authz(saz.spec.client, namespace, cluster)?
        };
        let server_selector = saz.spec.server.into();
        Ok(Self {
            authz,
            server_selector,
        })
    }
}

// === impl ServerSelector ===

impl From<k8s::policy::server_authorization::Server> for ServerSelector {
    fn from(s: k8s::policy::server_authorization::Server) -> Self {
        s.name
            .map(Self::Name)
            .unwrap_or_else(|| Self::Selector(s.selector.unwrap_or_default()))
    }
}

impl ServerSelector {
    pub(crate) fn selects(&self, name: &str, labels: &k8s::Labels) -> bool {
        match self {
            Self::Name(n) => *n == name,
            Self::Selector(selector) => selector.matches(labels),
        }
    }
}

fn client_authz(
    client: k8s::policy::server_authorization::Client,
    namespace: &str,
    cluster: &ClusterInfo,
) -> Result<ClientAuthorization> {
    let networks = client
        .networks
        .into_iter()
        .flatten()
        .map(|net| NetworkMatch {
            net: net.cidr.into(),
            except: net.except.into_iter().flatten().map(Into::into).collect(),
        })
        .collect();

    let authentication = if client.unauthenticated {
        ClientAuthentication::Unauthenticated
    } else if let Some(mtls) = client.mesh_tls {
        client_mtls_authn(mtls, namespace, cluster)?
    } else {
        anyhow::bail!("no client authentication configured");
    };

    Ok(ClientAuthorization {
        networks,
        authentication,
    })
}

fn client_mtls_authn(
    mtls: MeshTls,
    namespace: &str,
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

    let sas = mtls.service_accounts.into_iter().flatten().map(|sa| {
        let ns = sa.namespace.as_deref().unwrap_or(namespace);
        IdentityMatch::Exact(cluster.service_account_identity(ns, &sa.name))
    });

    let identities = ids.chain(sas).collect::<Vec<_>>();
    if identities.is_empty() {
        anyhow::bail!("authorization authorizes no clients");
    }

    Ok(ClientAuthentication::TlsAuthenticated(identities))
}
