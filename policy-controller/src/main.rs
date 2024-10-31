#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::{bail, Result};
use clap::Parser;
use futures::prelude::*;
use k8s::{api::apps::v1::Deployment, Client, ObjectMeta, Resource};
use k8s_openapi::api::coordination::v1 as coordv1;
use kube::{api::PatchParams, runtime::watcher};
use kubert::LeaseManager;
use linkerd_policy_controller::{
    grpc, inbound, index_list::IndexList, k8s, outbound, Admission, ClusterInfo, DefaultPolicy,
    InboundDiscover, IpNet, OutboundDiscover,
};
use linkerd_policy_controller_k8s_api::gateway as k8s_gateway_api;
use linkerd_policy_controller_k8s_index::ports::parse_portset;
use linkerd_policy_controller_k8s_status::{self as status};
use prometheus_client::registry::Registry;
use std::{net::SocketAddr, sync::Arc};
use tokio::{sync::mpsc, time::Duration};
use tonic::transport::Server;
use tracing::{info, info_span, instrument, Instrument};

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

const DETECT_TIMEOUT: Duration = Duration::from_secs(10);
const LEASE_DURATION: Duration = Duration::from_secs(30);
const LEASE_NAME: &str = "policy-controller-write";
const RENEW_GRACE_PERIOD: Duration = Duration::from_secs(1);
const RECONCILIATION_PERIOD: Duration = Duration::from_secs(10);
// The maximum number of status patches to buffer. As a conservative estimate,
// we assume that sending a patch will take at least 1ms, so we set the buffer
// size to be the same as the reconciliation period in milliseconds.
const STATUS_UPDATE_QUEUE_SIZE: usize = RECONCILIATION_PERIOD.as_millis() as usize;

#[derive(Debug, Parser)]
#[clap(name = "policy", about = "A policy resource prototype")]
struct Args {
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

    #[clap(long, default_value = "5000")]
    patch_timeout_ms: u64,

    #[clap(long)]
    allow_l5d_request_headers: bool,

    #[clap(long, default_value = "linkerd-external")]
    global_external_network_namespace: String,
}

#[tokio::main]
async fn main() -> Result<()> {
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
        patch_timeout_ms,
        allow_l5d_request_headers,
        global_external_network_namespace,
    } = Args::parse();

    let server = if admission_controller_disabled {
        None
    } else {
        Some(server)
    };

    let probe_networks = probe_networks.map(|IpNets(nets)| nets).unwrap_or_default();
    let global_external_network_namespace = Arc::new(global_external_network_namespace);
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
        global_external_network_namespace,
    });

    // Build the API index data structures which will maintain information
    // necessary for serving the inbound policy and outbound policy gRPC APIs.
    let inbound_index = inbound::Index::shared(cluster_info.clone());
    let outbound_index = outbound::Index::shared(cluster_info.clone());

    let mut prom = <Registry>::default();
    let resource_status = prom.sub_registry_with_prefix("resource_status");
    let status_metrics = status::ControllerMetrics::register(resource_status);
    let status_index_metrcs = status::IndexMetrics::register(resource_status);

    outbound::metrics::register(
        prom.sub_registry_with_prefix("outbound_index"),
        outbound_index.clone(),
    );
    inbound::metrics::register(
        prom.sub_registry_with_prefix("inbound_index"),
        inbound_index.clone(),
    );

    let mut runtime = kubert::Runtime::builder()
        .with_log(log_level, log_format)
        .with_admin(admin.into_builder().with_prometheus(prom))
        .with_client(client)
        .with_optional_server(server)
        .build()
        .await?;

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

    // Build the status index which will maintain information necessary for
    // updating the status field of policy resources.
    let (updates_tx, updates_rx) = mpsc::channel(STATUS_UPDATE_QUEUE_SIZE);
    let status_index = status::Index::shared(
        hostname.clone(),
        claims.clone(),
        updates_tx,
        status_index_metrcs,
        cluster_networks.clone(),
    );

    // Spawn resource watches.

    let pods = runtime
        .watch_all::<k8s::Pod>(watcher::Config::default().labels("linkerd.io/control-plane-ns"));
    tokio::spawn(
        kubert::index::namespaced(inbound_index.clone(), pods).instrument(info_span!("pods")),
    );

    let external_workloads =
        runtime.watch_all::<k8s::external_workload::ExternalWorkload>(watcher::Config::default());
    tokio::spawn(
        kubert::index::namespaced(inbound_index.clone(), external_workloads)
            .instrument(info_span!("external_workloads")),
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

    let http_routes_indexes = IndexList::new(inbound_index.clone())
        .push(outbound_index.clone())
        .push(status_index.clone())
        .shared();

    if api_resource_exists::<k8s::policy::HttpRoute>(&runtime.client()).await {
        let http_routes = runtime.watch_all::<k8s::policy::HttpRoute>(watcher::Config::default());

        tokio::spawn(
            kubert::index::namespaced(http_routes_indexes.clone(), http_routes)
                .instrument(info_span!("httproutes.policy.linkerd.io")),
        );
    } else {
        tracing::warn!("httproutes.policy.linkerd.io resource kind not found, skipping watches");
    }

    if api_resource_exists::<k8s_gateway_api::HttpRoute>(&runtime.client()).await {
        let gateway_http_routes =
            runtime.watch_all::<k8s_gateway_api::HttpRoute>(watcher::Config::default());
        tokio::spawn(
            kubert::index::namespaced(http_routes_indexes, gateway_http_routes)
                .instrument(info_span!("httproutes.gateway.networking.k8s.io")),
        );
    } else {
        tracing::warn!(
            "httproutes.gateway.networking.k8s.io resource kind not found, skipping watches"
        );
    }

    if api_resource_exists::<k8s_gateway_api::GrpcRoute>(&runtime.client()).await {
        let gateway_grpc_routes =
            runtime.watch_all::<k8s_gateway_api::GrpcRoute>(watcher::Config::default());
        let gateway_grpc_routes_indexes = IndexList::new(outbound_index.clone())
            .push(inbound_index.clone())
            .push(status_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(gateway_grpc_routes_indexes.clone(), gateway_grpc_routes)
                .instrument(info_span!("grpcroutes.gateway.networking.k8s.io")),
        );
    } else {
        tracing::warn!(
            "grpcroutes.gateway.networking.k8s.io resource kind not found, skipping watches"
        );
    }

    if api_resource_exists::<k8s_gateway_api::TlsRoute>(&runtime.client()).await {
        let tls_routes = runtime.watch_all::<k8s_gateway_api::TlsRoute>(watcher::Config::default());
        let tls_routes_indexes = IndexList::new(status_index.clone())
            .push(outbound_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(tls_routes_indexes.clone(), tls_routes)
                .instrument(info_span!("tlsroutes.gateway.networking.k8s.io")),
        );
    } else {
        tracing::warn!(
            "tlsroutes.gateway.networking.k8s.io resource kind not found, skipping watches"
        );
    }

    if api_resource_exists::<k8s_gateway_api::TcpRoute>(&runtime.client()).await {
        let tcp_routes = runtime.watch_all::<k8s_gateway_api::TcpRoute>(watcher::Config::default());
        let tcp_routes_indexes = IndexList::new(status_index.clone())
            .push(outbound_index.clone())
            .shared();
        tokio::spawn(
            kubert::index::namespaced(tcp_routes_indexes.clone(), tcp_routes)
                .instrument(info_span!("tcproutes.gateway.networking.k8s.io")),
        );
    } else {
        tracing::warn!(
            "tcproutes.gateway.networking.k8s.io resource kind not found, skipping watches"
        );
    }

    let services = runtime.watch_all::<k8s::Service>(watcher::Config::default());
    let services_indexes = IndexList::new(outbound_index.clone())
        .push(status_index.clone())
        .shared();
    tokio::spawn(
        kubert::index::namespaced(services_indexes, services).instrument(info_span!("services")),
    );

    let egress_networks =
        runtime.watch_all::<k8s::policy::EgressNetwork>(watcher::Config::default());
    let egress_networks_indexes = IndexList::new(status_index.clone())
        .push(outbound_index.clone())
        .shared();
    tokio::spawn(
        kubert::index::namespaced(egress_networks_indexes, egress_networks)
            .instrument(info_span!("egressnetworks")),
    );

    // Spawn the status Controller reconciliation.
    tokio::spawn(
        status::Index::run(status_index.clone(), RECONCILIATION_PERIOD)
            .instrument(info_span!("status::Index")),
    );

    // Run the gRPC server, serving results by looking up against the index handle.
    tokio::spawn(grpc(
        grpc_addr,
        cluster_domain,
        cluster_networks,
        allow_l5d_request_headers,
        inbound_index,
        outbound_index,
        runtime.shutdown_handle(),
    ));

    let client = runtime.client();
    let status_controller = status::Controller::new(
        claims,
        client,
        hostname,
        updates_rx,
        Duration::from_millis(patch_timeout_ms),
        status_metrics,
    );
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
    allow_l5d_request_headers: bool,
    inbound_index: inbound::SharedIndex,
    outbound_index: outbound::SharedIndex,
    drain: drain::Watch,
) -> Result<()> {
    let inbound_discover = InboundDiscover::new(inbound_index);
    let inbound_svc =
        grpc::inbound::InboundPolicyServer::new(inbound_discover, cluster_networks, drain.clone())
            .svc();

    let outbound_discover = OutboundDiscover::new(outbound_index);
    let outbound_svc = grpc::outbound::OutboundPolicyServer::new(
        outbound_discover,
        cluster_domain,
        allow_l5d_request_headers,
        drain.clone(),
    )
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

async fn api_resource_exists<T>(client: &Client) -> bool
where
    T: Resource,
    T::DynamicType: Default,
{
    let dt = Default::default();
    let resources = client
        .list_api_group_resources(&T::api_version(&dt))
        .await
        .expect("Failed to list API group resources");
    resources.resources.iter().any(|r| r.kind == T::kind(&dt))
}
