#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::{Context, Result};
use futures::{future, prelude::*};
use linkerd_policy_controller::k8s::DefaultAllow;
use linkerd_policy_controller_core::IpNet;
use std::net::SocketAddr;
use structopt::StructOpt;
use tokio::{sync::watch, time};
use tracing::{debug, info, instrument};
use warp::Filter;

#[derive(Debug, StructOpt)]
#[structopt(name = "policy", about = "A policy resource prototype")]
struct Args {
    #[structopt(long, default_value = "0.0.0.0:8080")]
    admin_addr: SocketAddr,

    #[structopt(long, default_value = "0.0.0.0:8090")]
    grpc_addr: SocketAddr,

    #[structopt(long)]
    admission_addr: Option<SocketAddr>,

    /// Network CIDRs of pod IPs.
    ///
    /// The default includes all private networks.
    #[structopt(
        long,
        default_value = "10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16"
    )]
    cluster_networks: IpNets,

    #[structopt(long, default_value = "cluster.local")]
    identity_domain: String,

    #[structopt(long, default_value = "all-unauthenticated")]
    default_allow: DefaultAllow,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt::init();

    let Args {
        admin_addr,
        grpc_addr,
        admission_addr,
        identity_domain,
        cluster_networks: IpNets(cluster_networks),
        default_allow,
    } = Args::from_args();

    let (drain_tx, drain_rx) = drain::channel();

    // Load a Kubernetes client from the environment (check for in-cluster configuration first).
    //
    // TODO support --kubeconfig and --context command-line arguments.
    let client = kube::Client::try_default()
        .await
        .context("failed to initialize kubernetes client")?;

    // Spawn an admin server, failing readiness checks until the index is updated.
    let (ready_tx, ready_rx) = watch::channel(false);
    tokio::spawn(linkerd_policy_controller::admin::serve(
        admin_addr, ready_rx,
    ));

    // Index cluster resources, returning a handle that supports lookups for the gRPC server.
    let handle = {
        const DETECT_TIMEOUT: time::Duration = time::Duration::from_secs(10);
        let (handle, index) = linkerd_policy_controller::k8s::Index::new(
            cluster_networks.clone(),
            identity_domain,
            default_allow,
            DETECT_TIMEOUT,
        );

        tokio::spawn(index.run(client.clone(), ready_tx));
        handle
    };

    // Run the gRPC server, serving results by looking up against the index handle.
    tokio::spawn(grpc(grpc_addr, cluster_networks, handle, drain_rx));

    // Run the admission controller
    let admission = linkerd_policy_controller::admission::Admission(client);

    if let Some(admission_addr) = admission_addr {
        let routes = warp::path::end()
            .and(warp::body::json())
            .and(warp::any().map(move || admission.clone()))
            .and_then(linkerd_policy_controller::admission::mutate_handler)
            .with(warp::trace::request());
        tokio::spawn(
            warp::serve(warp::post().and(routes))
                .tls()
                .cert_path("/var/run/linkerd/tls/tls.crt")
                .key_path("/var/run/linkerd/tls/tls.key")
                .run(admission_addr),
        );
    }

    // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
    // complete before exiting.
    shutdown(drain_tx).await;

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

async fn shutdown(drain: drain::Signal) {
    tokio::select! {
        _ = tokio::signal::ctrl_c() => {
            debug!("Received ctrl-c");
        },
        _ = sigterm() => {
            debug!("Received SIGTERM");
        }
    }
    info!("Shutting down");
    drain.drain().await;
}

async fn sigterm() {
    use tokio::signal::unix::{signal, SignalKind};
    match signal(SignalKind::terminate()) {
        Ok(mut term) => term.recv().await,
        _ => future::pending().await,
    };
}
