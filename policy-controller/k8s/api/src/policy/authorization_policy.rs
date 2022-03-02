use super::TargetRef;
use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

#[derive(CustomResource, Default, Deserialize, Serialize, Clone, Debug, JsonSchema)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "AuthorizationPolicy",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct AuthorizationPolicySpec {
    pub target_ref: TargetRef,
    pub required_authentication_refs: Vec<RequiredAuthenticationRef>,
}

#[derive(Default, Deserialize, Serialize, Clone, Debug, JsonSchema)]
pub struct RequiredAuthenticationRef {
    pub group: Option<String>,
    pub kind: String,
    pub namespace: Option<String>,
    pub name: Option<String>,
}
