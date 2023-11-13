use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

/// Describes an interface exposed by a VM
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "multicluster.linkerd.io",
    version = "v1alpha1",
    kind = "ExternalEndpoint",
    status = "ExternalEndpointStatus",
    namespaced
)]
pub struct ExternalEndpointSpec {
    #[serde(rename = "workloadIPs")]
    pub workload_ips: Vec<ExternalIP>,
    pub ports: Vec<PortSpec>,
    pub identity: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct PortSpec {
    pub port: std::num::NonZeroU16,
    protocol: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct ExternalEndpointStatus {
    pub conditions: Vec<Condition>,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct ExternalIP {
    pub ip: String,
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
