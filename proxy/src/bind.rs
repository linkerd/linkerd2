use std::io;
use std::marker::PhantomData;
use std::net::SocketAddr;
use std::sync::Arc;
use std::sync::atomic::AtomicUsize;
use std::time::Duration;

use h2;
use http;
use tokio_core::reactor::Handle;
use tower_h2;
use tower_reconnect::{self, Reconnect};

use control;
use ctx;
use telemetry;
use transport;

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
    h2_builder: h2::client::Builder,
    sensors: telemetry::Sensors,
    executor: Handle,
    req_ids: Arc<AtomicUsize>,
    connect_timeout: Duration,
    _p: PhantomData<B>,
}

type Service<B> = Reconnect<
    telemetry::sensor::NewHttp<
        tower_h2::client::Client<
            telemetry::sensor::Connect<transport::TimeoutConnect<transport::Connect>>,
            CtxtExec,
            B,
        >,
        B,
        tower_h2::RecvBody,
    >,
>;

type CtxtExec = ::logging::ContextualExecutor<(&'static str, SocketAddr), Handle>;

impl<B> Bind<(), B> {
    pub fn new(executor: Handle) -> Self {
        Self {
            executor,
            ctx: (),
            sensors: telemetry::Sensors::null(),
            req_ids: Default::default(),
            h2_builder: h2::client::Builder::default(),
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
            h2_builder: self.h2_builder,
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
            h2_builder: self.h2_builder.clone(),
            sensors: self.sensors.clone(),
            executor: self.executor.clone(),
            req_ids: self.req_ids.clone(),
            connect_timeout: self.connect_timeout,
            _p: PhantomData,
        }
    }
}


impl<C, B> Bind<C, B> {
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
    pub fn bind_service(&self, addr: &SocketAddr) -> Service<B> {
        trace!("bind_service {}", addr);
        let client_ctx = ctx::transport::Client::new(&self.ctx, addr);

        // Map a socket address to an HTTP/2.0 connection.
        let connect = {
            let c = transport::TimeoutConnect::new(
                transport::Connect::new(*addr, &self.executor),
                self.connect_timeout,
                &self.executor,
            );

            self.sensors.connect(c, &client_ctx)
        };

        // Establishes an HTTP/2.0 connection
        let client = tower_h2::client::Client::new(
            connect,
            self.h2_builder.clone(),
            ::logging::context_executor(("client", *addr), self.executor.clone()),
        );

        let h2_proxy = self.sensors.http(self.req_ids.clone(), client, &client_ctx);

        // Automatically perform reconnects if the connection fails.
        //
        // TODO: Add some sort of backoff logic.
        Reconnect::new(h2_proxy)
    }
}

// ===== impl Bind =====

impl<B> control::discovery::Bind for Bind<Arc<ctx::Proxy>, B>
where
    B: tower_h2::Body + 'static,
{
    type Request = http::Request<B>;
    type Response = http::Response<telemetry::sensor::http::ResponseBody<tower_h2::RecvBody>>;
    type Error = tower_reconnect::Error<
        tower_h2::client::Error,
        tower_h2::client::ConnectError<transport::TimeoutError<io::Error>>,
    >;
    type Service = Service<B>;
    type BindError = ();

    fn bind(&self, addr: &SocketAddr) -> Result<Self::Service, Self::BindError> {
        Ok::<_, ()>(self.bind_service(addr))
    }
}
