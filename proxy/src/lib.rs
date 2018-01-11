#![cfg_attr(feature = "cargo-clippy", allow(clone_on_ref_ptr))]
#![cfg_attr(feature = "cargo-clippy", allow(new_without_default_derive))]
#![deny(warnings)]

extern crate abstract_ns;
extern crate bytes;
extern crate chrono;
extern crate domain;
extern crate env_logger;
#[macro_use]
extern crate futures;
extern crate futures_mpsc_lossy;
extern crate h2;
extern crate http;
extern crate ipnet;
#[cfg(target_os = "linux")]
extern crate libc;
#[macro_use]
extern crate log;
extern crate ns_dns_tokio;
extern crate ordermap;
extern crate prost;
#[macro_use]
extern crate prost_derive;
extern crate prost_types;
#[cfg(test)]
#[macro_use]
extern crate quickcheck;
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
extern crate tower_router;
extern crate tower_util;
extern crate url;

use futures::*;

use std::io;
use std::net::SocketAddr;
use std::sync::Arc;
use std::thread;
use std::time::Instant;

use tokio_core::reactor::{Core, Handle};
use tower::NewService;
use tower_fn::*;
use tower_h2::*;
use tower_router::{Recognize, Router};

pub mod app;
mod bind;
pub mod config;
mod connection;
pub mod control;
pub mod convert;
mod ctx;
mod dns;
mod fully_qualified_authority;
mod inbound;
mod logging;
mod map_err;
mod outbound;
mod telemetry;
mod transport;
pub mod timeout;
mod tower_fn; // TODO: move to tower-fn

use bind::Bind;
use connection::BoundPort;
use control::pb::proxy::tap;
use inbound::Inbound;
use map_err::MapErr;
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

pub struct Main {
    config: config::Config,

    control_listener: BoundPort,
    inbound_listener: BoundPort,
    outbound_listener: BoundPort,
}

impl Main {
    pub fn new(config: config::Config) -> Self {

        let control_listener = BoundPort::new(config.control_listener.addr)
            .expect("controller listener bind");
        let inbound_listener = BoundPort::new(config.public_listener.addr)
            .expect("public listener bind");
        let outbound_listener = BoundPort::new(config.private_listener.addr)
            .expect("private listener bind");
        Main {
            config,
            control_listener,
            inbound_listener,
            outbound_listener,
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

    pub fn run(self) {
        self.run_until(::futures::future::empty());
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
        } = self;

        let control_host_and_port = config.control_host_and_port.clone();

        info!("using controller at {:?}", control_host_and_port);
        info!("routing on {:?}", outbound_listener.local_addr());
        info!(
            "proxying on {:?} to {:?}",
            inbound_listener.local_addr(),
            config.private_forward
        );

        let (sensors, telemetry) = telemetry::new(
            &process_ctx,
            config.event_buffer_capacity,
            config.metrics_flush_interval,
        );

        let (control, control_bg) = control::new();

        let mut core = Core::new().expect("executor");
        let executor = core.handle();

        let dns_config = dns::Config::from_file(&config.resolv_conf_path);

        let bind = Bind::new(executor.clone()).with_sensors(sensors.clone());

        // Setup the public listener. This will listen on a publicly accessible
        // address and listen for inbound connections that should be forwarded
        // to the managed application (private destination).
        let inbound = {
            let ctx = ctx::Proxy::inbound(&process_ctx);

            let bind = bind.clone()
                .with_connect_timeout(config.private_connect_timeout)
                .with_ctx(ctx.clone());

            let default_addr = config.private_forward.map(|a| a.into());

            let fut = serve(
                inbound_listener,
                h2::server::Builder::default(),
                Inbound::new(default_addr, bind),
                ctx,
                sensors.clone(),
                &executor,
            );
            ::logging::context_future("inbound", fut)
        };

        // Setup the private listener. This will listen on a locally accessible
        // address and listen for outbound requests that should be routed
        // to a remote service (public destination).
        let outbound = {
            let ctx = ctx::Proxy::outbound(&process_ctx);

            let bind = config
                .public_connect_timeout
                .map_or_else(|| bind.clone(), |t| bind.clone().with_connect_timeout(t))
                .with_ctx(ctx.clone());

            let outgoing = Outbound::new(
                bind,
                control,
                config.default_destination_namespace().cloned(),
                config.default_destination_zone().cloned());

            let fut = serve(
                outbound_listener,
                h2::server::Builder::default(),
                outgoing,
                ctx,
                sensors,
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
                    let mut core = Core::new().expect("initialize controller core");
                    let executor = core.handle();

                    let (taps, observe) = control::Observe::new(100);

                    let new_service = tap::server::Tap::new_service().observe(observe);

                    let server = serve_control(
                        control_listener,
                        h2::server::Builder::default(),
                        new_service,
                        &executor,
                    );

                    let telemetry = telemetry
                        .make_control(&taps, &executor)
                        .expect("bad news in telemetry town");

                    let client = control_bg.bind(
                        telemetry,
                        control_host_and_port,
                        dns_config,
                        config.report_timeout,
                        &executor
                    );

                    let fut = client.join(server.map_err(|_| {})).map(|_| {});
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

fn serve<R, B, E, F>(
    bound_port: BoundPort,
    h2_builder: h2::server::Builder,
    recognize: R,
    proxy_ctx: Arc<ctx::Proxy>,
    sensors: telemetry::Sensors,
    executor: &Handle,
) -> Box<Future<Item = (), Error = io::Error> + 'static>
where
    B: Body + Default + 'static,
    E: ::std::fmt::Debug + 'static,
    F: ::std::fmt::Debug + 'static,
    R: Recognize<
        Request = http::Request<RecvBody>,
        Response = http::Response<telemetry::sensor::http::ResponseBody<B>>,
        Error = E,
        RouteError = F,
    >
        + 'static,
{
    let router = Router::new(recognize);
    let stack = NewServiceFn::new(move || {
        // Clone the router handle
        let router = router.clone();

        // Map errors to 500 responses
        MapErr::new(router)
    });

    let listen_addr = bound_port.local_addr();
    let server = Server::new(
        stack,
        h2_builder,
        ::logging::context_executor(("serve", listen_addr), executor.clone()),
    );
    bound_port.listen_and_fold(
        executor,
        (server, proxy_ctx, sensors, executor.clone()),
        move |(server, proxy_ctx, sensors, executor), (connection, remote_addr)| {
            let opened_at = Instant::now();
            let orig_dst = connection.original_dst_addr();
            let local_addr = connection.local_addr().unwrap_or(listen_addr);
            // TODO: detect protocol.
            let protocol = control::pb::common::Protocol::Http;
            let srv_ctx =
                ctx::transport::Server::new(
                    &proxy_ctx,
                    &local_addr,
                    &remote_addr,
                    &orig_dst,
                    protocol,
                );

            let io = sensors.accept(connection, opened_at, &srv_ctx);

            // TODO session context
            let set_ctx = move |request: &mut http::Request<()>| {
                request.extensions_mut().insert(Arc::clone(&srv_ctx));
            };

            let s = server.serve_modified(io, set_ctx).map_err(|_| ());
            executor.spawn(::logging::context_future(("serve", local_addr), s));

            future::ok((server, proxy_ctx, sensors, executor))
        },
    )
}

fn serve_control<N, B>(
    bound_port: BoundPort,
    h2_builder: h2::server::Builder,
    new_service: N,
    executor: &Handle,
) -> Box<Future<Item = (), Error = io::Error> + 'static>
where
    B: Body + 'static,
    N: NewService<Request = http::Request<RecvBody>, Response = http::Response<B>> + 'static,
{
    let server = Server::new(new_service, h2_builder, executor.clone());
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
