#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::Result;
use index::outbound_index::ServiceRef;
use linkerd_policy_controller_core::{
    DiscoverOutboundPolicy, OutboundPolicy, OutboundPolicyStream,
};
use std::{net::IpAddr, num::NonZeroU16};

mod admission;
mod index_pair;

pub use self::admission::Admission;
pub use self::index_pair::IndexPair;
pub use linkerd_policy_controller_core::{
    DiscoverInboundServer, InboundServer, InboundServerStream, IpNet,
};
pub use linkerd_policy_controller_grpc as grpc;
pub use linkerd_policy_controller_k8s_api as k8s;
pub use linkerd_policy_controller_k8s_index::{
    self as index, outbound_index, ClusterInfo, DefaultPolicy, Index, SharedIndex,
};

#[derive(Clone, Debug)]
pub struct IndexDiscover(SharedIndex);

#[derive(Clone, Debug)]
pub struct OutboundDiscover(outbound_index::SharedIndex);

impl IndexDiscover {
    pub fn new(index: SharedIndex) -> Self {
        Self(index)
    }
}

impl OutboundDiscover {
    pub fn new(index: outbound_index::SharedIndex) -> Self {
        Self(index)
    }
}

#[async_trait::async_trait]
impl DiscoverInboundServer<(String, String, NonZeroU16)> for IndexDiscover {
    async fn get_inbound_server(
        &self,
        (namespace, pod, port): (String, String, NonZeroU16),
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
        (namespace, pod, port): (String, String, NonZeroU16),
    ) -> Result<Option<InboundServerStream>> {
        match self.0.write().pod_server_rx(&namespace, &pod, port) {
            Ok(rx) => Ok(Some(Box::pin(tokio_stream::wrappers::WatchStream::new(rx)))),
            Err(_) => Ok(None),
        }
    }
}

#[async_trait::async_trait]
impl DiscoverOutboundPolicy<(String, String, NonZeroU16)> for OutboundDiscover {
    async fn get_outbound_policy(
        &self,
        (namespace, service, port): (String, String, NonZeroU16),
    ) -> Result<Option<OutboundPolicy>> {
        let rx = match self
            .0
            .write()
            .outbound_policy_rx(namespace, service, port)
        {
            Ok(rx) => rx,
            Err(error) => {
                tracing::error!(%error, "failed to get outbound policy rx");
                return Ok(None);
            }
        };
        let policy = (*rx.borrow()).clone();
        Ok(Some(policy))
    }

    async fn watch_outbound_policy(
        &self,
        (namespace, service, port): (String, String, NonZeroU16),
    ) -> Result<Option<OutboundPolicyStream>> {
        match self
            .0
            .write()
            .outbound_policy_rx(namespace, service, port)
        {
            Ok(rx) => Ok(Some(Box::pin(tokio_stream::wrappers::WatchStream::new(rx)))),
            Err(_) => Ok(None),
        }
    }

    fn service_lookup(
        &self,
        addr: IpAddr,
        port: NonZeroU16,
    ) -> Option<(String, String, NonZeroU16)> {
        self.0
            .read()
            .lookup_service(addr)
            .map(|ServiceRef { namespace, name }| (namespace, name, port))
    }
}
