use crate::{
    index::{Index, ServerSelector},
    ClusterInfo,
};
use anyhow::Result;
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, NetworkMatch,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s, policy::server_authorization::MeshTls, ResourceExt,
};

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

        self.apply_server_authorization(namespace, name, server_selector(saz.spec.server), authz)
    }

    fn delete(&mut self, namespace: String, name: String) {
        self.delete_server_authorization(namespace, &name);
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

    let ids = mtls
        .identities
        .into_iter()
        .flatten()
        .map(|s| match s.parse::<IdentityMatch>() {
            Ok(id) => id,
            Err(e) => match e {},
        });

    let sa_ids = mtls.service_accounts.into_iter().flatten().map(|sa| {
        let ns = sa.namespace.as_deref().unwrap_or(namespace);
        IdentityMatch::Exact(cluster.service_account_identity(ns, &sa.name))
    });

    let identities = ids.chain(sa_ids).collect::<Vec<_>>();
    if identities.is_empty() {
        anyhow::bail!("authorization authorizes no clients");
    }

    Ok(ClientAuthentication::TlsAuthenticated(identities))
}
