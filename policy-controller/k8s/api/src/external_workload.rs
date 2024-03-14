use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

/// ExternalWorkload describes a single workload (i.e. a deployable unit,
/// conceptually similar to a Kubernetes Pod) that is running outside of a
/// Kubernetes cluster. An ExternalWorkload should be enrolled in the mesh and
/// typically represents a virtual machine.
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "workload.linkerd.io",
    version = "v1beta1",
    kind = "ExternalWorkload",
    status = "ExternalWorkloadStatus",
    namespaced
)]
pub struct ExternalWorkloadSpec {
    /// MeshTls describes TLS settings associated with an external workload
    #[serde(rename = "meshTLS")]
    pub mesh_tls: MeshTls,
    /// Ports describes a set of ports exposed by the workload
    pub ports: Option<Vec<PortSpec>>,
    /// List of IP addresses that can be used to send traffic to an external
    /// workload
    #[serde(rename = "workloadIPs")]
    pub workload_ips: Option<Vec<WorkloadIP>>,
}

/// MeshTls describes TLS settings associated with an external workload
#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct MeshTls {
    /// Identity associated with the workload. Used by peers to perform
    /// verification in the mTLS handshake
    pub identity: String,
    /// ServerName is the DNS formatted name associated with the workload. Used
    /// to terminate TLS using the SNI extension.
    #[serde(rename = "serverName")]
    pub server_name: String,
}

/// PortSpec represents a network port in a single workload.
#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct PortSpec {
    /// If specified, must be an IANA_SVC_NAME and unique within the exposed
    /// ports set. Each named port must have a unique name. The name may be
    /// referred to by services
    pub name: Option<String>,
    /// Number of port exposed on the workload's IP address.
    /// Must be a valid port number, i.e. 0 < x < 65536.
    pub port: std::num::NonZeroU16,
    /// Protocol defines network protocols supported. One of UDP, TCP, or SCTP.
    /// Should coincide with Service selecting the workload.
    /// Defaults to "TCP" if unspecified.
    pub protocol: Option<String>,
}

/// WorkloadIPs contains a list of IP addresses exposed by an ExternalWorkload
#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct WorkloadIP {
    pub ip: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct ExternalWorkloadStatus {
    pub conditions: Vec<Condition>,
}

/// WorkloadCondition represents the service state of an ExternalWorkload
#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Condition {
    /// Type of the condition
    // see: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#pod-conditions
    #[serde(rename = "type")]
    typ: String,
    /// Status of the condition.
    /// Can be True, False, Unknown
    status: ConditionStatus,
    /// Last time a condition transitioned from one status to another.
    last_transition_time: Option<crate::apimachinery::pkg::apis::meta::v1::Time>,
    /// Last time an ExternalWorkload was probed for a condition.
    last_probe_time: Option<crate::apimachinery::pkg::apis::meta::v1::Time>,
    /// Unique one word reason in CamelCase that describes the reason for a
    /// transition.
    reason: Option<String>,
    /// Human readable message that describes details about last transition.
    message: Option<String>,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum ConditionStatus {
    True,
    False,
    Unknown,
}
