#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::Result;

mod admission;

pub use self::admission::Admission;
pub use linkerd_policy_controller_core::{
    DiscoverInboundServer, InboundServer, InboundServerStream, IpNet,
};
pub use linkerd_policy_controller_grpc as grpc;
pub use linkerd_policy_controller_k8s_api as k8s;
pub use linkerd_policy_controller_k8s_index::{ClusterInfo, DefaultPolicy, Index, SharedIndex};

#[derive(Clone, Debug)]
pub struct IndexDiscover(SharedIndex);

impl IndexDiscover {
    pub fn new(index: SharedIndex) -> Self {
        Self(index)
    }
}

#[async_trait::async_trait]
impl DiscoverInboundServer<(String, String, u16)> for IndexDiscover {
    async fn get_inbound_server(
        &self,
        (namespace, pod, port): (String, String, u16),
    ) -> Result<Option<InboundServer>> {
        let rx = match self.0.write().pod_server_rx(&namespace, &pod, port) {
            Ok(rx) => rx,
            Err(_) => return Ok(None),
        };
        let server = (*rx.borrow()).clone();
        Ok(Some(server))
    }

    async fn watch_inbound_server(
        &self,
        (namespace, pod, port): (String, String, u16),
    ) -> Result<Option<InboundServerStream>> {
        match self.0.write().pod_server_rx(&namespace, &pod, port) {
            Ok(rx) => Ok(Some(Box::pin(tokio_stream::wrappers::WatchStream::new(rx)))),
            Err(_) => Ok(None),
        }
    }
}
