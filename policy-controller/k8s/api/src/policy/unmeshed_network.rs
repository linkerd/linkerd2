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
    pub traffic_policy: TrafficPolicy,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum TrafficPolicy {
    AllowUnknown,
    DenyUnknown,
}
