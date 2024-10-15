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
    DiscoverOutboundPolicy, OutboundDiscoverTarget, OutboundPolicy, OutboundPolicyStream,
    TargetKind,
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
        OutboundDiscoverTarget {
            kind,
            name,
            namespace,
            port,
            source_namespace,
            ..
        }: OutboundDiscoverTarget,
    ) -> Result<Option<OutboundPolicy>> {
        if let TargetKind::UnmeshedNetwork = kind {
            tracing::debug!("UnmeshedNetwork is not supported yet");
            return Ok(None);
        }

        let rx =
            match self
                .0
                .write()
                .outbound_policy_rx(name, namespace, port, source_namespace, kind)
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
        OutboundDiscoverTarget {
            kind,
            name,
            namespace,
            port,
            source_namespace,
        }: OutboundDiscoverTarget,
    ) -> Result<Option<OutboundPolicyStream>> {
        if let TargetKind::UnmeshedNetwork = kind {
            tracing::debug!("UnmeshedNetwork is not supported yet");
            return Ok(None);
        }

        match self
            .0
            .write()
            .outbound_policy_rx(name, namespace, port, source_namespace)
        {
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
        let index = self.0.read();
        // First try to lookup a service that we have indexed
        if let Some(outbound::ResourceRef { name, namespace }) = index.lookup_service(addr) {
            return Some(OutboundDiscoverTarget {
                kind: TargetKind::Service,
                name,
                namespace,
                port,
                source_namespace: source_namespace.clone(),
            });
        }

        // Next, check if a pod with this IP exists. If it does, return a None
        // so we can let the destinations controller serve the discovery request
        // as usual.
        // The reason we need to do that is because the proxy would prefer a policy
        // resolution over a profiles one. The precise logic that is being implemented
        // in the proxy at the time of this commit is the following:
        //
        // 1. The outbound discovery does two requests to both profiles and policy
        // 2. If policy is served, it is returned along with the profiles (https://github.com/linkerd/linkerd2-proxy/blob/53b528a38ff2eabe79ef4f6986e56e00eefb4c1c/linkerd/app/outbound/src/discover.rs#L106)
        // 3. If no policy is served, we synthesize one from the profiles result is there is one (https://github.com/linkerd/linkerd2-proxy/blob/53b528a38ff2eabe79ef4f6986e56e00eefb4c1c/linkerd/app/outbound/src/discover.rs#L122)
        // 4. Otherwise an original destination Policy is synthesized (https://github.com/linkerd/linkerd2-proxy/blob/53b528a38ff2eabe79ef4f6986e56e00eefb4c1c/linkerd/app/outbound/src/discover.rs#L144)
        // 3. Profiles are used only if the re is explicit profiles configuration or there is no policy (https://github.com/linkerd/linkerd2-proxy/blob/53b528a38ff2eabe79ef4f6986e56e00eefb4c1c/linkerd/app/outbound/src/sidecar.rs#L182)
        //
        // As we want to preserve the current behavior, we need to avoid serving any policy for known pods as
        // it will be in fact used by the proxy.
        if index.pod_exists(addr) {
            return None;
        }

        // Now try and look for an UnmeshedNetwork that matches this IP address.
        index
            .lookup_unmeshed_network(addr, source_namespace.clone())
            .map(
                |outbound::ResourceRef { name, namespace }| OutboundDiscoverTarget {
                    kind: TargetKind::UnmeshedNetwork,
                    name,
                    namespace,
                    port,
                    source_namespace,
                },
            )
    }
}
