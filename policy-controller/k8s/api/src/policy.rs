pub mod authz;
pub mod server;
pub mod server_authz;

pub use self::server::{Server, ServerSpec};
pub use self::server_authz::{ServerAuthorization, ServerAuthorizationSpec};

#[derive(Default, serde::Deserialize, serde::Serialize, Clone, Debug, schemars::JsonSchema)]
pub struct TargetRef {
    pub group: Option<String>,
    pub kind: String,
    pub name: Option<String>,
}
