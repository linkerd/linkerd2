pub mod authorization_policy;
pub mod meshtls_authentication;
// pub mod network_authentication;
pub mod server;
pub mod server_authorization;

pub use self::authorization_policy::{AuthorizationPolicy, AuthorizationPolicySpec};
pub use self::server::{Server, ServerSpec};
pub use self::server_authorization::{ServerAuthorization, ServerAuthorizationSpec};

#[derive(Default, serde::Deserialize, serde::Serialize, Clone, Debug, schemars::JsonSchema)]
pub struct TargetRef {
    pub group: Option<String>,
    pub kind: String,
    pub name: Option<String>,
}
