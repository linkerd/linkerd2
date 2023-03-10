use crate::{server, ClusterInfo, HashMap, HashSet, NsUpdate};
use linkerd_policy_controller_core::ProxyProtocol;
use linkerd_policy_controller_k8s_api::{self as k8s, policy::server::Port, ResourceExt};
use tracing::info_span;

/// The parts of a `Server` resource that can change.
#[derive(Debug, PartialEq)]
pub(crate) struct Server {
    pub labels: k8s::Labels,
    pub pod_selector: k8s::labels::Selector,
    pub port_ref: Port,
    pub protocol: ProxyProtocol,
}

impl kubert::index::IndexNamespacedResource<k8s::policy::Server> for crate::Index {
    fn apply(&mut self, srv: k8s::policy::Server) {
        let ns = srv.namespace().expect("server must be namespaced");
        let name = srv.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        let server = server::Server::from_resource(srv, &self.cluster_info);
        self.ns_or_default_with_reindex(ns, |ns| ns.policy.update_server(name, server))
    }

    fn delete(&mut self, ns: String, name: String) {
        let _span = info_span!("delete", %ns, %name).entered();
        self.ns_with_reindex(ns, |ns| ns.policy.servers.remove(&name).is_some())
    }

    fn reset(&mut self, srvs: Vec<k8s::policy::Server>, deleted: HashMap<String, HashSet<String>>) {
        let _span = info_span!("reset").entered();

        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        type Ns = NsUpdate<server::Server>;
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for srv in srvs.into_iter() {
            let namespace = srv.namespace().expect("server must be namespaced");
            let name = srv.name_unchecked();
            let server = server::Server::from_resource(srv, &self.cluster_info);
            updates_by_ns
                .entry(namespace)
                .or_default()
                .added
                .push((name, server));
        }
        for (ns, names) in deleted.into_iter() {
            updates_by_ns.entry(ns).or_default().removed = names;
        }

        for (namespace, Ns { added, removed }) in updates_by_ns.into_iter() {
            if added.is_empty() {
                // If there are no live resources in the namespace, we do not
                // want to create a default namespace instance, we just want to
                // clear out all resources for the namespace (and then drop the
                // whole namespace, if necessary).
                self.ns_with_reindex(namespace, |ns| {
                    ns.policy.servers.clear();
                    true
                });
            } else {
                // Otherwise, we take greater care to reindex only when the
                // state actually changed. The vast majority of resets will see
                // no actual data change.
                self.ns_or_default_with_reindex(namespace, |ns| {
                    let mut changed = !removed.is_empty();
                    for name in removed.into_iter() {
                        ns.policy.servers.remove(&name);
                    }
                    for (name, server) in added.into_iter() {
                        changed = ns.policy.update_server(name, server) || changed;
                    }
                    changed
                });
            }
        }
    }
}

impl Server {
    pub(crate) fn from_resource(srv: k8s::policy::Server, cluster: &ClusterInfo) -> Self {
        Self {
            labels: srv.metadata.labels.into(),
            pod_selector: srv.spec.pod_selector,
            port_ref: srv.spec.port,
            protocol: proxy_protocol(srv.spec.proxy_protocol, cluster),
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
        Some(k8s::policy::server::ProxyProtocol::Grpc) => ProxyProtocol::Http2,
        Some(k8s::policy::server::ProxyProtocol::Opaque) => ProxyProtocol::Opaque,
        Some(k8s::policy::server::ProxyProtocol::Tls) => ProxyProtocol::Tls,
    }
}
