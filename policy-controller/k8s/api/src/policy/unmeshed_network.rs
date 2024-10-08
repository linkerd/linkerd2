use super::network::Cidr;
use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "UnmeshedNetwork",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct UnmeshedNetworkSpec {
    pub networks: Vec<Cidr>,
    pub default_policy: DefaultPolicy,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum DefaultPolicy {
    AllowUnknown,
    DenyUnknown,
}

impl Default for DefaultPolicy {
    fn default() -> Self {
        Self::AllowUnknown
    }
}
