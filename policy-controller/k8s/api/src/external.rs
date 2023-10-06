use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

/// Describes an interface exposed by a VM
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "linkerd.io",
    version = "v1alpha1",
    kind = "ExternalWorkload",
    namespaced
)]
pub struct ExternalWorkloadSpec {
    pub ports: Vec<PortSpec>,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct PortSpec {
    pub port: std::num::NonZeroU16,
    protocol: String,
}
