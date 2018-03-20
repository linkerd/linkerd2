use std::fmt;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{Duration, Instant};

use futures::Future;
use http;
use hyper;
use tokio_core::reactor::Handle;
use tower::NewService;
use tower_h2;

use conduit_proxy_controller_grpc::common;
use connection::Connection;
use ctx::Proxy as ProxyCtx;
use ctx::transport::{Server as ServerCtx};
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
pub struct Server<S: NewService, B: tower_h2::Body, G>
where
    S: NewService<Request=http::Request<HttpBody>>,
    S::Future: 'static,
{
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
        executor: Handle,
    ) -> Self {
        let recv_body_svc = HttpBodyNewSvc::new(stack.clone());
        let tcp = tcp::Proxy::new(tcp_connect_timeout, sensors.clone(), &executor);
        Server {
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
        let proxy_ctx = self.proxy_ctx.clone();

        // try to sniff protocol
        let sniff = [0u8; 32];
        let sensors = self.sensors.clone();
        let h1 = self.h1.clone();
        let h2 = self.h2.clone();
        let tcp = self.tcp.clone();
        let new_service = self.new_service.clone();
        let fut = connection
            .peek_future(sniff)
            .map_err(|_| ())
            .and_then(move |(connection, sniff, n)| -> Box<Future<Item=(), Error=()>> {
                if let Some(proto) = Protocol::detect(&sniff[..n]) {
                    let srv_ctx = ServerCtx::new(
                        &proxy_ctx,
                        &local_addr,
                        &remote_addr,
                        &orig_dst,
                        common::Protocol::Http,
                    );

                    // record telemetry
                    let io = sensors.accept(connection, opened_at, &srv_ctx);

                    match proto {
                        Protocol::Http1 => {
                            trace!("transparency detected HTTP/1");

                            Box::new(new_service.new_service()
                                .map_err(|_| ())
                                .and_then(move |s| {
                                    let svc = HyperServerSvc::new(s, srv_ctx);
                                    h1.serve_connection(io, svc)
                                        .map(|_| ())
                                        .map_err(|_| ())
                                }))
                        },
                        Protocol::Http2 => {
                            trace!("transparency detected HTTP/2");

                            let set_ctx = move |request: &mut http::Request<()>| {
                                request.extensions_mut().insert(srv_ctx.clone());
                            };
                            Box::new(h2.serve_modified(io, set_ctx).map_err(|_| ()))
                        }
                    }
                } else {
                    trace!("transparency did not detect protocol, treating as TCP");

                    let srv_ctx = ServerCtx::new(
                        &proxy_ctx,
                        &local_addr,
                        &remote_addr,
                        &orig_dst,
                        common::Protocol::Tcp,
                    );

                    // record telemetry
                    let tcp_in = sensors.accept(connection, opened_at, &srv_ctx);

                    tcp.serve(tcp_in, srv_ctx)
                }
            });

        self.executor.spawn(fut);
    }
}

