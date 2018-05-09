use std::fmt;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{Duration, Instant};

use futures::Future;
use http;
use hyper;
use indexmap::IndexSet;
use tokio_core::reactor::Handle;
use tokio_io::{AsyncRead, AsyncWrite};
use tower_service::NewService;
use tower_h2;

use connection::{Connection, Peek};
use ctx::Proxy as ProxyCtx;
use ctx::transport::{Server as ServerCtx};
use drain;
use telemetry::Sensors;
use transport::GetOriginalDst;
use super::glue::{HttpBody, HttpBodyNewSvc, HyperServerSvc};
use super::protocol::Protocol;
use super::tcp;

/// A protocol-transparent Server!
///
/// This type can `serve` new connections, determine what protocol
/// the connection is speaking, and route it to the corresponding
/// service.
pub struct Server<S, B, G>
where
    S: NewService<Request=http::Request<HttpBody>>,
    S::Future: 'static,
    B: tower_h2::Body,
{
    disable_protocol_detection_ports: IndexSet<u16>,
    drain_signal: drain::Watch,
    executor: Handle,
    get_orig_dst: G,
    h1: hyper::server::Http,
    h2: tower_h2::Server<HttpBodyNewSvc<S>, Handle, B>,
    listen_addr: SocketAddr,
    new_service: S,
    proxy_ctx: Arc<ProxyCtx>,
    sensors: Sensors,
    tcp: tcp::Proxy,
}

impl<S, B, G> Server<S, B, G>
where
    S: NewService<
        Request = http::Request<HttpBody>,
        Response = http::Response<B>
    > + Clone + 'static,
    S::Future: 'static,
    S::Error: fmt::Debug,
    S::InitError: fmt::Debug,
    B: tower_h2::Body + 'static,
    G: GetOriginalDst,
{
    /// Creates a new `Server`.
    pub fn new(
        listen_addr: SocketAddr,
        proxy_ctx: Arc<ProxyCtx>,
        sensors: Sensors,
        get_orig_dst: G,
        stack: S,
        tcp_connect_timeout: Duration,
        disable_protocol_detection_ports: IndexSet<u16>,
        drain_signal: drain::Watch,
        executor: Handle,
    ) -> Self {
        let recv_body_svc = HttpBodyNewSvc::new(stack.clone());
        let tcp = tcp::Proxy::new(tcp_connect_timeout, sensors.clone(), &executor);
        Server {
            disable_protocol_detection_ports,
            drain_signal,
            executor: executor.clone(),
            get_orig_dst,
            h1: hyper::server::Http::new(),
            h2: tower_h2::Server::new(recv_body_svc, Default::default(), executor),
            listen_addr,
            new_service: stack,
            proxy_ctx,
            sensors,
            tcp,
        }
    }

    /// Handle a new connection.
    ///
    /// This will peek on the connection for the first bytes to determine
    /// what protocol the connection is speaking. From there, the connection
    /// will be mapped into respective services, and spawned into an
    /// executor.
    pub fn serve(&self, connection: Connection, remote_addr: SocketAddr) {
        let opened_at = Instant::now();

        // create Server context
        let orig_dst = connection.original_dst_addr(&self.get_orig_dst);
        let local_addr = connection.local_addr().unwrap_or(self.listen_addr);
        let srv_ctx = ServerCtx::new(
            &self.proxy_ctx,
            &local_addr,
            &remote_addr,
            &orig_dst,
        );

        // record telemetry
        let io = self.sensors.accept(connection, opened_at, &srv_ctx);

        // We are using the port from the connection's SO_ORIGINAL_DST to
        // determine whether to skip protocol detection, not any port that
        // would be found after doing discovery.
        let disable_protocol_detection = orig_dst
            .map(|addr| {
                self.disable_protocol_detection_ports.contains(&addr.port())
            })
            .unwrap_or(false);

        if disable_protocol_detection {
            trace!("protocol detection disabled for {:?}", orig_dst);
            let fut = tcp_serve(
                &self.tcp,
                io,
                srv_ctx,
                self.drain_signal.clone(),
            );
            self.executor.spawn(fut);
            return;
        }

        // try to sniff protocol
        let h1 = self.h1.clone();
        let h2 = self.h2.clone();
        let tcp = self.tcp.clone();
        let new_service = self.new_service.clone();
        let drain_signal = self.drain_signal.clone();
        let fut = io.peek()
            .map_err(|e| debug!("peek error: {}", e))
            .and_then(move |io| -> Box<Future<Item=(), Error=()>> {
                if let Some(proto) = Protocol::detect(io.peeked()) {
                    match proto {
                        Protocol::Http1 => {
                            trace!("transparency detected HTTP/1");

                            Box::new(new_service.new_service()
                                .map_err(|_| ())
                                .and_then(move |s| {
                                    let svc = HyperServerSvc::new(s, srv_ctx);
                                    drain_signal
                                        .watch(h1.serve_connection(io, svc), |conn| {
                                            conn.disable_keep_alive();
                                        })
                                        .map(|_| ())
                                        .map_err(|e| trace!("http1 server error: {:?}", e))
                                }))
                        },
                        Protocol::Http2 => {
                            trace!("transparency detected HTTP/2");

                            let set_ctx = move |request: &mut http::Request<()>| {
                                request.extensions_mut().insert(srv_ctx.clone());
                            };

                            let fut = drain_signal
                                .watch(h2.serve_modified(io, set_ctx), |conn| {
                                    conn.graceful_shutdown();
                                })
                                .map_err(|e| trace!("h2 server error: {:?}", e));

                            Box::new(fut)
                        }
                    }
                } else {
                    trace!("transparency did not detect protocol, treating as TCP");
                    tcp_serve(
                        &tcp,
                        io,
                        srv_ctx,
                        drain_signal,
                    )
                }
            });

        self.executor.spawn(fut);
    }
}

fn tcp_serve<T: AsyncRead + AsyncWrite + 'static>(
    tcp: &tcp::Proxy,
    io: T,
    srv_ctx: Arc<ServerCtx>,
    drain_signal: drain::Watch,
) -> Box<Future<Item=(), Error=()>> {
    let fut = tcp.serve(io, srv_ctx);

    // There's nothing to do when drain is signaled, we just have to hope
    // the sockets finish soon. However, the drain signal still needs to
    // 'watch' the TCP future so that the process doesn't close early.
    Box::new(drain_signal.watch(fut, |_| ()))
}
