use std::{
    net::{IpAddr, SocketAddr},
    process::exit,
    sync::Arc,
    time,
};

use anyhow::{anyhow, Context, Result};
use clap::Parser;
use rand::distributions::{Alphanumeric, DistString};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream},
    sync::Notify,
};
use tracing::{debug, error, info, Instrument};

#[derive(Parser)]
#[clap(version)]
struct Args {
    #[clap(
        long,
        env = "LINKERD_VALIDATION_LOG_LEVEL",
        default_value = "linkerd_cni_validator=info,warn"
    )]
    log_level: kubert::LogFilter,

    #[clap(long, default_value = "plain")]
    log_format: kubert::LogFormat,

    #[clap(long, default_value = "0.0.0.0:4140")]
    outbound_proxy_addr: SocketAddr,

    #[clap(parse(try_from_str), long, env = "KUBERNETES_SERVICE_HOST")]
    target_ip: IpAddr,

    #[clap(parse(try_from_str), long, env = "KUBERNETES_SERVICE_PORT")]
    target_port: u16,

    #[clap(parse(try_from_str = parse_timeout), long, default_value = "120s")]
    timeout: std::time::Duration,
}

// ERRNO 95: Operation not supported
const UNSUCCESSFUL_EXIT_CODE: i32 = 95;

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<()> {
    let Args {
        log_level,
        log_format,
        outbound_proxy_addr,
        target_ip,
        target_port,
        timeout,
    } = Args::parse();

    log_format.try_init(log_level)?;
    let target_addr = SocketAddr::new(target_ip, target_port);

    let rng_resp = Alphanumeric.sample_string(&mut rand::thread_rng(), 8);
    info!(%outbound_proxy_addr, %target_addr, "Validating outbound traffic is redirected to proxy's outbound port");

    let (shutdown_tx, shutdown_rx) = kubert::shutdown::sigint_or_sigterm()?;
    let notify_ready = Arc::new(Notify::new());

    let server_task = tokio::spawn(outbound_serve(
        outbound_proxy_addr,
        notify_ready.clone(),
        shutdown_rx,
        rng_resp.clone(),
    ));

    let validation_task = tokio::spawn(validate_outbound_redirect(
        target_addr,
        notify_ready,
        timeout,
        rng_resp,
    ));

    tokio::select! {
        task = validation_task => {
            if let Err(error) = task.expect("Failed to run validator task") {
                error!(%error, "Failed to validate");
                exit(UNSUCCESSFUL_EXIT_CODE);
            }

            info!("Validation passed successfully...exiting");
            Ok(())
        }

        _ = shutdown_tx.signaled() => {
            if let Err(error) = server_task.await.expect("Failed to run outbound server task") {
                error!(%error, "Failed to validate outbound routing configuration");
            } else {
                error!("Failed to validate due to server terminating early");
            }

            exit(UNSUCCESSFUL_EXIT_CODE);
        }
    }
}

#[tracing::instrument(level = "debug", skip_all)]
async fn validate_outbound_redirect(
    target_addr: SocketAddr,
    ready: Arc<Notify>,
    timeout: time::Duration,
    expected_resp: String,
) -> Result<()> {
    if tokio::time::timeout(timeout, ready.notified())
        .await
        .is_err()
    {
        anyhow::bail!("timed-out ({:?}) waiting for server to be ready", timeout);
    }

    let mut stream = {
        debug!("Building validation client");
        let socket = TcpStream::connect(target_addr).await?;
        debug_assert_eq!(target_addr, socket.peer_addr().unwrap());
        debug!("Connection established");
        socket
    };

    tokio::select! {
        is_readable = stream.readable() => {
            is_readable.context("cannot read off client socket")?;
        }

        () = tokio::time::sleep(timeout) => {
            anyhow::bail!("timed-out ({:?}) waiting for socket to become readable", timeout);
        }
    };

    let mut buf = [0u8; 8];
    let read_sz = stream.read_exact(&mut buf).await?;
    let resp = String::from_utf8(buf[..expected_resp.len()].to_vec())?;
    debug!(redirect_response = %resp, bytes_read = %read_sz);
    anyhow::ensure!(
        resp == expected_resp,
        "expected client to receive {:?}, got {:?} instead",
        expected_resp,
        resp
    );
    Ok(())
}

#[tracing::instrument(name = "outbound_server", skip(ready, shutdown))]
async fn outbound_serve(
    listen_addr: SocketAddr,
    ready: Arc<Notify>,
    shutdown: kubert::shutdown::Watch,
    resp: String,
) -> Result<()> {
    let listener = TcpListener::bind(listen_addr)
        .await
        .expect("Failed to bind server");
    info!("Listening for incoming connections");

    ready.notify_one();

    tokio::select! {
        _ = accept(listener, resp).in_current_span() => unreachable!("`accept` function never returns"),
        _ = shutdown.signaled() => debug!("Received shutdown signal")
    }

    Ok(())
}

async fn accept(listener: TcpListener, resp: String) {
    loop {
        let (mut stream, client_addr) = listener
            .accept()
            .await
            .expect("Failed to establish connection");
        info!("Accepted connection");
        let rng_resp = resp.clone();
        let _ = tokio::spawn(
            async move {
                let resp_bytes = rng_resp.as_bytes();
                // We expect this write to complete instantaneously,
                // a timeout is not needed here.
                match stream.write_all(resp_bytes).await {
                    Ok(()) => debug!(written_bytes = resp_bytes.len()),
                    Err(error) => error!(%error, "Failed to write bytes to client"),
                }
            }
            .instrument(tracing::info_span!("conn", %client_addr)),
        );
    }
}

pub fn parse_timeout(s: &str) -> Result<time::Duration> {
    let s = s.trim();
    let (magnitude, unit) = if let Some(offset) = s.rfind(|c: char| c.is_digit(10)) {
        let (magnitude, unit) = s.split_at(offset + 1);
        let magnitude = magnitude.parse::<u64>()?;
        (magnitude, unit)
    } else {
        anyhow::bail!("{} does not contain a timeout duration value", s);
    };

    let mul = match unit {
        "" if magnitude == 0 => 0,
        "ms" => 1,
        "s" => 1000,
        "m" => 1000 * 60,
        "h" => 1000 * 60 * 60,
        "d" => 1000 * 60 * 60 * 24,
        _ => anyhow::bail!("invalid duration unit {} (expected one of 'ms', 's', 'm', 'h', or 'd')", unit),
    };

    let ms = magnitude
        .checked_mul(mul)
        .ok_or_else(|| anyhow!("Timeout value {} overflows when converted to 'ms'", s))?;
    Ok(time::Duration::from_millis(ms))
}

#[cfg(test)]
mod tests {
    use crate::parse_timeout;
    use std::time;

    #[test]
    fn test_parse_timeout_invalid() {
        assert!(parse_timeout("120").is_err());
        assert!(parse_timeout("s").is_err());
        assert!(parse_timeout("foobars").is_err());
        assert!(parse_timeout("18446744073709551615s").is_err())
    }

    #[test]
    fn test_parse_timeout_seconds() {
        assert_eq!(time::Duration::from_secs(0), parse_timeout("0").unwrap());
        assert_eq!(time::Duration::from_secs(0), parse_timeout("0ms").unwrap());
        assert_eq!(time::Duration::from_secs(0), parse_timeout("0s").unwrap());
        assert_eq!(time::Duration::from_secs(0), parse_timeout("0m").unwrap());

        assert_eq!(
            time::Duration::from_secs(120),
            parse_timeout("120s").unwrap()
        );
        assert_eq!(
            time::Duration::from_secs(120),
            parse_timeout("120000ms").unwrap()
        );
        assert_eq!(time::Duration::from_secs(120), parse_timeout("2m").unwrap());
        assert_eq!(
            time::Duration::from_secs(7200),
            parse_timeout("2h").unwrap()
        );
        assert_eq!(
            time::Duration::from_secs(172800),
            parse_timeout("2d").unwrap()
        );
    }
}
