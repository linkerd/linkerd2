#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::{bail, Context, Result};
use clap::Parser;
use futures::prelude::*;
use kubert::shutdown;
use linkerd_policy_controller::admission;
use linkerd_policy_controller::k8s::DefaultPolicy;
use linkerd_policy_controller_core::IpNet;
use std::net::SocketAddr;
use tokio::time;
use tracing::{info, instrument};

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

#[derive(Debug, Parser)]
#[clap(name = "policy", about = "A policy resource prototype")]
struct Args {
    #[clap(
        parse(try_from_str),
        long,
        default_value = "linkerd=info,warn",
        env = "LINKERD_POLICY_CONTROLLER_LOG"
    )]
    log_level: kubert::log::EnvFilter,

    #[clap(long, default_value = "plain")]
    log_format: kubert::log::LogFormat,

    #[clap(flatten)]
    client: kubert::ClientArgs,

    #[clap(flatten)]
    server: kubert::server::ServerArgs,

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
}

#[tokio::main]
async fn main() -> Result<()> {
    let Args {
        client,
        admin,
        grpc_addr,
        server,
        admission_controller_disabled,
        identity_domain,
        cluster_networks: IpNets(cluster_networks),
        default_policy,
        log_level,
        log_format,
        control_plane_namespace,
    } = Args::parse();

    log_format.try_init(log_level)?;

    // Spawn an admin server, failing readiness checks until the index is updated.
    let admin = admin.spawn();
    let readiness = admin.readiness();

    let (shutdown, drain_rx) = shutdown::channel();

    // Load a Kubernetes client from the environment (check for in-cluster configuration first).
    let client = client
        .try_client()
        .await
        .context("failed to initialize kubernetes client")?;

    // Index cluster resources, returning a handle that supports lookups for the gRPC server.
    let handle = {
        const DETECT_TIMEOUT: time::Duration = time::Duration::from_secs(10);
        let cluster = linkerd_policy_controller::k8s::ClusterInfo {
            networks: cluster_networks.clone(),
            identity_domain,
            control_plane_ns: control_plane_namespace,
        };
        let (handle, index) =
            linkerd_policy_controller::k8s::Index::new(cluster, default_policy, DETECT_TIMEOUT);

        tokio::spawn(index.run(client.clone(), readiness.clone()));
        handle
    };

    // Run the gRPC server, serving results by looking up against the index handle.
    tokio::spawn(grpc(grpc_addr, cluster_networks, handle, drain_rx.clone()));

    if admission_controller_disabled {
        tracing::info!("Admission controller disabled");
    } else {
        let srv = server
            .spawn(admission::Service { client }, drain_rx.clone())
            .await?;
        info!(addr = %srv.local_addr(), "Admission controller server listening");
    }

    // Spawn a task that emits a log message when shutdown starts. It also flips the readiness so
    // the instance falls out of its Service.
    tokio::spawn(async move {
        let release = drain_rx.signaled().await;
        info!("Shutdown signaled");
        readiness.set(false);
        drop(release);
    });

    // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
    // complete before exiting.
    if shutdown
        .on_signal()
        .await
        .expect("Shutdown signal must register")
        .is_aborted()
    {
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
