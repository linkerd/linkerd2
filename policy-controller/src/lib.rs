#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]
mod admission;
pub mod index_list;
mod validation;
pub use self::admission::Admission;
use anyhow::Result;
use linkerd_policy_controller_core::inbound::{
    DiscoverInboundServer, InboundServer, InboundServerStream,
};
use linkerd_policy_controller_core::outbound::{
    DiscoverOutboundPolicy, FallbackPolicyStream, Kind, OutboundDiscoverTarget, OutboundPolicy,
    OutboundPolicyStream, ResourceTarget,
};
pub use linkerd_policy_controller_core::IpNet;
pub use linkerd_policy_controller_grpc as grpc;
pub use linkerd_policy_controller_k8s_api as k8s;
pub use linkerd_policy_controller_k8s_index::{inbound, outbound, ClusterInfo, DefaultPolicy};
use std::net::SocketAddr;
use std::{net::IpAddr, num::NonZeroU16};

#[derive(Clone, Debug)]
pub struct InboundDiscover(inbound::SharedIndex);

#[derive(Clone, Debug)]
pub struct OutboundDiscover(outbound::SharedIndex);

impl InboundDiscover {
    pub fn new(index: inbound::SharedIndex) -> Self {
        Self(index)
    }
}

impl OutboundDiscover {
    pub fn new(index: outbound::SharedIndex) -> Self {
        Self(index)
    }
}

#[async_trait::async_trait]
impl DiscoverInboundServer<(grpc::workload::Workload, NonZeroU16)> for InboundDiscover {
    async fn get_inbound_server(
        &self,
        (workload, port): (grpc::workload::Workload, NonZeroU16),
    ) -> Result<Option<InboundServer>> {
        let grpc::workload::Workload { namespace, kind } = workload;
        let rx = match kind {
            grpc::workload::Kind::External(name) => self
                .0
                .write()
                .external_workload_server_rx(&namespace, &name, port),
            grpc::workload::Kind::Pod(name) => {
                self.0.write().pod_server_rx(&namespace, &name, port)
            }
        };

        if let Ok(rx) = rx {
            let server = (*rx.borrow()).clone();
            Ok(Some(server))
        } else {
            Ok(None)
        }
    }

    async fn watch_inbound_server(
        &self,
        (workload, port): (grpc::workload::Workload, NonZeroU16),
    ) -> Result<Option<InboundServerStream>> {
        let grpc::workload::Workload { namespace, kind } = workload;
        let rx = match kind {
            grpc::workload::Kind::External(name) => self
                .0
                .write()
                .external_workload_server_rx(&namespace, &name, port),
            grpc::workload::Kind::Pod(name) => {
                self.0.write().pod_server_rx(&namespace, &name, port)
            }
        };

        if let Ok(rx) = rx {
            Ok(Some(Box::pin(tokio_stream::wrappers::WatchStream::new(rx))))
        } else {
            Ok(None)
        }
    }
}

#[async_trait::async_trait]
impl DiscoverOutboundPolicy<ResourceTarget, OutboundDiscoverTarget> for OutboundDiscover {
    async fn get_outbound_policy(
        &self,
        resource: ResourceTarget,
    ) -> Result<Option<OutboundPolicy>> {
        let rx = match self.0.write().outbound_policy_rx(resource.clone()) {
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
        target: ResourceTarget,
    ) -> Result<Option<OutboundPolicyStream>> {
        match self.0.write().outbound_policy_rx(target) {
            Ok(rx) => Ok(Some(Box::pin(tokio_stream::wrappers::WatchStream::new(rx)))),
            Err(_) => Ok(None),
        }
    }

    async fn watch_fallback_policy(&self) -> FallbackPolicyStream {
        Box::pin(tokio_stream::wrappers::WatchStream::new(
            self.0.read().fallback_policy_rx(),
        ))
    }

    fn lookup_ip(
        &self,
        addr: IpAddr,
        port: NonZeroU16,
        source_namespace: String,
    ) -> Option<OutboundDiscoverTarget> {
        let index = self.0.read();
        if let Some((namespace, name)) = index.lookup_service(addr) {
            return Some(OutboundDiscoverTarget::Resource(ResourceTarget {
                name,
                namespace,
                port,
                source_namespace,
                kind: Kind::Service,
            }));
        }

        if let Some((namespace, name)) = index.lookup_egress_network(addr, source_namespace.clone())
        {
            let original_dst = SocketAddr::new(addr, port.into());
            return Some(OutboundDiscoverTarget::Resource(ResourceTarget {
                name,
                namespace,
                port,
                source_namespace,
                kind: Kind::EgressNetwork(original_dst),
            }));
        }

        if !index.is_address_in_cluster(addr) {
            let original_dst = SocketAddr::new(addr, port.into());
            return Some(OutboundDiscoverTarget::Fallback(original_dst));
        }

        None
    }
}
