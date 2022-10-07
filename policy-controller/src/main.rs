#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::{bail, Result};
use clap::Parser;
use futures::prelude::*;
use kube::api::ListParams;
use linkerd_policy_controller::{
    grpc, k8s, Admission, ClusterInfo, DefaultPolicy, Index, IndexDiscover, IpNet, SharedIndex,
};
use std::net::SocketAddr;
use tokio::time;
use tracing::{info, info_span, instrument, Instrument};

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

const DETECT_TIMEOUT: time::Duration = time::Duration::from_secs(10);

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

    #[clap(long, default_value = "all-unauthenticated")]
    default_policy: DefaultPolicy,

    #[clap(long, default_value = "linkerd")]
    control_plane_namespace: String,

    /// Network CIDRs of all expected probes.
    #[clap(long)]
    probe_networks: Option<IpNets>,
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
        cluster_networks: IpNets(cluster_networks),
        default_policy,
        control_plane_namespace,
        probe_networks,
    } = Args::parse();

    let server = if admission_controller_disabled {
        None
    } else {
        Some(server)
    };

    let mut runtime = kubert::Runtime::builder()
        .with_log(log_level, log_format)
        .with_admin(admin)
        .with_client(client)
        .with_optional_server(server)
        .build()
        .await?;

    let probe_networks = probe_networks.map(|IpNets(nets)| nets).unwrap_or_default();

    // Build the index data structure, which will be used to process events from all watches
    // The lookup handle is used by the gRPC server.
    let index = Index::shared(ClusterInfo {
        networks: cluster_networks.clone(),
        identity_domain,
        control_plane_ns: control_plane_namespace,
        default_policy,
        default_detect_timeout: DETECT_TIMEOUT,
        probe_networks,
    });

    // Spawn resource indexers that update the index and publish lookups for the gRPC server.

    let pods =
        runtime.watch_all::<k8s::Pod>(ListParams::default().labels("linkerd.io/control-plane-ns"));
    tokio::spawn(kubert::index::namespaced(index.clone(), pods).instrument(info_span!("pods")));

    let servers = runtime.watch_all::<k8s::policy::Server>(ListParams::default());
    tokio::spawn(
        kubert::index::namespaced(index.clone(), servers).instrument(info_span!("servers")),
    );

    let server_authzs =
        runtime.watch_all::<k8s::policy::ServerAuthorization>(ListParams::default());
    tokio::spawn(
        kubert::index::namespaced(index.clone(), server_authzs)
            .instrument(info_span!("serverauthorizations")),
    );

    let authz_policies =
        runtime.watch_all::<k8s::policy::AuthorizationPolicy>(ListParams::default());
    tokio::spawn(
        kubert::index::namespaced(index.clone(), authz_policies)
            .instrument(info_span!("authorizationpolicies")),
    );

    let mtls_authns =
        runtime.watch_all::<k8s::policy::MeshTLSAuthentication>(ListParams::default());
    tokio::spawn(
        kubert::index::namespaced(index.clone(), mtls_authns)
            .instrument(info_span!("meshtlsauthentications")),
    );

    let network_authns =
        runtime.watch_all::<k8s::policy::NetworkAuthentication>(ListParams::default());
    tokio::spawn(
        kubert::index::namespaced(index.clone(), network_authns)
            .instrument(info_span!("networkauthentications")),
    );

    let http_routes = runtime.watch_all::<k8s::policy::HttpRoute>(ListParams::default());
    tokio::spawn(
        kubert::index::namespaced(index.clone(), http_routes).instrument(info_span!("httproutes")),
    );

    // Run the gRPC server, serving results by looking up against the index handle.
    tokio::spawn(grpc(
        grpc_addr,
        cluster_networks,
        index,
        runtime.shutdown_handle(),
    ));

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
    cluster_networks: Vec<IpNet>,
    index: SharedIndex,
    drain: drain::Watch,
) -> Result<()> {
    let discover = IndexDiscover::new(index);
    let server = grpc::Server::new(discover, cluster_networks, drain.clone());
    let (close_tx, close_rx) = tokio::sync::oneshot::channel();
    tokio::pin! {
        let srv = server.serve(addr, close_rx.map(|_| {}));
    }
    info!(%addr, "gRPC server listening");
    tokio::select! {
        res = (&mut srv) => res?,
        handle = drain.signaled() => {
            let _ = close_tx.send(());
            handle.release_after(srv).await?
        }
    }
    Ok(())
}
