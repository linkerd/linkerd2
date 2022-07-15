use super::super::labels;
use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use std::num::NonZeroU16;

/// Describes a server interface exposed by a set of pods.
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1beta1",
    kind = "Server",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct ServerSpec {
    pub pod_selector: labels::Selector,
    pub port: Port,
    pub proxy_protocol: Option<ProxyProtocol>,
}

/// References a pod spec's port by name or number.
#[derive(Clone, Debug, PartialEq, Eq, Hash, Deserialize, Serialize, JsonSchema)]
#[serde(untagged)]
pub enum Port {
    Number(NonZeroU16),
    Name(String),
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum ProxyProtocol {
    #[serde(rename = "unknown")]
    Unknown,
    #[serde(rename = "HTTP/1")]
    Http1,
    #[serde(rename = "HTTP/2")]
    Http2,
    #[serde(rename = "gRPC")]
    Grpc,
    #[serde(rename = "opaque")]
    Opaque,
    #[serde(rename = "TLS")]
    Tls,
}
