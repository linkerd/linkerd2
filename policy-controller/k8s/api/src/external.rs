use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

/// Describes an interface exposed by a VM
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "multicluster.linkerd.io",
    version = "v1alpha1",
    kind = "ExternalWorkload",
    status = "ExternalWorkloadStatus",
    namespaced
)]
pub struct ExternalWorkloadSpec {
    pub address: String,
    pub ports: Vec<PortSpec>,
    pub identity: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct PortSpec {
    pub port: std::num::NonZeroU16,
    protocol: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct ExternalWorkloadStatus {
    pub conditions: Vec<Condition>,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Condition {
    last_probe_time: Option<String>,
    last_transition_time: Option<String>,
    status: Option<String>,
    #[serde(rename = "type")]
    typ: Option<String>,
    reason: Option<String>,
    message: Option<String>,
}
