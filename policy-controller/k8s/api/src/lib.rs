#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod labels;
pub mod policy;
mod watch;

pub use self::{
    labels::Labels,
    watch::{Event, Watch},
};
pub use k8s_openapi::api::{
    self,
    core::v1::{Namespace, Node, NodeSpec, Pod, PodSpec, PodStatus},
};
use kube::api::{Api, ListParams};
pub use kube::api::{ObjectMeta, ResourceExt};
use kube_runtime::watcher;

/// Resource watches.
pub struct ResourceWatches {
    pub nodes_rx: Watch<Node>,
    pub pods_rx: Watch<Pod>,
    pub servers_rx: Watch<policy::Server>,
    pub authorizations_rx: Watch<policy::ServerAuthorization>,
}

// === impl ResourceWatches ===

impl ResourceWatches {
    const DEFAULT_TIMEOUT_SECS: u32 = 5 * 60;
}

impl From<kube::Client> for ResourceWatches {
    fn from(client: kube::Client) -> Self {
        let params = ListParams::default().timeout(Self::DEFAULT_TIMEOUT_SECS);
        Self {
            nodes_rx: watcher(Api::all(client.clone()), params.clone()).into(),
            pods_rx: watcher(
                Api::all(client.clone()),
                params.clone().labels("linkerd.io/control-plane-ns"),
            )
            .into(),
            servers_rx: watcher(Api::all(client.clone()), params.clone()).into(),
            authorizations_rx: watcher(Api::all(client), params).into(),
        }
    }
}
