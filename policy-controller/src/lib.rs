#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]
mod admission;
pub mod index_list;
pub use self::admission::Admission;
pub use linkerd_policy_controller_core::IpNet;
pub use linkerd_policy_controller_grpc as grpc;
pub use linkerd_policy_controller_k8s_api as k8s;
pub use linkerd_policy_controller_k8s_index::{inbound, outbound, ClusterInfo, DefaultPolicy};

use anyhow::{bail, Result};
use linkerd_policy_controller_core::inbound::{
    DiscoverInboundServer, InboundServer, InboundServerStream,
};
use linkerd_policy_controller_core::outbound::{
    DiscoverOutboundPolicy, OutboundDiscoverTarget, OutboundPolicy, OutboundPolicyStream,
};
use clap::Parser;
use futures::prelude::*;
use k8s::{api::apps::v1::Deployment, Client, ObjectMeta, Resource};
use k8s_openapi::api::coordination::v1 as coordv1;
use kube::{api::PatchParams, runtime::watcher};
use kubert::LeaseManager;
use index_list::IndexList;
use linkerd_policy_controller_k8s_index::ports::parse_portset;
use linkerd_policy_controller_k8s_status::{self as status};
use std::{net::IpAddr, num::NonZeroU16, net::SocketAddr, sync::Arc};
use tokio::{sync::mpsc, time::Duration};
use tonic::transport::Server;
use tracing::{info, info_span, instrument, Instrument};

const DETECT_TIMEOUT: Duration = Duration::from_secs(10);
const LEASE_DURATION: Duration = Duration::from_secs(30);
const LEASE_NAME: &str = "policy-controller-write";
const RENEW_GRACE_PERIOD: Duration = Duration::from_secs(1);

#[derive(Debug, Parser)]
#[clap(name = "policy", about = "Linkerd 2 policy controller")]
pub struct Args {
    #[clap(
        long,
        default_value = "linkerd=info,warn",
        env = "LINKERD_POLICY_CONTROLLER_LOG"
    )]
    log_level: kubert::LogFilter,

    #[clap(long, default_value = "plain")]
    log_format: kubert::LogFormat,

    #[clap(flatten)]
    client: kubert::ClientArgs,

    #[clap(flatten)]
    server: kubert::ServerArgs,

    #[clap(flatten)]
    admin: kubert::AdminArgs,

    /// Disables the admission controller server.
    #[clap(long)]
    admission_controller_disabled: bool,

    #[clap(long, default_value = "0.0.0.0:8090")]
    grpc_addr: SocketAddr,

    /// Network CIDRs of pod IPs.
    ///
    /// The default includes all private networks.
    #[clap(
        long,
        default_value = "10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16"
    )]
    cluster_networks: IpNets,

    #[clap(long, default_value = "cluster.local")]
    identity_domain: String,

    #[clap(long, default_value = "cluster.local")]
    cluster_domain: String,

    #[clap(long, default_value = "all-unauthenticated")]
    default_policy: DefaultPolicy,

    #[clap(long, default_value = "linkerd-destination")]
    policy_deployment_name: String,

    #[clap(long, default_value = "linkerd")]
    control_plane_namespace: String,

    /// Network CIDRs of all expected probes.
    #[clap(long)]
    probe_networks: Option<IpNets>,

    #[clap(long)]
    default_opaque_ports: String,
}

#[derive(Clone, Debug)]
pub struct InboundDiscover(inbound::SharedIndex);

#[derive(Clone, Debug)]
pub struct OutboundDiscover(outbound::SharedIndex);

/// Runs the policy controller with arguments parsed from the process'
/// command-line arguments. This function constructs a multi-threaded Tokio
/// runtime.
#[tokio::main]
pub async fn run() -> Result<()> {
    Args::parse().run().await
}

// === impl Args ===

impl Args {
    /// Runs the policy controller with the provided [`Args`].
    pub async fn run(self) -> Result<()> {
        let Args {
            admin,
            client,
            log_level,
            log_format,
            server,
            grpc_addr,
            admission_controller_disabled,
            identity_domain,
            cluster_domain,
            cluster_networks: IpNets(cluster_networks),
            default_policy,
            policy_deployment_name,
            control_plane_namespace,
            probe_networks,
            default_opaque_ports,
        } = self;

        let server = if admission_controller_disabled {
            None
        } else {
            Some(server)
        };

        let mut admin = admin.into_builder();
        admin.with_default_prometheus();

        let mut runtime = kubert::Runtime::builder()
            .with_log(log_level, log_format)
            .with_admin(admin)
            .with_client(client)
            .with_optional_server(server)
            .build()
            .await?;

        let probe_networks = probe_networks.map(|IpNets(nets)| nets).unwrap_or_default();

        let default_opaque_ports = parse_portset(&default_opaque_ports)?;
        let cluster_info = Arc::new(ClusterInfo {
            networks: cluster_networks.clone(),
            identity_domain,
            control_plane_ns: control_plane_namespace.clone(),
            dns_domain: cluster_domain.clone(),
            default_policy,
            default_detect_timeout: DETECT_TIMEOUT,
            default_opaque_ports,
            probe_networks,
        });

        let hostname =
            std::env::var("HOSTNAME").expect("Failed to fetch `HOSTNAME` environment variable");
        let params = kubert::lease::ClaimParams {
            lease_duration: LEASE_DURATION,
            renew_grace_period: RENEW_GRACE_PERIOD,
        };

        let lease = init_lease(
            runtime.client(),
            &control_plane_namespace,
            &policy_deployment_name,
        )
        .await?;
        let (claims, _task) = lease.spawn(hostname.clone(), params).await?;

        // Build the API index data structures which will maintain information
        // necessary for serving the inbound policy and outbound policy gRPC APIs.
        let inbound_index = inbound::Index::shared(cluster_info.clone());
        let outbound_index = outbound::Index::shared(cluster_info);

        // Build the status index which will maintain information necessary for
        // updating the status field of policy resources.
        let (updates_tx, updates_rx) = mpsc::unbounded_channel();
        let status_index = status::Index::shared(hostname.clone(), claims.clone(), updates_tx);

        // Spawn resource watches.

        let pods = runtime
            .watch_all::<k8s::Pod>(watcher::Config::default().labels("linkerd.io/control-plane-ns"));
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), pods).instrument(info_span!("pods")),
        );

        let servers = runtime.watch_all::<k8s::policy::Server>(watcher::Config::default());
        let servers_indexes = IndexList::new(inbound_index.clone())
            .push(status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(servers_indexes, servers).instrument(info_span!("servers")),
        );

        let server_authzs =
            runtime.watch_all::<k8s::policy::ServerAuthorization>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), server_authzs)
                .instrument(info_span!("serverauthorizations")),
        );

        let authz_policies =
            runtime.watch_all::<k8s::policy::AuthorizationPolicy>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), authz_policies)
                .instrument(info_span!("authorizationpolicies")),
        );

        let mtls_authns =
            runtime.watch_all::<k8s::policy::MeshTLSAuthentication>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), mtls_authns)
                .instrument(info_span!("meshtlsauthentications")),
        );

        let network_authns =
            runtime.watch_all::<k8s::policy::NetworkAuthentication>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(inbound_index.clone(), network_authns)
                .instrument(info_span!("networkauthentications")),
        );

        let http_routes = runtime.watch_all::<k8s::policy::HttpRoute>(watcher::Config::default());
        let http_routes_indexes = IndexList::new(inbound_index.clone())
            .push(outbound_index.clone())
            .push(status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(http_routes_indexes.clone(), http_routes)
                .instrument(info_span!("httproutes.policy.linkerd.io")),
        );

        let gateway_http_routes =
            runtime.watch_all::<k8s_gateway_api::HttpRoute>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(http_routes_indexes, gateway_http_routes)
                .instrument(info_span!("httproutes.gateway.networking.k8s.io")),
        );

        let services = runtime.watch_all::<k8s::Service>(watcher::Config::default());
        let services_indexes = IndexList::new(outbound_index.clone())
            .push(status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(services_indexes, services).instrument(info_span!("services")),
        );

        // Spawn the status Controller reconciliation.
        tokio::spawn(status::Index::run(status_index.clone()).instrument(info_span!("status::Index")));

        // Run the gRPC server, serving results by looking up against the index handle.
        tokio::spawn(grpc(
            grpc_addr,
            cluster_domain,
            cluster_networks,
            inbound_index,
            outbound_index,
            runtime.shutdown_handle(),
        ));

        let client = runtime.client();
        let status_controller = status::Controller::new(claims, client, hostname, updates_rx);
        tokio::spawn(
            status_controller
                .run()
                .instrument(info_span!("status::Controller")),
        );

        let client = runtime.client();
        let runtime = runtime.spawn_server(|| Admission::new(client));

        // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
        // complete before exiting.
        if runtime.run().await.is_err() {
            bail!("Aborted");
        }

        Ok(())
    }
}

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


#[derive(Clone, Debug)]
struct IpNets(Vec<IpNet>);

impl std::str::FromStr for IpNets {
    type Err = anyhow::Error;
    fn from_str(s: &str) -> Result<Self> {
        s.split(',')
            .map(|n| n.parse().map_err(Into::into))
            .collect::<Result<Vec<IpNet>>>()
            .map(Self)
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
