#![cfg_attr(feature = "cargo-clippy", allow(clone_on_ref_ptr))]
#![cfg_attr(feature = "cargo-clippy", allow(new_without_default_derive))]
#![deny(warnings)]

extern crate abstract_ns;
extern crate bytes;
extern crate conduit_proxy_controller_grpc;
extern crate convert;
extern crate domain;
extern crate env_logger;
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
extern crate ns_dns_tokio;
extern crate indexmap;
extern crate prost;
extern crate prost_types;
#[cfg(test)]
#[macro_use]
extern crate quickcheck;
extern crate rand;
extern crate tokio_connect;
extern crate tokio_core;
extern crate tokio_io;
extern crate tower;
extern crate tower_balance;
extern crate tower_buffer;
extern crate tower_discover;
extern crate tower_grpc;
extern crate tower_h2;
extern crate tower_reconnect;
extern crate conduit_proxy_router;
extern crate tower_util;
extern crate tower_in_flight_limit;

use futures::*;

use std::error::Error;
use std::io;
use std::net::SocketAddr;
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use tokio_core::reactor::{Core, Handle};
use tower::NewService;
use tower_fn::*;
use conduit_proxy_router::{Recognize, Router, Error as RouteError};

pub mod app;
mod bind;
pub mod config;
mod connection;
pub mod control;
mod ctx;
mod dns;
mod fully_qualified_authority;
mod inbound;
mod logging;
mod map_err;
mod outbound;
mod telemetry;
mod transparency;
mod transport;
pub mod timeout;
mod tower_fn; // TODO: move to tower-fn

use bind::Bind;
use connection::BoundPort;
use inbound::Inbound;
use map_err::MapErr;
use transparency::{HttpBody, Server};
pub use transport::{GetOriginalDst, SoOriginalDst};
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

    reactor: Core,
}

impl<G> Main<G>
where
    G: GetOriginalDst + Clone + 'static,
{
    pub fn new(config: config::Config, get_original_dst: G) -> Self {

        let control_listener = BoundPort::new(config.control_listener.addr)
            .expect("controller listener bind");
        let inbound_listener = BoundPort::new(config.public_listener.addr)
            .expect("public listener bind");
        let outbound_listener = BoundPort::new(config.private_listener.addr)
            .expect("private listener bind");

        let reactor = Core::new().expect("reactor");

        let metrics_listener = BoundPort::new(config.metrics_listener.addr)
            .expect("metrics listener bind");
        Main {
            config,
            control_listener,
            inbound_listener,
            outbound_listener,
            metrics_listener,
            get_original_dst,
            reactor,
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

    pub fn handle(&self) -> Handle {
        self.reactor.handle()
    }

    pub fn metrics_addr(&self) -> SocketAddr {
        self.metrics_listener.local_addr()
    }

    pub fn run_until<F>(self, shutdown_signal: F)
    where
        F: Future<Item = (), Error = ()>,
    {
        let process_ctx = ctx::Process::new(&self.config);

        let Main {
            config,
            control_listener,
            inbound_listener,
            outbound_listener,
            metrics_listener,
            get_original_dst,
            reactor: mut core,
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
        let (sensors, telemetry) = telemetry::new(
            &process_ctx,
            config.event_buffer_capacity,
            config.metrics_flush_interval,
            config.prometheus_labels.clone(),
        );

        let (control, control_bg) = control::new();

        let executor = core.handle();

        let dns_config = dns::Config::from_file(&config.resolv_conf_path);

        let bind = Bind::new(executor.clone()).with_sensors(sensors.clone());

        // Setup the public listener. This will listen on a publicly accessible
        // address and listen for inbound connections that should be forwarded
        // to the managed application (private destination).
        let inbound = {
            let ctx = ctx::Proxy::inbound(&process_ctx);

            let bind = bind.clone().with_ctx(ctx.clone());

            let default_addr = config.private_forward.map(|a| a.into());

            let fut = serve(
                inbound_listener,
                Inbound::new(default_addr, bind),
                config.private_connect_timeout,
                ctx,
                sensors.clone(),
                get_original_dst.clone(),
                &executor,
            );
            ::logging::context_future("inbound", fut)
        };

        // Setup the private listener. This will listen on a locally accessible
        // address and listen for outbound requests that should be routed
        // to a remote service (public destination).
        let outbound = {
            let ctx = ctx::Proxy::outbound(&process_ctx);

            let bind = bind.clone().with_ctx(ctx.clone());

            let outgoing = Outbound::new(
                bind,
                control,
                config.default_destination_namespace().to_owned(),
                config.bind_timeout,
            );

            let fut = serve(
                outbound_listener,
                outgoing,
                config.public_connect_timeout,
                ctx,
                sensors,
                get_original_dst,
                &executor,
            );
            ::logging::context_future("outbound", fut)
        };

        trace!("running");

        let (_tx, controller_shutdown_signal) = futures::sync::oneshot::channel::<()>();
        {
            thread::Builder::new()
                .name("controller-client".into())
                .spawn(move || {
                    use conduit_proxy_controller_grpc::tap::server::TapServer;

                    let mut core = Core::new().expect("initialize controller core");
                    let executor = core.handle();

                    let (taps, observe) = control::Observe::new(100);
                    let new_service = TapServer::new(observe);

                    let server = serve_control(
                        control_listener,
                        new_service,
                        &executor,
                    );

                    let telemetry = telemetry
                        .make_control(&taps, &executor)
                        .expect("bad news in telemetry town");

                    let metrics_server = telemetry
                        .serve_metrics(metrics_listener);

                    let client = control_bg.bind(
                        telemetry,
                        control_host_and_port,
                        dns_config,
                        config.report_timeout,
                        &executor
                    );

                    let fut = client.join3(
                        server.map_err(|_| {}),
                        metrics_server.map_err(|_| {}),
                    ).map(|_| {});
                    executor.spawn(::logging::context_future("controller-client", fut));

                    let shutdown = controller_shutdown_signal.then(|_| Ok::<(), ()>(()));
                    core.run(shutdown).expect("controller api");
                })
                .expect("initialize controller api thread");
        }

        let fut = inbound
            .join(outbound)
            .map(|_| ())
            .map_err(|err| error!("main error: {:?}", err));

        core.handle().spawn(fut);
        core.run(shutdown_signal).expect("executor");
    }
}

fn serve<R, B, E, F, G>(
    bound_port: BoundPort,
    recognize: R,
    tcp_connect_timeout: Duration,
    proxy_ctx: Arc<ctx::Proxy>,
    sensors: telemetry::Sensors,
    get_orig_dst: G,
    executor: &Handle,
) -> Box<Future<Item = (), Error = io::Error> + 'static>
where
    B: tower_h2::Body + Default + 'static,
    E: Error + 'static,
    F: Error + 'static,
    R: Recognize<
        Request = http::Request<HttpBody>,
        Response = http::Response<telemetry::sensor::http::ResponseBody<B>>,
        Error = E,
        RouteError = F,
    >
        + 'static,
    G: GetOriginalDst + 'static,
{
    let router = Router::new(recognize);
    let stack = Arc::new(NewServiceFn::new(move || {
        // Clone the router handle
        let router = router.clone();

        // Map errors to appropriate response error codes.
        MapErr::new(router, |e| {
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
            }
        })
    }));

    let listen_addr = bound_port.local_addr();
    let server = Server::new(
        listen_addr,
        proxy_ctx,
        sensors,
        get_orig_dst,
        stack,
        tcp_connect_timeout,
        executor.clone(),
    );

    bound_port.listen_and_fold(
        executor,
        (),
        move |(), (connection, remote_addr)| {
            server.serve(connection, remote_addr);
            Ok(())
        },
    )
}

fn serve_control<N, B>(
    bound_port: BoundPort,
    new_service: N,
    executor: &Handle,
) -> Box<Future<Item = (), Error = io::Error> + 'static>
where
    B: tower_h2::Body + 'static,
    N: NewService<Request = http::Request<tower_h2::RecvBody>, Response = http::Response<B>> + 'static,
{
    let h2_builder = h2::server::Builder::default();
    let server = tower_h2::Server::new(new_service, h2_builder, executor.clone());
    bound_port.listen_and_fold(
        executor,
        (server, executor.clone()),
        move |(server, executor), (session, _)| {
            let s = server.serve(session).map_err(|_| ());

            executor.spawn(::logging::context_future("serve_control", s));


            future::ok((server, executor))
        },
    )
}
