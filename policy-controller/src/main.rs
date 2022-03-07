#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::{bail, Result};
use clap::Parser;
use futures::prelude::*;
use kube::api::ListParams;
use linkerd_policy_controller::{k8s, Admission};
use linkerd_policy_controller_core::IpNet;
use std::net::SocketAddr;
use tokio::time;
use tracing::{info, instrument};

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

const DETECT_TIMEOUT: time::Duration = time::Duration::from_secs(10);

#[derive(Debug, Parser)]
#[clap(name = "policy", about = "A policy resource prototype")]
struct Args {
    #[clap(
        parse(try_from_str),
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
    default_policy: k8s::DefaultPolicy,

    #[clap(long, default_value = "linkerd")]
    control_plane_namespace: String,
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

    // Build the index data structure, which will be used to process events from all watches
    // The lookup handle is used by the gRPC server.
    let (lookup, index) = {
        let cluster = k8s::ClusterInfo {
            networks: cluster_networks.clone(),
            identity_domain,
            control_plane_ns: control_plane_namespace,
        };
        k8s::Index::new(cluster, default_policy, DETECT_TIMEOUT)
    };

    // Spawn resource indexers that update the index and publish lookups for the gRPC server.

    let pods = runtime.watch_all(ListParams::default().labels("linkerd.io/control-plane-ns"));
    tokio::spawn(k8s::pod::index(index.clone(), pods));

    let servers = runtime.watch_all(ListParams::default());
    tokio::spawn(k8s::server::index(index.clone(), servers));

    let server_authzs = runtime.watch_all(ListParams::default());
    tokio::spawn(k8s::server_authorization::index(
        index.clone(),
        server_authzs,
    ));

    // Run the gRPC server, serving results by looking up against the index handle.
    tokio::spawn(grpc(
        grpc_addr,
        cluster_networks,
        lookup,
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

#[derive(Debug)]
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

#[instrument(skip(handle, drain))]
async fn grpc(
    addr: SocketAddr,
    cluster_networks: Vec<IpNet>,
    handle: linkerd_policy_controller_k8s_index::Reader,
    drain: drain::Watch,
) -> Result<()> {
    let server =
        linkerd_policy_controller_grpc::Server::new(handle, cluster_networks, drain.clone());
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
