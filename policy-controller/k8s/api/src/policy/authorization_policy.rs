use super::{LocalTargetRef, NamespacedTargetRef};

#[derive(
    Clone, Debug, kube::CustomResource, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "AuthorizationPolicy",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct AuthorizationPolicySpec {
    pub target_ref: LocalTargetRef,
    pub required_authentication_refs: Vec<NamespacedTargetRef>,
}
