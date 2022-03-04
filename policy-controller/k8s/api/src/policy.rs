pub mod authorization_policy;
pub mod meshtls_authentication;
pub mod network_authentication;
pub mod server;
pub mod server_authorization;

pub use self::{
    authorization_policy::{AuthorizationPolicy, AuthorizationPolicySpec},
    meshtls_authentication::{MeshTLSAuthentication, MeshTLSAuthenticationSpec},
    network_authentication::{NetworkAuthentication, NetworkAuthenticationSpec},
    server::{Server, ServerSpec},
    server_authorization::{ServerAuthorization, ServerAuthorizationSpec},
};

/// Targets a resource--or resource type--within a the same namespace.
#[derive(Clone, Debug, Default, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
pub struct TargetRef {
    pub group: Option<String>,
    pub kind: String,
    pub name: Option<String>,
}

impl TargetRef {
    pub fn targets_kind<T>(&self) -> bool
    where
        T: kube::Resource,
        T::DynamicType: Default,
    {
        let dt = Default::default();
        self.group.as_deref() == Some(&*T::group(&dt)) && *self.kind == *T::kind(&dt)
    }
}
