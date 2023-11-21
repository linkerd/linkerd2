use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

/// Describes an interface exposed by a VM
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "multicluster.linkerd.io",
    version = "v1alpha1",
    kind = "ExternalEndpoint",
    status = "ExternalStatus",
    namespaced
)]
pub struct ExternalEndpointSpec {
    #[serde(rename = "workloadIPs")]
    pub workload_ips: Vec<ExternalIP>,
    pub ports: Vec<PortSpec>,
    pub identity: String,
}

/// Describes a set of VMs that share a template; analogous to a deployment
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "multicluster.linkerd.io",
    version = "v1alpha1",
    kind = "ExternalGroup",
    status = "ExternalStatus",
    namespaced
)]
pub struct ExternalGroupSpec {
    pub template: ExternalTemplate,
    pub ports: Vec<PortSpec>,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct ExternalTemplate {
    pub expected_identity: String,
}

// Shared
#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct PortSpec {
    pub port: std::num::NonZeroU16,
    protocol: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct ExternalStatus {
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
