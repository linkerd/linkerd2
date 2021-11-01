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
pub use kube::api::{ObjectMeta, ResourceExt};
use kube::{
    api::{Api, ListParams},
    runtime::watcher,
};
use tracing::info_span;

/// Resource watches.
pub struct ResourceWatches {
    pub pods_rx: Watch<Pod>,
    pub servers_rx: Watch<policy::Server>,
    pub authorizations_rx: Watch<policy::ServerAuthorization>,
}

// === impl ResourceWatches ===

impl ResourceWatches {
    /// Limits the amount of time a watch can be idle before being reset.
    ///
    /// Must be less than 295 or Kubernetes throws an error.
    const DEFAULT_TIMEOUT_SECS: u32 = 290;
}

impl From<kube::Client> for ResourceWatches {
    fn from(client: kube::Client) -> Self {
        let params = ListParams::default().timeout(Self::DEFAULT_TIMEOUT_SECS);

        // We only need to watch pods that are injected with a Linkerd sidecar because these are the
        // only pods that can have inbound policy. We avoid indexing information about uninjected
        // pods.
        let pod_params = params.clone().labels("linkerd.io/control-plane-ns");

        Self {
            pods_rx: Watch::from(watcher(Api::all(client.clone()), pod_params))
                .instrument(info_span!("pods")),
            servers_rx: Watch::from(watcher(Api::all(client.clone()), params.clone()))
                .instrument(info_span!("servers")),
            authorizations_rx: Watch::from(watcher(Api::all(client), params))
                .instrument(info_span!("serverauthorizations")),
        }
    }
}
