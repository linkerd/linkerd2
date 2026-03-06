use super::LocalTargetRef;
use k8s_openapi::apimachinery::pkg::apis::meta::v1::Condition;

#[derive(
    Clone, Debug, kube::CustomResource, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "HTTPLocalConcurrencyLimitPolicy",
    root = "HttpLocalConcurrencyLimitPolicy",
    status = "HttpLocalConcurrencyLimitPolicyStatus",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct ConcurrencyLimitPolicySpec {
    pub target_ref: LocalTargetRef,
    /// Maximum number of concurrent in-flight requests allowed.
    pub max_in_flight_requests: u32,
}

#[derive(Clone, Debug, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct HttpLocalConcurrencyLimitPolicyStatus {
    pub conditions: Vec<Condition>,
    pub target_ref: LocalTargetRef,
}
