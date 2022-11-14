use crate::ClusterInfo;
use ahash::{AHashMap as HashMap};
use linkerd_policy_controller_core::http_route::OutboundHttpRoute;
use linkerd_policy_controller_k8s_api::policy::HttpRoute;
use std::sync::Arc;

#[derive(Debug)]
pub struct Index {
    _cluster_info: Arc<ClusterInfo>,
    _namespaces: NamespaceIndex,
}

#[derive(Debug)]
pub struct NamespaceIndex {
    _cluster_info: Arc<ClusterInfo>,
    _by_ns: HashMap<String, Namespace>,
}

#[derive(Debug)]
struct Namespace {
    _http_routes: HashMap<String, OutboundHttpRoute>,
}

impl kubert::index::IndexNamespacedResource<HttpRoute> for Index {
    fn apply(&mut self, resource: HttpRoute) {
        todo!()
    }

    fn delete(&mut self, namespace: String, name: String) {
        todo!()
    }
}
