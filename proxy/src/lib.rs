#![cfg_attr(feature = "cargo-clippy", allow(clone_on_ref_ptr))]
#![cfg_attr(feature = "cargo-clippy", allow(new_without_default_derive))]
#![deny(warnings)]

extern crate bytes;
extern crate conduit_proxy_controller_grpc;
extern crate convert;
extern crate env_logger;
extern crate deflate;
#[macro_use]
extern crate futures;
extern crate futures_mpsc_lossy;
extern crate h2;
extern crate http;
extern crate httparse;
extern crate hyper;
extern crate ipnet;
#[cfg(target_os = "linux")]
extern crate libc;
#[macro_use]
extern crate log;
#[cfg_attr(test, macro_use)]
extern crate indexmap;
extern crate prost;
extern crate prost_types;
#[cfg(test)]
#[macro_use]
extern crate quickcheck;
extern crate rand;
extern crate regex;
extern crate tokio;
extern crate tokio_connect;
extern crate tower_balance;
extern crate tower_buffer;
extern crate tower_discover;
extern crate tower_grpc;
extern crate tower_h2;
extern crate tower_reconnect;
extern crate tower_service;
extern crate conduit_proxy_router;
extern crate tower_util;
extern crate tower_in_flight_limit;
extern crate trust_dns_resolver;

use futures::*;

use std::error::Error;
use std::io;
use std::net::SocketAddr;
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use indexmap::IndexSet;
use tokio::{
    executor::{self, DefaultExecutor, Executor},
    runtime::current_thread,
};
use tower_service::NewService;
use tower_fn::*;
use conduit_proxy_router::{Recognize, Router, Error as RouteError};

pub mod app;
mod bind;
pub mod config;
mod connection;
pub mod control;
pub mod ctx;
mod dns;
mod drain;
mod inbound;
mod logging;
mod map_err;
mod outbound;
pub mod task;
pub mod telemetry;
mod transparency;
mod transport;
pub mod timeout;
mod tower_fn; // TODO: move to tower-fn
mod rng;

use bind::Bind;
use connection::BoundPort;
use inbound::Inbound;
use map_err::MapErr;
use task::MainRuntime;
use transparency::{HttpBody, Server};
pub use transport::{AddrInfo, GetOriginalDst, SoOriginalDst};
use outbound::Outbound;

/// Runs a sidecar proxy.
///
/// The proxy binds two listeners:
///
/// - a private socket (TCP or UNIX) for outbound requests to other instances;
/// - and a public socket (TCP and optionally TLS) for inbound requests from other
///   instances.
///
/// The public listener forwards requests to a local socket (TCP or UNIX).
///
/// The private listener routes requests to service-discovery-aware load-balancer.
///

pub struct Main<G> {
    config: config::Config,

    control_listener: BoundPort,
    inbound_listener: BoundPort,
    outbound_listener: BoundPort,
    metrics_listener: BoundPort,

    get_original_dst: G,

    runtime: MainRuntime,
}

impl<G> Main<G>
where
    G: GetOriginalDst + Clone + Send + 'static,
{
    pub fn new<R>(
        config: config::Config,
        get_original_dst: G,
        runtime: R
    ) -> Self
    where
        R: Into<MainRuntime>,
    {

        let control_listener = BoundPort::new(config.control_listener.addr)
            .expect("controller listener bind");
        let inbound_listener = BoundPort::new(config.public_listener.addr)
            .expect("public listener bind");
        let outbound_listener = BoundPort::new(config.private_listener.addr)
            .expect("private listener bind");

        let runtime = runtime.into();

        let metrics_listener = BoundPort::new(config.metrics_listener.addr)
            .expect("metrics listener bind");
        Main {
            config,
            control_listener,
            inbound_listener,
            outbound_listener,
            metrics_listener,
            get_original_dst,
            runtime,
        }
    }


    pub fn control_addr(&self) -> SocketAddr {
        self.control_listener.local_addr()
    }

    pub fn inbound_addr(&self) -> SocketAddr {
        self.inbound_listener.local_addr()
    }

    pub fn outbound_addr(&self) -> SocketAddr {
        self.outbound_listener.local_addr()
    }

    pub fn metrics_addr(&self) -> SocketAddr {
        self.metrics_listener.local_addr()
    }

    pub fn run_until<F>(self, shutdown_signal: F)
    where
        F: Future<Item = (), Error = ()> + Send + 'static,
    {
        let process_ctx = ctx::Process::new(&self.config);

        let Main {
            config,
            control_listener,
            inbound_listener,
            outbound_listener,
            metrics_listener,
            get_original_dst,
            mut runtime,
        } = self;

        let control_host_and_port = config.control_host_and_port.clone();

        info!("using controller at {:?}", control_host_and_port);
        info!("routing on {:?}", outbound_listener.local_addr());
        info!(
            "proxying on {:?} to {:?}",
            inbound_listener.local_addr(),
            config.private_forward
        );
        info!(
            "serving Prometheus metrics on {:?}",
            metrics_listener.local_addr(),
        );
        info!(
            "protocol detection disabled for inbound ports {:?}",
            config.inbound_ports_disable_protocol_detection,
        );
        info!(
            "protocol detection disabled for outbound ports {:?}",
            config.outbound_ports_disable_protocol_detection,
        );

        let (taps, observe) = control::Observe::new(100);
        let (sensors, telemetry) = telemetry::new(
            &process_ctx,
            config.event_buffer_capacity,
            config.metrics_retain_idle,
            &taps,
        );

        let dns_resolver = dns::Resolver::from_system_config()
            .unwrap_or_else(|e| {
                // TODO: Make DNS configuration infallible.
                panic!("invalid DNS configuration: {:?}", e);
            });

        let (control, control_bg) = control::destination::new(
            dns_resolver.clone(),
            config.pod_namespace.clone(),
            control_host_and_port
        );

        let (drain_tx, drain_rx) = drain::channel();

        let bind = Bind::new().with_sensors(sensors.clone());

        // Setup the public listener. This will listen on a publicly accessible
        // address and listen for inbound connections that should be forwarded
        // to the managed application (private destination).
        let inbound = {
            let ctx = ctx::Proxy::inbound(&process_ctx);

            let bind = bind.clone().with_ctx(ctx.clone());

            let default_addr = config.private_forward.map(|a| a.into());

            let router = Router::new(
                Inbound::new(default_addr, bind),
                config.inbound_router_capacity,
                config.inbound_router_max_idle_age,
            );
            serve(
                "inbound",
                inbound_listener,
                router,
                config.private_connect_timeout,
                config.inbound_ports_disable_protocol_detection,
                ctx,
                sensors.clone(),
                get_original_dst.clone(),
                drain_rx.clone(),
            )
        };

        // Setup the private listener. This will listen on a locally accessible
        // address and listen for outbound requests that should be routed
        // to a remote service (public destination).
        let outbound = {
            let ctx = ctx::Proxy::outbound(&process_ctx);
            let bind = bind.clone().with_ctx(ctx.clone());
            let router = Router::new(
                Outbound::new(bind, control, config.bind_timeout),
                config.outbound_router_capacity,
                config.outbound_router_max_idle_age,
            );
            serve(
                "outbound",
                outbound_listener,
                router,
                config.public_connect_timeout,
                config.outbound_ports_disable_protocol_detection,
                ctx,
                sensors,
                get_original_dst,
                drain_rx,
            )
        };

        trace!("running");

        let (_tx, controller_shutdown_signal) = futures::sync::oneshot::channel::<()>();
        {
            thread::Builder::new()
                .name("controller-client".into())
                .spawn(move || {
                    use conduit_proxy_controller_grpc::tap::server::TapServer;

                    let mut rt = current_thread::Runtime::new()
                        .expect("initialize controller-client thread runtime");

                    let new_service = TapServer::new(observe);

                    let server = serve_control(control_listener, new_service);

                    let metrics_server = telemetry
                        .serve_metrics(metrics_listener);

                    let fut = control_bg.join4(
                        server.map_err(|_| {}),
                        telemetry,
                        metrics_server.map_err(|_| {}),
                    ).map(|_| {});
                    let fut = ::logging::context_future("controller-client", fut);

                    rt.spawn(Box::new(fut));

                    let shutdown = controller_shutdown_signal.then(|_| Ok::<(), ()>(()));
                    rt.block_on(shutdown).expect("controller api");
                    trace!("controller client shutdown finished");
                })
                .expect("initialize controller api thread");
            trace!("controller client thread spawned");
        }

        let fut = inbound
            .join(outbound)
            .map(|_| ())
            .map_err(|err| error!("main error: {:?}", err));

        runtime.spawn(Box::new(fut));
        trace!("main task spawned");

        let shutdown_signal = shutdown_signal.and_then(move |()| {
            debug!("shutdown signaled");
            drain_tx.drain()
        });
        runtime.run_until(shutdown_signal).expect("executor");
        debug!("shutdown complete");
    }
}

fn serve<R, B, E, F, G>(
    name: &'static str,
    bound_port: BoundPort,
    router: Router<R>,
    tcp_connect_timeout: Duration,
    disable_protocol_detection_ports: IndexSet<u16>,
    proxy_ctx: Arc<ctx::Proxy>,
    sensors: telemetry::Sensors,
    get_orig_dst: G,
    drain_rx: drain::Watch,
) -> impl Future<Item = (), Error = io::Error> + Send + 'static
where
    B: tower_h2::Body + Default + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
    E: Error + Send + 'static,
    F: Error + Send + 'static,
    R: Recognize<
        Request = http::Request<HttpBody>,
        Response = http::Response<telemetry::sensor::http::ResponseBody<B>>,
        Error = E,
        RouteError = F,
    >
        + Send + Sync + 'static,
    R::Key: Send,
    R::Service: Send,
    <R::Service as tower_service::Service>::Future: Send,
    Router<R>: Send,
    G: GetOriginalDst + Send + 'static,
{
    let stack = Arc::new(NewServiceFn::new(move || {
        // Clone the router handle
        let router = router.clone();

        // Map errors to appropriate response error codes.
        let map_err = MapErr::new(router, |e| {
            match e {
                RouteError::Route(r) => {
                    error!(" turning route error: {} into 500", r);
                    http::StatusCode::INTERNAL_SERVER_ERROR
                }
                RouteError::Inner(i) => {
                    error!("turning {} into 500", i);
                    http::StatusCode::INTERNAL_SERVER_ERROR
                }
                RouteError::NotRecognized => {
                    error!("turning route not recognized error into 500");
                    http::StatusCode::INTERNAL_SERVER_ERROR
                }
                RouteError::NoCapacity(capacity) => {
                    // TODO For H2 streams, we should probably signal a protocol-level
                    // capacity change.
                    error!("router at capacity ({}); returning a 503", capacity);
                    http::StatusCode::SERVICE_UNAVAILABLE
                }
            }
        });

        // Install the request open timestamp module at the very top
        // of the stack, in order to take the timestamp as close as
        // possible to the beginning of the request's lifetime.
        telemetry::sensor::http::TimestampRequestOpen::new(map_err)
    }));

    let listen_addr = bound_port.local_addr();
    let server = Server::new(
        listen_addr,
        proxy_ctx,
        sensors,
        get_orig_dst,
        stack,
        tcp_connect_timeout,
        disable_protocol_detection_ports,
        drain_rx.clone(),
    );


    let accept = bound_port.listen_and_fold(
        (),
        move |(), (connection, remote_addr)| {
            let s = server.serve(connection, remote_addr);
            let s = ::logging::context_future((name, remote_addr), s);
            let r = DefaultExecutor::current()
                .spawn(Box::new(s))
                .map_err(task::Error::into_io);
            future::result(r)
        },
    );
    let accept = ::logging::context_future(name, accept);

    let accept_until = Cancelable {
        future: accept,
        canceled: false,
    };

    // As soon as we get a shutdown signal, the listener
    // is canceled immediately.
    drain_rx.watch(accept_until, |accept| {
        accept.canceled = true;
    })
}

/// Can cancel a future by setting a flag.
///
/// Used to 'watch' the accept futures, and close the listeners
/// as soon as the shutdown signal starts.
struct Cancelable<F> {
    future: F,
    canceled: bool,
}

impl<F> Future for Cancelable<F>
where
    F: Future<Item=()>,
{
    type Item = ();
    type Error = F::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        if self.canceled {
            Ok(().into())
        } else {
            self.future.poll()
        }
    }
}

fn serve_control<N, B>(
    bound_port: BoundPort,
    new_service: N,
) -> impl Future<Item = (), Error = io::Error> + 'static
where
    B: tower_h2::Body + Send + 'static,
    <B::Data as bytes::IntoBuf>::Buf: Send,
    N: NewService<
        Request = http::Request<tower_h2::RecvBody>,
        Response = http::Response<B>
    >
        + Send + 'static,
    tower_h2::server::Connection<
        connection::Connection,
        N,
        task::LazyExecutor,
        B,
        ()
    >: Future<Item = ()>,
{
    let h2_builder = h2::server::Builder::default();
    let server = tower_h2::Server::new(
        new_service,
        h2_builder,
        task::LazyExecutor
    );
    bound_port.listen_and_fold(
        server,
        move |server, (session, _)| {
            let s = server.serve(session).map_err(|_| ());
            let s = ::logging::context_future("serve_control", s);

            let r = executor::current_thread::TaskExecutor::current()
                .spawn_local(Box::new(s))
                .map(move |_| server)
                .map_err(task::Error::into_io);
            future::result(r)
        },
    )
}
