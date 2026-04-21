use crate::{ClusterInfo, DefaultPolicy};
use linkerd_policy_controller_core::inbound::ProxyProtocol;
use linkerd_policy_controller_k8s_api::{
    self as k8s, policy::server::Port, policy::server::Selector,
};

/// The parts of a `Server` resource that can change.
#[derive(Debug, PartialEq)]
pub(crate) struct Server {
    pub labels: k8s::Labels,
    pub selector: Selector,
    pub port_ref: Port,
    pub protocol: ProxyProtocol,
    pub access_policy: Option<DefaultPolicy>,
    pub created_at: Option<k8s::Time>,
}

impl Server {
    pub(crate) fn from_resource(srv: k8s::policy::Server, cluster: &ClusterInfo) -> Self {
        Self {
            labels: srv.metadata.labels.into(),
            selector: srv.spec.selector,
            port_ref: srv.spec.port,
            protocol: proxy_protocol(srv.spec.proxy_protocol, cluster),
            access_policy: srv.spec.access_policy.and_then(|p| p.parse().ok()),
            created_at: srv.metadata.creation_timestamp,
        }
    }
}

fn proxy_protocol(
    p: Option<k8s::policy::server::ProxyProtocol>,
    cluster: &ClusterInfo,
) -> ProxyProtocol {
    match p {
        None | Some(k8s::policy::server::ProxyProtocol::Unknown) => ProxyProtocol::Detect {
            timeout: cluster.default_detect_timeout,
        },
        Some(k8s::policy::server::ProxyProtocol::Http1) => ProxyProtocol::Http1,
        Some(k8s::policy::server::ProxyProtocol::Http2) => ProxyProtocol::Http2,
        Some(k8s::policy::server::ProxyProtocol::Grpc) => ProxyProtocol::Grpc,
        Some(k8s::policy::server::ProxyProtocol::Opaque) => ProxyProtocol::Opaque,
        Some(k8s::policy::server::ProxyProtocol::Tls) => ProxyProtocol::Tls,
    }
}
