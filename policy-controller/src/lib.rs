#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]
mod admission;
mod cli;
pub mod index_list;
pub use self::admission::Admission;
pub use linkerd_policy_controller_core::IpNet;
pub use linkerd_policy_controller_grpc as grpc;
pub use linkerd_policy_controller_k8s_api as k8s;
pub use linkerd_policy_controller_k8s_index::{inbound, outbound, ClusterInfo, DefaultPolicy};

use anyhow::{bail, Result};
use clap::Parser;
use futures::prelude::*;
use index_list::IndexList;
use k8s::{api::apps::v1::Deployment, Client, ObjectMeta, Resource};
use k8s_openapi::api::coordination::v1 as coordv1;
use kube::{api::PatchParams, runtime::watcher};
use kubert::LeaseManager;
use linkerd_policy_controller_core::inbound::{
    DiscoverInboundServer, InboundServer, InboundServerStream,
};
use linkerd_policy_controller_core::outbound::{
    DiscoverOutboundPolicy, OutboundDiscoverTarget, OutboundPolicy, OutboundPolicyStream,
};
use linkerd_policy_controller_k8s_status::{self as status};
use std::{net::IpAddr, net::SocketAddr, num::NonZeroU16, sync::Arc};
use tokio::{sync::mpsc, time::Duration};
use tonic::transport::Server;
use tracing::{info, info_span, instrument, Instrument};

const DETECT_TIMEOUT: Duration = Duration::from_secs(10);
const LEASE_DURATION: Duration = Duration::from_secs(30);
const LEASE_NAME: &str = "policy-controller-write";
const RENEW_GRACE_PERIOD: Duration = Duration::from_secs(1);
#[derive(Clone, Debug)]
pub struct InboundDiscover(inbound::SharedIndex);

#[derive(Clone, Debug)]
pub struct OutboundDiscover(outbound::SharedIndex);

pub struct Controller {
    pub runtime: kubert::Runtime<Option<kubert::server::Bound>>,
    pub inbound_index: inbound::SharedIndex,
    pub outbound_index: outbound::SharedIndex,
    pub status_index: status::SharedIndex,
    pub grpc_addr: SocketAddr,
    pub cluster_info: Arc<ClusterInfo>,
}

// === impl Controller ===

impl Controller {
    /// Runs the policy controller, returning an error if the runtime is aborted.
    pub async fn run(mut self) -> Result<()> {
        self.spawn_inbound_watches();
        self.spawn_outbound_watches();
        self.spawn_shared_watches();

        // Run the gRPC server, serving results by looking up against the index handle.
        tokio::spawn(grpc(
            self.grpc_addr,
            self.cluster_info.dns_domain.clone(),
            self.cluster_info.networks.clone(),
            self.inbound_index,
            self.outbound_index,
            self.runtime.shutdown_handle(),
        ));

        let client = self.runtime.client();
        let runtime = self.runtime.spawn_server(|| Admission::new(client));

        // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
        // complete before exiting.
        if runtime.run().await.is_err() {
            bail!("Aborted");
        }

        Ok(())
    }

    pub async fn from_default_args() -> Result<Self> {
        Self::from_args(cli::Args::parse()).await
    }

    // TODO(eliza): this could be made public if we choose to make the
    // `cli::Args` type public
    async fn from_args(args: cli::Args) -> Result<Self> {
        let runtime = args.runtime().await?;
        let cluster_info = args.cluster_info()?;

        let hostname =
            std::env::var("HOSTNAME").expect("Failed to fetch `HOSTNAME` environment variable");
        let params = kubert::lease::ClaimParams {
            lease_duration: LEASE_DURATION,
            renew_grace_period: RENEW_GRACE_PERIOD,
        };

        let lease = init_lease(
            runtime.client(),
            &args.control_plane_namespace,
            &args.policy_deployment_name,
        )
        .await?;
        let (claims, _task) = lease.spawn(hostname.clone(), params).await?;

        // Build the API index data structures which will maintain information
        // necessary for serving the inbound policy and outbound policy gRPC APIs.
        let inbound_index = inbound::Index::shared(cluster_info.clone());
        let outbound_index = outbound::Index::shared(cluster_info.clone());

        // Build the status index which will maintain information necessary for
        // updating the status field of policy resources.
        let (updates_tx, updates_rx) = mpsc::unbounded_channel();
        let status_index = status::Index::shared(hostname.clone(), claims.clone(), updates_tx);

        // Spawn the status Controller reconciliation.
        tokio::spawn(
            status::Index::run(status_index.clone()).instrument(info_span!("status::Index")),
        );

        let client = runtime.client();
        let status_controller = status::Controller::new(claims, client, hostname, updates_rx);
        tokio::spawn(
            status_controller
                .run()
                .instrument(info_span!("status::Controller")),
        );

        Ok(Self {
            runtime,
            inbound_index,
            outbound_index,
            status_index,
            cluster_info,
            grpc_addr: args.grpc_addr,
        })
    }

    /// Spawn watcher tasks for inbound-only resources.
    ///
    /// This spawns watches for the following resource types:
    ///
    /// - Pods
    /// - Servers
    /// - ServerAuthorizations
    /// - AuthorizationPolicies
    /// - MeshTLSAuthentications
    /// - NetworkAuthentications
    pub fn spawn_inbound_watches(&mut self) {
        let pods = self.runtime.watch_all::<k8s::Pod>(
            watcher::Config::default().labels("linkerd.io/control-plane-ns"),
        );
        tokio::spawn(
            kubert::index::namespaced(self.inbound_index.clone(), pods)
                .instrument(info_span!("pods")),
        );

        let servers = self
            .runtime
            .watch_all::<k8s::policy::Server>(watcher::Config::default());
        let servers_indexes = IndexList::new(self.inbound_index.clone())
            .push(self.status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(servers_indexes, servers).instrument(info_span!("servers")),
        );

        let server_authzs = self
            .runtime
            .watch_all::<k8s::policy::ServerAuthorization>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(self.inbound_index.clone(), server_authzs)
                .instrument(info_span!("serverauthorizations")),
        );

        let authz_policies = self
            .runtime
            .watch_all::<k8s::policy::AuthorizationPolicy>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(self.inbound_index.clone(), authz_policies)
                .instrument(info_span!("authorizationpolicies")),
        );

        let mtls_authns = self
            .runtime
            .watch_all::<k8s::policy::MeshTLSAuthentication>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(self.inbound_index.clone(), mtls_authns)
                .instrument(info_span!("meshtlsauthentications")),
        );

        let network_authns = self
            .runtime
            .watch_all::<k8s::policy::NetworkAuthentication>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(self.inbound_index.clone(), network_authns)
                .instrument(info_span!("networkauthentications")),
        );
    }

    /// Spawn watches on resources only needed by the outbound index.
    ///
    /// This spawns watches for the following resource types:
    ///
    /// - Services
    pub fn spawn_outbound_watches(&mut self) {
        let services = self
            .runtime
            .watch_all::<k8s::Service>(watcher::Config::default());
        let services_indexes = IndexList::new(self.outbound_index.clone())
            .push(self.status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(services_indexes, services)
                .instrument(info_span!("services")),
        );
    }

    /// Spawn watches for resource types needed by both the inbound and outbound
    /// indices.
    ///
    /// This spawns watches for the following resource types:
    ///
    /// - policy.linkerd.io.HTTPRoutes
    /// - gateway.networking.k8s.io.HTTPRoutes
    ///
    pub fn spawn_shared_watches(&mut self) {
        let http_routes = self
            .runtime
            .watch_all::<k8s::policy::HttpRoute>(watcher::Config::default());
        let http_routes_indexes = IndexList::new(self.inbound_index.clone())
            .push(self.outbound_index.clone())
            .push(self.status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(http_routes_indexes.clone(), http_routes)
                .instrument(info_span!("httproutes.policy.linkerd.io")),
        );

        let gateway_http_routes = self
            .runtime
            .watch_all::<k8s_gateway_api::HttpRoute>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(http_routes_indexes, gateway_http_routes)
                .instrument(info_span!("httproutes.gateway.networking.k8s.io")),
        );
    }
}

// === impl Args ===

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
impl DiscoverInboundServer<(String, String, NonZeroU16)> for InboundDiscover {
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

#[instrument(skip_all, fields(port = %addr.port()))]
async fn grpc(
    addr: SocketAddr,
    cluster_domain: String,
    cluster_networks: Vec<IpNet>,
    inbound_index: inbound::SharedIndex,
    outbound_index: outbound::SharedIndex,
    drain: drain::Watch,
) -> Result<()> {
    let inbound_discover = InboundDiscover::new(inbound_index);
    let inbound_svc =
        grpc::inbound::InboundPolicyServer::new(inbound_discover, cluster_networks, drain.clone())
            .svc();

    let outbound_discover = OutboundDiscover::new(outbound_index);
    let outbound_svc =
        grpc::outbound::OutboundPolicyServer::new(outbound_discover, cluster_domain, drain.clone())
            .svc();

    let (close_tx, close_rx) = tokio::sync::oneshot::channel();
    tokio::pin! {
        let srv = Server::builder().add_service(inbound_svc).add_service(outbound_svc).serve_with_shutdown(addr, close_rx.map(|_| {}));
    }

    info!(%addr, "policy gRPC server listening");
    tokio::select! {
        res = (&mut srv) => res?,
        handle = drain.signaled() => {
            let _ = close_tx.send(());
            handle.release_after(srv).await?
        }
    }
    Ok(())
}

async fn init_lease(client: Client, ns: &str, deployment_name: &str) -> Result<LeaseManager> {
    // Fetch the policy-controller deployment so that we can use it as an owner
    // reference of the Lease.
    let api = k8s::Api::<Deployment>::namespaced(client.clone(), ns);
    let deployment = api.get(deployment_name).await?;

    let api = k8s::Api::namespaced(client, ns);
    let params = PatchParams {
        field_manager: Some("policy-controller".to_string()),
        ..Default::default()
    };
    match api
        .patch(
            LEASE_NAME,
            &params,
            &kube::api::Patch::Apply(coordv1::Lease {
                metadata: ObjectMeta {
                    name: Some(LEASE_NAME.to_string()),
                    namespace: Some(ns.to_string()),
                    // Specifying a resource version of "0" means that we will
                    // only create the Lease if it does not already exist.
                    resource_version: Some("0".to_string()),
                    owner_references: Some(vec![deployment.controller_owner_ref(&()).unwrap()]),
                    labels: Some(
                        [
                            (
                                "linkerd.io/control-plane-component".to_string(),
                                "destination".to_string(),
                            ),
                            ("linkerd.io/control-plane-ns".to_string(), ns.to_string()),
                        ]
                        .into_iter()
                        .collect(),
                    ),
                    ..Default::default()
                },
                spec: None,
            }),
        )
        .await
    {
        Ok(lease) => tracing::info!(?lease, "created Lease resource"),
        Err(k8s::Error::Api(_)) => tracing::info!("Lease already exists, no need to create it"),
        Err(error) => {
            tracing::error!(%error, "error creating Lease resource");
            return Err(error.into());
        }
    };
    // Create the lease manager used for trying to claim the policy
    // controller write lease.
    // todo: Do we need to use LeaseManager::field_manager here?
    kubert::lease::LeaseManager::init(api, LEASE_NAME)
        .await
        .map_err(Into::into)
}
