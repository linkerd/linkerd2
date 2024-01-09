use super::super::labels;
use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use std::{fmt, num::NonZeroU16};

/// Describes a server interface exposed by a set of pods.
#[derive(Clone, Debug, PartialEq, Eq, CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1beta2",
    kind = "Server",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct ServerSpec {
    #[serde(flatten)]
    pub selector: Selector,
    pub port: Port,
    pub proxy_protocol: Option<ProxyProtocol>,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum Selector {
    #[serde(rename = "podSelector")]
    Pod(labels::Selector),
    #[serde(rename = "externalWorkloadSelector")]
    ExternalWorkload(labels::Selector),
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

impl fmt::Display for Port {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Port::Number(n) => fmt::Display::fmt(n, f),
            Port::Name(n) => fmt::Display::fmt(n, f),
        }
    }
}
