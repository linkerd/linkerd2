use super::super::labels;
use ipnet::IpNet;
use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

/// Authorizes clients to connect to a Server.
#[derive(CustomResource, Default, Deserialize, Serialize, Clone, Debug, JsonSchema)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1beta1",
    kind = "ServerAuthorization",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct ServerAuthorizationSpec {
    pub server: Server,
    pub client: Client,
}

#[derive(Default, Deserialize, Serialize, Clone, Debug, JsonSchema)]
pub struct Server {
    pub name: Option<String>,
    pub selector: Option<labels::Selector>,
}

/// Describes an authenticated client.
///
/// Exactly one of `identities` and `service_accounts` should be set.
#[derive(Default, Deserialize, Serialize, Clone, Debug, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Client {
    pub networks: Option<Vec<Network>>,

    #[serde(default)]
    pub unauthenticated: bool,

    #[serde(rename = "meshTLS")]
    pub mesh_tls: Option<MeshTls>,
}

/// Describes an authenticated client.
///
/// Exactly one of `identities` and `service_accounts` should be set.
#[derive(Default, Deserialize, Serialize, Clone, Debug, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct MeshTls {
    #[serde(rename = "unauthenticatedTLS", default)]
    pub unauthenticated_tls: bool,

    /// Indicates a Linkerd identity that is authorized to access a server.
    pub identities: Option<Vec<String>>,

    /// Identifies a `ServiceAccount` authorized to access a server.
    pub service_accounts: Option<Vec<ServiceAccountRef>>,
}

#[derive(Deserialize, Serialize, Clone, Debug, JsonSchema)]
pub struct Network {
    pub cidr: IpNet,
    pub except: Option<Vec<IpNet>>,
}

/// References a Kubernetes `ServiceAccount` instance.
///
/// If no namespace is specified, the `Authorization`'s namespace is used.
#[derive(Deserialize, Serialize, Clone, Debug, JsonSchema)]
pub struct ServiceAccountRef {
    pub namespace: Option<String>,
    pub name: String,
}
