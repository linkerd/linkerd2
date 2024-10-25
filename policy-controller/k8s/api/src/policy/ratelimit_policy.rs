use super::{LocalTargetRef, NamespacedTargetRef};

#[derive(
    Clone, Debug, kube::CustomResource, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "HTTPLocalRateLimitPolicy",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct RateLimitPolicySpec {
    pub target_ref: LocalTargetRef,
    pub total: Option<Limit>,
    pub identity: Option<Limit>,
    pub overrides: Option<Vec<Override>>,
}

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Limit {
    pub requests_per_second: u32,
}

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Override {
    pub requests_per_second: u32,
    pub client_refs: Vec<NamespacedTargetRef>,
}
