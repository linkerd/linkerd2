#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::{bail, Context, Error, Result};
use futures::{future, prelude::*};
use linkerd_policy_controller::k8s::DefaultPolicy;
use linkerd_policy_controller::{admin, admission};
use linkerd_policy_controller_core::IpNet;
use std::net::SocketAddr;
use structopt::StructOpt;
use tokio::{sync::watch, time};
use tracing::{debug, info, info_span, instrument, Instrument};
use tracing_subscriber::{fmt::format, prelude::*, EnvFilter};

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

#[derive(Debug, StructOpt)]
#[structopt(name = "policy", about = "A policy resource prototype")]
struct Args {
    #[structopt(
        parse(try_from_str),
        long,
        default_value = "linkerd=info,warn",
        env = "LINKERD_POLICY_CONTROLLER_LOG"
    )]
    log_level: EnvFilter,

    #[structopt(long, default_value = "plain")]
    log_format: LogFormat,

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
    default_policy: DefaultPolicy,

    #[structopt(long, default_value = "linkerd")]
    control_plane_namespace: String,
}

#[derive(Clone, Debug)]
enum LogFormat {
    Json,
    Plain,
}

#[tokio::main]
async fn main() -> Result<()> {
    let Args {
        admin_addr,
        grpc_addr,
        admission_addr,
        identity_domain,
        cluster_networks: IpNets(cluster_networks),
        default_policy,
        log_level,
        log_format,
        control_plane_namespace,
    } = Args::from_args();

    log_init(log_level, log_format)?;

    let (drain_tx, drain_rx) = drain::channel();

    // Load a Kubernetes client from the environment (check for in-cluster configuration first).
    //
    // TODO support --kubeconfig and --context command-line arguments.
    let client = kube::Client::try_default()
        .await
        .context("failed to initialize kubernetes client")?;

    // Spawn an admin server, failing readiness checks until the index is updated.
    let (ready_tx, ready_rx) = watch::channel(false);
    tokio::spawn(admin::serve(admin_addr, ready_rx));

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

        tokio::spawn(index.run(client.clone(), ready_tx));
        handle
    };

    // Run the gRPC server, serving results by looking up against the index handle.
    tokio::spawn(grpc(grpc_addr, cluster_networks, handle, drain_rx.clone()));

    // Run the admission controller
    if let Some(bind_addr) = admission_addr {
        let (listen_addr, serve) = warp::serve(admission::routes(client))
            .tls()
            .cert_path("/var/run/linkerd/tls/tls.crt")
            .key_path("/var/run/linkerd/tls/tls.key")
            .bind_with_graceful_shutdown(bind_addr, async move {
                let _ = drain_rx.signaled().await;
            });
        info!(addr = %listen_addr, "Admission controller server listening");
        tokio::spawn(serve.instrument(info_span!("admission")));
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

fn log_init(filter: EnvFilter, format: LogFormat) -> Result<()> {
    let registry = tracing_subscriber::registry().with(filter);

    match format {
        LogFormat::Plain => registry.with(tracing_subscriber::fmt::layer()).try_init()?,

        LogFormat::Json => {
            let event_fmt = tracing_subscriber::fmt::format()
                // Configure the formatter to output JSON logs.
                .json()
                // Output the current span context as a JSON list.
                .with_span_list(true)
                // Don't output a field for the current span, since this
                // would duplicate information already in the span list.
                .with_current_span(false);

            // Use the JSON event formatter and the JSON field formatter.
            let fmt = tracing_subscriber::fmt::layer()
                .event_format(event_fmt)
                .fmt_fields(format::JsonFields::default());

            registry.with(fmt).try_init()?
        }
    };

    Ok(())
}

// === impl LogFormat ===

impl std::str::FromStr for LogFormat {
    type Err = Error;

    fn from_str(s: &str) -> Result<Self> {
        if s == "json" {
            Ok(Self::Json)
        } else if s == "plain" {
            Ok(Self::Plain)
        } else {
            bail!("invalid log format: {}", s)
        }
    }
}
