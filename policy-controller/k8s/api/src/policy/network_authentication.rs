pub use super::Network;

#[derive(
    Clone,
    Debug,
    Default,
    Eq,
    PartialEq,
    kube::CustomResource,
    serde::Deserialize,
    serde::Serialize,
    schemars::JsonSchema,
)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "NetworkAuthentication",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct NetworkAuthenticationSpec {
    pub networks: Vec<Network>,
}
