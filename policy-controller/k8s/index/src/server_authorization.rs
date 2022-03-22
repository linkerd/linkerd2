use crate::ClusterInfo;
use anyhow::Result;
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, NetworkMatch,
};
use linkerd_policy_controller_k8s_api::{self as k8s, policy::server_authorization::MeshTls};

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
