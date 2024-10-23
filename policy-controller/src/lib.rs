#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]
mod admission;
pub mod index_list;
mod validation;
pub use self::admission::Admission;
use anyhow::Result;
use futures::StreamExt;
use linkerd_policy_controller_core::inbound::{
    DiscoverInboundServer, InboundServer, InboundServerStream,
};
use linkerd_policy_controller_core::outbound::{
    DiscoverOutboundPolicy, Kind, OutboundDiscoverTarget, OutboundPolicyKind, OutboundPolicyStream,
    ParentMeta, ResourceOutboundPolicy, ResourceTarget,
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
impl DiscoverOutboundPolicy<OutboundDiscoverTarget> for OutboundDiscover {
    async fn get_outbound_policy(
        &self,
        target: OutboundDiscoverTarget,
    ) -> Result<Option<OutboundPolicyKind>> {
        match target {
            OutboundDiscoverTarget::Fallback(original_dst) => {
                Ok(Some(OutboundPolicyKind::Fallback(original_dst)))
            }
            OutboundDiscoverTarget::Resource(resource) => {
                let rx = match self.0.write().outbound_policy_rx(resource.clone()) {
                    Ok(rx) => rx,
                    Err(error) => {
                        tracing::error!(%error, "failed to get outbound policy rx");
                        return Ok(None);
                    }
                };
                let policy = (*rx.borrow()).clone();

                let resource = match (&policy.parent_meta, &resource.kind) {
                    (
                        ParentMeta::EgressNetwork(traffic_policy),
                        Kind::EgressNetwork(original_dst),
                    ) => ResourceOutboundPolicy::Egress {
                        traffic_policy: *traffic_policy,
                        original_dst: *original_dst,
                        policy: policy.clone(),
                    },

                    (ParentMeta::Service { authority }, Kind::EgressNetwork(_)) => {
                        ResourceOutboundPolicy::Service {
                            authority: authority.clone(),
                            policy,
                        }
                    }
                    (policy_kind, resource_kind) => {
                        anyhow::bail!(
                            "policy kind {:?} incorrect for resource kind: {:?}",
                            policy_kind,
                            resource_kind
                        );
                    }
                };
                Ok(Some(OutboundPolicyKind::Resource(resource)))
            }
        }
    }

    async fn watch_outbound_policy(
        &self,
        target: OutboundDiscoverTarget,
    ) -> Result<Option<OutboundPolicyStream>> {
        match target {
            OutboundDiscoverTarget::Fallback(original_dst) => {
                let rx = self.0.write().fallback_policy_rx();
                let stream = tokio_stream::wrappers::WatchStream::new(rx)
                    .map(move |_| OutboundPolicyKind::Fallback(original_dst));
                Ok(Some(Box::pin(stream)))
            }

            OutboundDiscoverTarget::Resource(resource) => {
                match self.0.write().outbound_policy_rx(resource.clone()) {
                    Ok(rx) => {
                        let stream = tokio_stream::wrappers::WatchStream::new(rx).filter_map(
                            move |policy| {
                                let resource = match (policy.parent_meta.clone(), resource.kind) {
                                    (
                                        ParentMeta::EgressNetwork(traffic_policy),
                                        Kind::EgressNetwork(original_dst),
                                    ) => Some(ResourceOutboundPolicy::Egress {
                                        traffic_policy,
                                        original_dst,
                                        policy: policy.clone(),
                                    }),

                                    (ParentMeta::Service { authority }, Kind::EgressNetwork(_)) => {
                                        Some(ResourceOutboundPolicy::Service { authority, policy })
                                    }
                                    (policy_kind, resource_kind) => {
                                        tracing::error!(
                                            "policy kind {:?} incorrect for resource kind: {:?}",
                                            policy_kind,
                                            resource_kind
                                        );
                                        None
                                    }
                                }
                                .map(OutboundPolicyKind::Resource);

                                futures::future::ready(resource)
                            },
                        );
                        Ok(Some(Box::pin(stream)))
                    }
                    Err(_) => Ok(None),
                }
            }
        }
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
