use super::network::Network;
use k8s_openapi::apimachinery::pkg::apis::meta::v1::Condition;
use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "EgressNetwork",
    status = "EgressNetworkStatus",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct EgressNetworkSpec {
    pub networks: Option<Vec<Network>>,
    pub traffic_policy: TrafficPolicy,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum TrafficPolicy {
    AllowAll,
    DenyAll,
}

#[derive(Clone, Debug, PartialEq, Deserialize, Serialize, JsonSchema)]
pub struct EgressNetworkStatus {
    pub conditions: Vec<Condition>,
}
