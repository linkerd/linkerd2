use crate::Index;
use linkerd_policy_controller_core::ProxyProtocol;
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};

impl kubert::index::IndexNamespacedResource<k8s::policy::Server> for Index {
    fn apply(&mut self, server: k8s::policy::Server) {
        let namespace = server.namespace().unwrap();
        self.apply_server(
            namespace,
            server.name(),
            server.metadata.labels.into(),
            server.spec.pod_selector,
            server.spec.port,
            proxy_protocol(server.spec.proxy_protocol),
        );
    }

    fn delete(&mut self, namespace: String, name: String) {
        self.delete_server(namespace, &name);
    }
}

fn proxy_protocol(p: Option<k8s::policy::server::ProxyProtocol>) -> Option<ProxyProtocol> {
    match p? {
        k8s::policy::server::ProxyProtocol::Unknown => None,
        k8s::policy::server::ProxyProtocol::Http1 => Some(ProxyProtocol::Http1),
        k8s::policy::server::ProxyProtocol::Http2 => Some(ProxyProtocol::Http2),
        k8s::policy::server::ProxyProtocol::Grpc => Some(ProxyProtocol::Http2),
        k8s::policy::server::ProxyProtocol::Opaque => Some(ProxyProtocol::Opaque),
        k8s::policy::server::ProxyProtocol::Tls => Some(ProxyProtocol::Tls),
    }
}
