use std::io;
use std::marker::PhantomData;
use std::net::SocketAddr;
use std::sync::Arc;
use std::sync::atomic::AtomicUsize;
use std::time::Duration;

use http;
use tokio_core::reactor::Handle;
use tower_h2;
use tower_reconnect::{self, Reconnect};

use control;
use ctx;
use telemetry;
use transparency;
use transport;
use ::timeout::Timeout;

const DEFAULT_TIMEOUT_MS: u64 = 300;

/// Binds a `Service` from a `SocketAddr`.
///
/// The returned `Service` buffers request until a connection is established.
///
/// # TODO
///
/// Buffering is not bounded and no timeouts are applied.
pub struct Bind<C, B> {
    ctx: C,
    sensors: telemetry::Sensors,
    executor: Handle,
    req_ids: Arc<AtomicUsize>,
    connect_timeout: Duration,
    _p: PhantomData<B>,
}

/// Binds a `Service` from a `SocketAddr` for a pre-determined protocol.
pub struct BindProtocol<C, B> {
    bind: Bind<C, B>,
    protocol: Protocol,
}

/// Mark whether to use HTTP/1 or HTTP/2
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Protocol {
    Http1,
    Http2
}

type Service<B> = Reconnect<
    telemetry::sensor::NewHttp<
        transparency::Client<
            telemetry::sensor::Connect<transport::TimeoutConnect<transport::Connect>>,
            B,
        >,
        B,
        transparency::HttpBody,
    >,
>;

impl<B> Bind<(), B> {
    pub fn new(executor: Handle) -> Self {
        Self {
            executor,
            ctx: (),
            sensors: telemetry::Sensors::null(),
            req_ids: Default::default(),
            connect_timeout: Duration::from_millis(DEFAULT_TIMEOUT_MS),
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

    pub fn with_ctx<C>(self, ctx: C) -> Bind<C, B> {
        Bind {
            ctx,
            sensors: self.sensors,
            executor: self.executor,
            req_ids: self.req_ids,
            connect_timeout: self.connect_timeout,
            _p: PhantomData,
        }
    }
}

impl<C: Clone, B> Clone for Bind<C, B> {
    fn clone(&self) -> Self {
        Self {
            ctx: self.ctx.clone(),
            sensors: self.sensors.clone(),
            executor: self.executor.clone(),
            req_ids: self.req_ids.clone(),
            connect_timeout: self.connect_timeout,
            _p: PhantomData,
        }
    }
}


impl<C, B> Bind<C, B> {
    pub fn connect_timeout(&self) -> Duration {
        self.connect_timeout
    }

    // pub fn ctx(&self) -> &C {
    //     &self.ctx
    // }

    pub fn executor(&self) -> &Handle {
        &self.executor
    }

    // pub fn req_ids(&self) -> &Arc<AtomicUsize> {
    //     &self.req_ids
    // }

    // pub fn sensors(&self) -> &telemetry::Sensors {
    //     &self.sensors
    // }

}

impl<B> Bind<Arc<ctx::Proxy>, B>
where
    B: tower_h2::Body + 'static,
{
    pub fn bind_service(&self, addr: &SocketAddr, protocol: Protocol) -> Service<B> {
        trace!("bind_service addr={}, protocol={:?}", addr, protocol);
        let client_ctx = ctx::transport::Client::new(
            &self.ctx,
            addr,
            control::pb::proxy::common::Protocol::Http,
        );

        // Map a socket address to a connection.
        let connect = {
            let c = Timeout::new(
                transport::Connect::new(*addr, &self.executor),
                self.connect_timeout,
                &self.executor,
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


impl<C, B> Bind<C, B> {
    pub fn with_protocol(self, protocol: Protocol) -> BindProtocol<C, B> {
        BindProtocol {
            bind: self,
            protocol,
        }
    }
}

impl<B> control::discovery::Bind for BindProtocol<Arc<ctx::Proxy>, B>
where
    B: tower_h2::Body + 'static,
{
    type Request = http::Request<B>;
    type Response = http::Response<telemetry::sensor::http::ResponseBody<transparency::HttpBody>>;
    type Error = tower_reconnect::Error<
        tower_h2::client::Error,
        tower_h2::client::ConnectError<transport::TimeoutError<io::Error>>,
    >;
    type Service = Service<B>;
    type BindError = ();

    fn bind(&self, addr: &SocketAddr) -> Result<Self::Service, Self::BindError> {
        Ok(self.bind.bind_service(addr, self.protocol))
    }
}

