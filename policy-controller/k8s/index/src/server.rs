use crate::Index;
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use linkerd_policy_controller_core::ProxyProtocol;
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};
use std::collections::hash_map::Entry;

impl kubert::index::IndexNamespacedResource<k8s::policy::Server> for Index {
    fn apply(&mut self, server: k8s::policy::Server) {
        let namespace = server.namespace().unwrap();
        self.ns_or_default(namespace).apply_server(
            server.name(),
            server.metadata.labels.into(),
            server.spec.pod_selector,
            server.spec.port,
            proxy_protocol(server.spec.proxy_protocol),
        );
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Entry::Occupied(mut entry) = self.entry(namespace) {
            entry.get_mut().delete_server(&*name);
            if entry.get().is_empty() {
                entry.remove();
            }
        }
    }

    fn snapshot_keys(&self) -> HashMap<String, HashSet<String>> {
        self.snapshot_servers()
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
