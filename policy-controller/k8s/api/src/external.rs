use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

// === Endpoint ===

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
    #[serde(rename = "serverName")]
    pub server_name: String,
}

// === Group ===

/// Describes a set of VMs that share a template; analogous to a deployment
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "multicluster.linkerd.io",
    version = "v1alpha1",
    kind = "ExternalGroup",
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

// === Shared ===

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct PortSpec {
    pub port: std::num::NonZeroU16,
    pub protocol: String,
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
    status: Option<ExternalConditionStatus>,
    #[serde(rename = "type")]
    typ: Option<ExternalConditionType>,
    reason: Option<String>,
    message: Option<String>,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum ExternalConditionType {
    Ready,
    Initialized,
    Deleted,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum ExternalConditionStatus {
    True,
    False,
    Unknown,
}

/// Check whether an ExternalStatus has a condition set, and if it does, checks
/// whether it is ready.
pub fn is_ready(status: Option<&ExternalStatus>) -> bool {
    status
        .map(|status| status.conditions.iter())
        .into_iter()
        .flatten()
        .find(|cond| matches!(cond.typ, Some(ExternalConditionType::Ready)))
        .map(|cond| matches!(cond.status, Some(ExternalConditionStatus::True)))
        .unwrap_or(false)
}

/// Checks whether an endpoint is marked for termination by looking at its
/// condition (for now)
pub fn is_terminating(status: Option<&ExternalStatus>) -> bool {
    status
        .map(|status| status.conditions.iter())
        .into_iter()
        .flatten()
        .find(|cond| matches!(cond.typ, Some(ExternalConditionType::Deleted)))
        .map(|cond| matches!(cond.status, Some(ExternalConditionStatus::True)))
        .unwrap_or(false)
}
