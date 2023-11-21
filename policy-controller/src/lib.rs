#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]
mod admission;
pub mod index_list;
pub use self::admission::Admission;
use anyhow::Result;
use linkerd_policy_controller_core::inbound::{
    DiscoverInboundServer, InboundServer, InboundServerStream,
};
use linkerd_policy_controller_core::outbound::{
    DiscoverOutboundPolicy, OutboundDiscoverTarget, OutboundPolicy, OutboundPolicyStream,
};
pub use linkerd_policy_controller_core::IpNet;
pub use linkerd_policy_controller_grpc as grpc;
pub use linkerd_policy_controller_k8s_api as k8s;
pub use linkerd_policy_controller_k8s_index::{inbound, outbound, ClusterInfo, DefaultPolicy};
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
impl DiscoverInboundServer<(grpc::inbound::PolicyWorkload, NonZeroU16)> for InboundDiscover {
    async fn get_inbound_server(
        &self,
        (workload, port): (grpc::inbound::PolicyWorkload, NonZeroU16),
    ) -> Result<Option<InboundServer>> {
        let rx = match workload {
            grpc::inbound::PolicyWorkload::Pod(ns, name) => {
                self.0.write().pod_server_rx(&ns, &name, port)
            }
            grpc::inbound::PolicyWorkload::External(ns, name) => {
                self.0.write().external_server_rx(&ns, &name, port)
            }
        };

        if let Some(rx) = rx.ok() {
            let server = (*rx.borrow()).clone();
            return Ok(Some(server));
        };

        Ok(None)
    }

    async fn watch_inbound_server(
        &self,
        (workload, port): (grpc::inbound::PolicyWorkload, NonZeroU16),
    ) -> Result<Option<InboundServerStream>> {
        let rx = match workload {
            grpc::inbound::PolicyWorkload::Pod(ns, name) => {
                self.0.write().pod_server_rx(&ns, &name, port)
            }
            grpc::inbound::PolicyWorkload::External(ns, name) => {
                self.0.write().external_server_rx(&ns, &name, port)
            }
        };

        match rx {
            Ok(rx) => Ok(Some(Box::pin(tokio_stream::wrappers::WatchStream::new(rx)))),
            Err(_) => Ok(None),
        }
    }
}

#[async_trait::async_trait]
impl DiscoverOutboundPolicy<OutboundDiscoverTarget> for OutboundDiscover {
    async fn get_outbound_policy(
        &self,
        OutboundDiscoverTarget {
            service_name,
            service_namespace,
            service_port,
            source_namespace,
        }: OutboundDiscoverTarget,
    ) -> Result<Option<OutboundPolicy>> {
        let rx = match self.0.write().outbound_policy_rx(
            service_name,
            service_namespace,
            service_port,
            source_namespace,
        ) {
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
        OutboundDiscoverTarget {
            service_name,
            service_namespace,
            service_port,
            source_namespace,
        }: OutboundDiscoverTarget,
    ) -> Result<Option<OutboundPolicyStream>> {
        match self.0.write().outbound_policy_rx(
            service_name,
            service_namespace,
            service_port,
            source_namespace,
        ) {
            Ok(rx) => Ok(Some(Box::pin(tokio_stream::wrappers::WatchStream::new(rx)))),
            Err(_) => Ok(None),
        }
    }

    fn lookup_ip(
        &self,
        addr: IpAddr,
        port: NonZeroU16,
        source_namespace: String,
    ) -> Option<OutboundDiscoverTarget> {
        self.0
            .read()
            .lookup_service(addr)
            .map(
                |outbound::ServiceRef { name, namespace }| OutboundDiscoverTarget {
                    service_name: name,
                    service_namespace: namespace,
                    service_port: port,
                    source_namespace,
                },
            )
    }
}
