#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use anyhow::{bail, Context, Error, Result};
use clap::Parser;
use futures::{future, prelude::*};
use linkerd_policy_controller::k8s::DefaultPolicy;
use linkerd_policy_controller::{admin, admission};
use linkerd_policy_controller_core::IpNet;
use std::net::SocketAddr;
use tokio::{sync::watch, time};
use tracing::{debug, error, info, info_span, instrument, Instrument};
use tracing_subscriber::{fmt::format, prelude::*, EnvFilter};

use std::convert::Infallible;
use std::{fs, io, sync};

use async_stream::stream;
use hyper::server::accept;
use hyper::service::make_service_fn;
use hyper::Server;
use tokio::net::TcpListener;
use tokio_rustls::TlsAcceptor;

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
    log_level: EnvFilter,

    #[clap(long, default_value = "plain")]
    log_format: LogFormat,

    #[clap(long, default_value = "0.0.0.0:8080")]
    admin_addr: SocketAddr,

    #[clap(long, default_value = "0.0.0.0:8090")]
    grpc_addr: SocketAddr,

    #[clap(long)]
    admission_addr: Option<SocketAddr>,

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
    } = Args::parse();

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

    if let Some(bind_addr) = admission_addr {
        let tcp = TcpListener::bind(&bind_addr).await?;
        let listen_addr = tcp.local_addr()?;
        // Prepare a long-running future stream to accept and serve clients.
        let incoming_tls_stream = stream! {
            loop {
                let socket = match tcp.accept().await {
                    Ok((socket, _)) => socket,
                    Err(err) => {
                        error!(%err, "Failed to accept connection");
                        continue;
                    }
                };
                let tls_cfg = {
                    // Load public certificate.
                    let certs = match load_certs("/var/run/linkerd/tls/tls.crt") {
                        Ok(certs) => certs,
                        Err(err) => {
                            error!(%err, "Failed to load certificate");
                            continue;
                        }
                    };
                    // Load private key.
                    let key = match load_private_key("/var/run/linkerd/tls/tls.key") {
                        Ok(key) => key,
                        Err(err) => {
                            error!(%err, "Failed to load key");
                            continue;
                        }
                    };
                    // Do not use client certificate authentication.
                    let mut cfg = match rustls::ServerConfig::builder()
                        .with_safe_defaults()
                        .with_no_client_auth()
                        .with_single_cert(certs, key) {
                        Ok(cfg) => cfg,
                        Err(err) => {
                            error!(%err, "Failed to configure TLS");
                            continue;
                        }
                    };
                    // Configure ALPN to accept HTTP/2, HTTP/1.1 in that order.
                    cfg.alpn_protocols = vec![b"h2".to_vec(), b"http/1.1".to_vec()];
                    sync::Arc::new(cfg)
                };
                let tls_acceptor = TlsAcceptor::from(tls_cfg);
                let stream = tls_acceptor.accept(socket);
                match stream.await {
                    Ok(stream) => yield Ok::<_, Infallible>(stream),
                    Err(e) => error!(%e, "TLS Error"),
                }
            }
        };

        let acceptor = accept::from_stream(incoming_tls_stream);

        let handler = admission::Service { client };
        let service = make_service_fn(move |_| {
            let handler = handler.clone();
            future::ok::<_, io::Error>(handler)
        });
        let server = Server::builder(acceptor).serve(service);
        info!(addr = %listen_addr, "Admission controller server listening");
        tokio::spawn(server.instrument(info_span!("admission")));
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

// Load public certificate from file.
fn load_certs(filename: &str) -> Result<Vec<rustls::Certificate>, String> {
    // Open certificate file.
    let certfile =
        fs::File::open(filename).map_err(|e| format!("failed to open {}: {}", filename, e))?;
    let mut reader = io::BufReader::new(certfile);

    // Load and return certificate.
    let certs = rustls_pemfile::certs(&mut reader)
        .map_err(|e| format!("failed to load certificate: {}", e))?;
    Ok(certs.into_iter().map(rustls::Certificate).collect())
}

// Load private key from file.
fn load_private_key(filename: &str) -> Result<rustls::PrivateKey, String> {
    // Open keyfile.
    let keyfile =
        fs::File::open(filename).map_err(|e| format!("failed to open {}: {}", filename, e))?;
    let mut reader = io::BufReader::new(keyfile);

    // Load and return a single private key.
    let keys = rustls_pemfile::rsa_private_keys(&mut reader)
        .map_err(|e| format!("failed to load private key: {}", e))?;
    if keys.len() != 1 {
        return Err("expected a single private key".into());
    }

    Ok(rustls::PrivateKey(keys[0].clone()))
}
