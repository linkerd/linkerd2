use std::sync::Arc;
use std::time::Duration;

use futures::{future, Future};
use tokio_connect::Connect;
use tokio_core::reactor::Handle;
use tokio_io::{AsyncRead, AsyncWrite};
use tokio_io::io::copy;

use control;
use ctx::transport::{Client as ClientCtx, Server as ServerCtx};
use telemetry::Sensors;
use timeout::Timeout;
use transport;

/// TCP Server Proxy
#[derive(Debug, Clone)]
pub struct Proxy {
    connect_timeout: Duration,
    executor: Handle,
    sensors: Sensors,
}

impl Proxy {
    /// Create a new TCP `Proxy`.
    pub fn new(connect_timeout: Duration, sensors: Sensors, executor: &Handle) -> Self {
        Self {
            connect_timeout,
            executor: executor.clone(),
            sensors,
        }
    }

    /// Serve a TCP connection, trying to forward it to its destination.
    pub fn serve<T>(&self, tcp_in: T, srv_ctx: Arc<ServerCtx>) -> Box<Future<Item=(), Error=()>>
    where
        T: AsyncRead + AsyncWrite + 'static,
    {
        let orig_dst = srv_ctx.orig_dst_if_not_local();

        // For TCP, we really have no extra information other than the
        // SO_ORIGINAL_DST socket option. If that isn't set, the only thing
        // to do is to drop this connection.
        let orig_dst = if let Some(orig_dst) = orig_dst {
            debug!(
                "tcp accepted, forwarding ({}) to {}",
                srv_ctx.remote,
                orig_dst,
            );
            orig_dst
        } else {
            debug!(
                "tcp accepted, no SO_ORIGINAL_DST to forward: remote={}",
                srv_ctx.remote,
            );
            return Box::new(future::ok(()));
        };

        let client_ctx = ClientCtx::new(
            &srv_ctx.proxy,
            &orig_dst,
            control::pb::proxy::common::Protocol::Tcp,
        );
        let c = Timeout::new(
            transport::Connect::new(orig_dst, &self.executor),
            self.connect_timeout,
            &self.executor,
        );
        let connect = self.sensors.connect(c, &client_ctx);

        let fut = connect.connect()
            .map_err(|e| debug!("tcp connect error: {:?}", e))
            .and_then(move |tcp_out| {
                let (in_r, in_w) = tcp_in.split();
                let (out_r, out_w) = tcp_out.split();

                copy(in_r, out_w)
                    .join(copy(out_r, in_w))
                    .map(|_| ())
                    .map_err(|e| debug!("tcp error: {}", e))
            });
        Box::new(fut)
    }
}
