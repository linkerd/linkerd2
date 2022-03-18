use crate::{
    index::{Index, ServerSelector},
    ClusterInfo,
};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::Result;
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, NetworkMatch,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s, policy::server_authorization::MeshTls, ResourceExt,
};
use std::collections::hash_map::Entry;

impl kubert::index::IndexNamespacedResource<k8s::policy::ServerAuthorization> for Index {
    fn apply(&mut self, saz: k8s::policy::ServerAuthorization) {
        let namespace = saz.namespace().unwrap();
        let name = saz.name();

        let authz = match client_authz(saz.spec.client, &namespace, self.cluster_info()) {
            Ok(ca) => ca,
            Err(error) => {
                tracing::warn!(%error, %namespace, saz = %name, "invalid authorization");
                return;
            }
        };

        self.ns_or_default(namespace).apply_server_authorization(
            name,
            server_selector(saz.spec.server),
            authz,
        )
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Entry::Occupied(mut entry) = self.entry(namespace) {
            entry.get_mut().delete_server_authorization(&*name);
            if entry.get().is_empty() {
                entry.remove();
            }
        }
    }

    fn snapshot_keys(&self) -> HashMap<String, HashSet<String>> {
        self.snapshot_server_authorizations()
    }
}

fn server_selector(s: k8s::policy::server_authorization::Server) -> ServerSelector {
    s.name
        .map(ServerSelector::Name)
        .unwrap_or_else(|| ServerSelector::Selector(s.selector.unwrap_or_default()))
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
            net: net.cidr,
            except: net.except.unwrap_or_default(),
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

    let mut identities = Vec::new();

    for id in mtls.identities.into_iter().flatten() {
        if id == "*" {
            tracing::debug!(suffix = %id, "Authenticated");
            identities.push(IdentityMatch::Suffix(vec![]));
        } else if id.starts_with("*.") {
            tracing::debug!(suffix = %id, "Authenticated");
            let mut parts = id.split('.');
            let star = parts.next();
            debug_assert_eq!(star, Some("*"));
            identities.push(IdentityMatch::Suffix(
                parts.map(|p| p.to_string()).collect::<Vec<_>>(),
            ));
        } else {
            tracing::debug!(%id, "Authenticated");
            identities.push(IdentityMatch::Exact(id));
        }
    }

    for sa in mtls.service_accounts.into_iter().flatten() {
        let name = sa.name;
        let ns = sa.namespace.unwrap_or_else(|| namespace.to_string());
        tracing::debug!(ns = %ns, serviceaccount = %name, "Authenticated");
        let n = format!(
            "{}.{}.serviceaccount.identity.{}.{}",
            name, ns, cluster.control_plane_ns, cluster.identity_domain
        );
        identities.push(IdentityMatch::Exact(n));
    }

    if identities.is_empty() {
        anyhow::bail!("authorization authorizes no clients");
    }

    Ok(ClientAuthentication::TlsAuthenticated(identities))
}
