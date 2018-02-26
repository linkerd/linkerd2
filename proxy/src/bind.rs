use std::marker::PhantomData;
use std::net::SocketAddr;
use std::sync::Arc;
use std::sync::atomic::AtomicUsize;
use std::time::Duration;

use http;
use tokio_core::reactor::Handle;
use tower;
use tower_h2;
use tower_reconnect::Reconnect;

use conduit_proxy_controller_grpc;
use control;
use ctx;
use telemetry::{self, sensor};
use transparency::{self, HttpBody};
use transport;
use time::{Timer, Timeout};

const DEFAULT_TIMEOUT_MS: u64 = 300;

/// Binds a `Service` from a `SocketAddr`.
///
/// The returned `Service` buffers request until a connection is established.
///
/// # TODO
///
/// Buffering is not bounded and no timeouts are applied.
pub struct Bind<C, B, T> {
    ctx: C,
    sensors: telemetry::Sensors,
    executor: Handle,
    req_ids: Arc<AtomicUsize>,
    connect_timeout: Duration,
    timer: T,
    _p: PhantomData<B>,
}

/// Binds a `Service` from a `SocketAddr` for a pre-determined protocol.
pub struct BindProtocol<C, B, T> {
    bind: Bind<C, B, T>,
    protocol: Protocol,
}

/// Mark whether to use HTTP/1 or HTTP/2
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Protocol {
    Http1,
    Http2
}

pub type Service<B, T> = Reconnect<NewHttp<B, T>>;

pub type NewHttp<B, T> = sensor::NewHttp<Client<B, T>, B, HttpBody>;

pub type HttpResponse = http::Response<sensor::http::ResponseBody<HttpBody>>;

pub type Client<B, T> = transparency::Client<
    sensor::Connect<Timeout<transport::Connect, T>>,
    B,
>;

impl<B, T> Bind<(), B, T> {
    pub fn new(executor: Handle, timer: T) -> Self {
        Self {
            executor,
            ctx: (),
            sensors: telemetry::Sensors::null(),
            req_ids: Default::default(),
            connect_timeout: Duration::from_millis(DEFAULT_TIMEOUT_MS),
            timer,
            _p: PhantomData,
        }
    }

    pub fn with_connect_timeout(self, connect_timeout: Duration) -> Self {
        Self {
            connect_timeout,
            ..self
        }
    }

    pub fn with_sensors(self, sensors: telemetry::Sensors) -> Self {
        Self {
            sensors,
            ..self
        }
    }

    pub fn with_ctx<C>(self, ctx: C) -> Bind<C, B, T> {
        Bind {
            ctx,
            sensors: self.sensors,
            executor: self.executor,
            req_ids: self.req_ids,
            connect_timeout: self.connect_timeout,
            timer: self.timer,
            _p: PhantomData,
        }
    }
}

impl<C: Clone, B, T: Clone> Clone for Bind<C, B, T> {
    fn clone(&self) -> Self {
        Self {
            ctx: self.ctx.clone(),
            sensors: self.sensors.clone(),
            executor: self.executor.clone(),
            req_ids: self.req_ids.clone(),
            connect_timeout: self.connect_timeout,
            timer: self.timer.clone(),
            _p: PhantomData,
        }
    }
}


impl<C, B, T> Bind<C, B, T> {
    pub fn connect_timeout(&self) -> Duration {
        self.connect_timeout
    }

    // pub fn ctx(&self) -> &C {
    //     &self.ctx
    // }

    pub fn executor(&self) -> &Handle {
        &self.executor
    }

    pub fn timer(&self) -> &T {
        &self.timer
    }


    // pub fn req_ids(&self) -> &Arc<AtomicUsize> {
    //     &self.req_ids
    // }

    // pub fn sensors(&self) -> &telemetry::Sensors {
    //     &self.sensors
    // }

}

impl<B, T> Bind<Arc<ctx::Proxy>, B, T>
where
    B: tower_h2::Body + 'static,
    T: Timer + 'static,
{
    pub fn bind_service(&self, addr: &SocketAddr, protocol: Protocol)
                        -> Service<B, T>
    {
        trace!("bind_service addr={}, protocol={:?}", addr, protocol);
        let client_ctx = ctx::transport::Client::new(
            &self.ctx,
            addr,
            conduit_proxy_controller_grpc::common::Protocol::Http,
        );

        // Map a socket address to a connection.
        let connect = {
            let c = self.timer
                .timeout(
                    transport::Connect::new(*addr, &self.executor),
                    self.connect_timeout,
                ).with_description(format!(
                    "binding service for {}, protocol={:?}", addr, protocol)
                );

            self.sensors.connect(c, &client_ctx)
        };

        let client = transparency::Client::new(
            protocol,
            connect,
            self.executor.clone(),
        );

        let proxy = self.sensors.http(self.req_ids.clone(), client, &client_ctx);

        // Automatically perform reconnects if the connection fails.
        //
        // TODO: Add some sort of backoff logic.
        Reconnect::new(proxy)
    }
}

// ===== impl BindProtocol =====


impl<C, B, T> Bind<C, B, T> {
    pub fn with_protocol(self, protocol: Protocol) -> BindProtocol<C, B, T> {
        BindProtocol {
            bind: self,
            protocol,
        }
    }
}

impl<B, T> control::discovery::Bind for BindProtocol<Arc<ctx::Proxy>, B, T>
where
    B: tower_h2::Body + 'static,
    T: Timer + 'static,
{
    type Request = http::Request<B>;
    type Response = HttpResponse;
    type Error = <Service<B, T> as tower::Service>::Error;
    type Service = Service<B, T>;
    type BindError = ();

    fn bind(&self, addr: &SocketAddr) -> Result<Self::Service, Self::BindError> {
        Ok(self.bind.bind_service(addr, self.protocol))
    }
}

