pub use linkerd_policy_controller_core as core;
pub use linkerd_policy_controller_grpc as grpc;
pub use linkerd_policy_controller_k8s_api as k8s;
pub use linkerd_policy_controller_k8s_index as index;
pub use linkerd_policy_controller_k8s_status as status;

mod admission;
mod args;
mod index_list;
mod validation;

mod lease;
pub use self::args::Args;

use std::num::NonZeroU16;

#[derive(Clone, Debug)]
struct InboundDiscover(index::inbound::SharedIndex);

#[derive(Clone, Debug)]
struct OutboundDiscover(index::outbound::SharedIndex);

impl InboundDiscover {
    pub fn new(index: index::inbound::SharedIndex) -> Self {
        Self(index)
    }
}

impl OutboundDiscover {
    pub fn new(index: index::outbound::SharedIndex) -> Self {
        Self(index)
    }
}

#[async_trait::async_trait]
impl core::inbound::DiscoverInboundServer<(grpc::workload::Workload, NonZeroU16)>
    for InboundDiscover
{
    async fn get_inbound_server(
        &self,
        (workload, port): (grpc::workload::Workload, NonZeroU16),
    ) -> anyhow::Result<Option<core::inbound::InboundServer>> {
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
    ) -> anyhow::Result<Option<core::inbound::InboundServerStream>> {
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
impl
    core::outbound::DiscoverOutboundPolicy<
        core::outbound::ResourceTarget,
        core::outbound::OutboundDiscoverTarget,
    > for OutboundDiscover
{
    async fn get_outbound_policy(
        &self,
        resource: core::outbound::ResourceTarget,
    ) -> anyhow::Result<Option<core::outbound::OutboundPolicy>> {
        let rx = match self.0.write().outbound_policy_rx(resource.clone()) {
            Ok(rx) => rx,
            Err(error) => {
                tracing::error!(%error, "Failed to get outbound policy rx");
                return Ok(None);
            }
        };

        let policy = (*rx.borrow()).clone();
        Ok(Some(policy))
    }

    async fn watch_outbound_policy(
        &self,
        target: core::outbound::ResourceTarget,
    ) -> anyhow::Result<Option<core::outbound::OutboundPolicyStream>> {
        match self.0.write().outbound_policy_rx(target) {
            Ok(rx) => Ok(Some(Box::pin(tokio_stream::wrappers::WatchStream::new(rx)))),
            Err(_) => Ok(None),
        }
    }

    async fn watch_external_policy(&self) -> core::outbound::ExternalPolicyStream {
        Box::pin(tokio_stream::wrappers::WatchStream::new(
            self.0.read().fallback_policy_rx(),
        ))
    }

    fn lookup_ip(
        &self,
        addr: std::net::IpAddr,
        port: NonZeroU16,
        source_namespace: String,
    ) -> Option<core::outbound::OutboundDiscoverTarget> {
        let index = self.0.read();
        if let Some((namespace, name)) = index.lookup_service(addr) {
            return Some(core::outbound::OutboundDiscoverTarget::Resource(
                core::outbound::ResourceTarget {
                    name,
                    namespace,
                    port,
                    source_namespace,
                    kind: core::outbound::Kind::Service,
                },
            ));
        }

        if let Some((namespace, name)) = index.lookup_egress_network(addr, source_namespace.clone())
        {
            return Some(core::outbound::OutboundDiscoverTarget::Resource(
                core::outbound::ResourceTarget {
                    name,
                    namespace,
                    port,
                    source_namespace,
                    kind: core::outbound::Kind::EgressNetwork(std::net::SocketAddr::new(
                        addr,
                        port.into(),
                    )),
                },
            ));
        }

        if !index.is_address_in_cluster(addr) {
            return Some(core::outbound::OutboundDiscoverTarget::External(
                std::net::SocketAddr::new(addr, port.into()),
            ));
        }

        None
    }
}
