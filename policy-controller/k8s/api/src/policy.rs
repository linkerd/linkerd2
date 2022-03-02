pub mod authorization_policy;
pub mod server;
pub mod server_authorization;

pub use self::server::{Server, ServerSpec};
pub use self::server_authorization::{ServerAuthorization, ServerAuthorizationSpec};

#[derive(Default, serde::Deserialize, serde::Serialize, Clone, Debug, schemars::JsonSchema)]
pub struct TargetRef {
    pub group: Option<String>,
    pub kind: String,
    pub name: Option<String>,
}
