use std::net::{IpAddr, Ipv4Addr, SocketAddr};

use anyhow::{anyhow, Context, Result};
use clap::Parser;
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream},
};
use tracing::{debug, error, info, Instrument};
use tracing_subscriber::prelude::*;
use tracing_subscriber::EnvFilter;

#[derive(Parser)]
#[clap(version)]
struct Args {
    #[clap(long, default_value = "4140")]
    outbound_port: u16,
    #[clap(
        parse(try_from_str),
        long,
        env = "LINKERD_VALIDATION_LOG_LEVEL",
        default_value = "debug"
    )]
    log_level: EnvFilter,
    #[clap(parse(try_from_str), long, env = "KUBERNETES_SERVICE_HOST")]
    target_host: Ipv4Addr,
    #[clap(parse(try_from_str), long, env = "KUBERNETES_SERVICE_PORT")]
    target_port: u16,
}

static REDIRECT_RESPONSE: &str = "REDIRECTION SUCCESSFUL";

#[tokio::main(flavor = "current_thread")]
async fn main() -> anyhow::Result<()> {
    let Args {
        outbound_port,
        log_level,
        target_host,
        target_port,
    } = Args::parse();

    tracing_subscriber::registry()
        .with(log_level)
        .with(tracing_subscriber::fmt::layer())
        .try_init()?;

    let target_addr = &format!("{}:{}", target_host, target_port);
    info!(original_dst = %target_addr, outbound_redirect = %outbound_port, "validating outbound traffic is redirected to proxy outbound port");

    let (shutdown_tx, shutdown_rx) = drain::channel();
    let (ready_tx, ready_rx) = tokio::sync::oneshot::channel::<Result<()>>();

    let srv_handle = tokio::spawn(async move {
        let listen_addr = SocketAddr::new(IpAddr::V4(Ipv4Addr::new(0, 0, 0, 0)), outbound_port);
        let listener = TcpListener::bind(listen_addr)
            .await
            .expect(&format!("Failed to bind server to {}", listen_addr));
        info!(server_addr=%listen_addr, "listening for incoming connections");

        if let Err(_) = ready_tx.send(Ok(())) {
            error!(server_addr=%listen_addr, "failed to send 'ready' signal, receiver dropped");
            return Err::<(), anyhow::Error>(anyhow!(
                "failed to bind server: listen_addr={}",
                listen_addr
            ));
        }

        let handler = tokio::spawn(async move {
            loop {
                let (mut stream, client_addr) = listener
                    .accept()
                    .await
                    .expect("failed to establish connection");
                info!(%client_addr, "accepted connection");
                tokio::spawn(async move {
                    let bytes = REDIRECT_RESPONSE.as_bytes();
                    match stream.write_all(&bytes).await {
                        Ok(_) => debug!(%client_addr, "wrote {:?} bytes", &bytes.len()),
                        Err(e) => error!(%client_addr, "failed to write bytes to client: {:?}", e),
                    }
                });
            }
        })
        .instrument(tracing::info_span!("srv_handler"));

        tokio::select! {
            _shutdown = shutdown_rx.signaled() => {
                return Ok(())
            }
            _v = handler => {
                return Ok(())
            }
        };
    })
    .instrument(tracing::info_span!("validation_server"));

    let client_handle = tokio::spawn(async move {
        let timeout = std::time::Duration::from_secs(120);
        if let Err(_) = tokio::time::timeout(timeout, ready_rx).await {
            return Err::<(), anyhow::Error>(anyhow!(
                "timed-out (120s) waiting for server to be ready"
            ));
        };

        let target_addr = SocketAddr::new(IpAddr::V4(target_host), target_port);
        let mut stream = {
            debug!(original_dst = %target_addr, "building validation client");
            let socket = TcpStream::connect(target_addr)
                .await
                .with_context(|| format!("failed to connect: {}", target_addr))?;
            let peer = socket.peer_addr()?;
            debug!(original_dst = %peer, "client connected to validation server");
            socket
        };

        tokio::select! {
            is_readable = stream.readable() => {
                if let Err(e) = is_readable {
                    anyhow::bail!("cannot read off client socket {}", e);
                }
            }

            () = tokio::time::sleep(timeout) => {
                anyhow::bail!("timed-out (120s) waiting for socket to become readable");
            }
        };

        let mut buf = [0u8; 100];
        let read_sz = stream.read(&mut buf[..REDIRECT_RESPONSE.len()]).await?;
        let resp = String::from_utf8(buf[..REDIRECT_RESPONSE.len()].to_vec())?;
        debug!(redirect_response=%resp, "read {} bytes", read_sz);

        if resp == REDIRECT_RESPONSE {
            return Ok(());
        } else {
            anyhow::bail!(
                "expected client to receive {:?}, got {:?} instead",
                REDIRECT_RESPONSE,
                resp,
            );
        }
    })
    .instrument(tracing::info_span!("validation_client"));

    let result = client_handle.await?;
    match result {
        Ok(_) => {
            info!("passed validation: iptables rules are properly set-up");
            shutdown_server(shutdown_tx).await;
        }
        Err(e) => {
            error!("failed validation: {}", e);
            shutdown_server(shutdown_tx).await;
            // Exit with ERRNO 111, 'Connection refused' if server does not validate
            // rules within 2 minutes, connection is unsuccessful, or resp &
            // req bytes don't match.
            std::process::exit(111);
        }
    }

    srv_handle.await??;

    Ok(())
}

#[tracing::instrument(name = "shutdown", skip(shutdown_tx))]
async fn shutdown_server(shutdown_tx: drain::Signal) {
    info!("sending shutdown signal to server");
    shutdown_tx.drain().await
}
