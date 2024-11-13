use super::{LocalTargetRef, NamespacedTargetRef};
use k8s_openapi::apimachinery::pkg::apis::meta::v1::Condition;

#[derive(
    Clone, Debug, kube::CustomResource, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "HTTPLocalRateLimitPolicy",
    status = "HTTPLocalRateLimitPolicyStatus",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct RateLimitPolicySpec {
    pub target_ref: LocalTargetRef,
    pub total: Option<Limit>,
    pub identity: Option<Limit>,
    pub overrides: Option<Vec<Override>>,
}

#[derive(Clone, Debug, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct HTTPLocalRateLimitPolicyStatus {
    pub conditions: Vec<Condition>,
    pub target_ref: LocalTargetRef,
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
