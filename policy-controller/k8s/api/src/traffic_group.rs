use std::collections::BTreeMap;

use k8s_gateway_api::ParentReference;
use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "multicluster.linkerd.io",
    version = "v1alpha1",
    kind = "TrafficGroup",
    namespaced
)]
pub struct TrafficGroupSpec {
    #[serde(rename = "parentRefs")]
    pub parent_refs: Vec<ParentReference>,
    pub strategy: String,
    pub subsets: Vec<TrafficSubset>,
}

#[derive(Debug, PartialEq, Eq, Clone, Deserialize, Serialize, JsonSchema)]
pub struct TrafficSubset {
    pub name: String,
    pub labels: BTreeMap<String, String>,
}
