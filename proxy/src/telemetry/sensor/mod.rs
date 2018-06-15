use std::sync::Arc;
use std::time::Instant;

use futures_mpsc_lossy::Sender;
use http::{Request, Response};
use tokio_connect;
use tokio::io::{AsyncRead, AsyncWrite};
use tower_service::NewService;
use tower_h2::{client, Body};

use ctx;
use telemetry::event;
use transport::tls;

pub mod http;
mod transport;

pub use self::http::{Http, NewHttp};
pub use self::transport::{Connect, Transport};

/// Accepts events from sensors.
#[derive(Clone, Debug)]
struct Handle(Option<Sender<event::Event>>);

/// Supports the creation of telemetry scopes.
#[derive(Clone, Debug)]
pub struct Sensors(Handle);

/// Given to the TLS config watch to generate events on reloads.
#[derive(Clone, Debug)]
pub struct TlsConfig(Handle);

impl Handle {
    fn send<F>(&mut self, mk: F)
    where
        F: FnOnce() -> event::Event,
    {
        if let Some(tx) = self.0.as_mut() {
            // We may want to capture timestamps here instead of on the consumer-side...  That
            // level of precision doesn't necessarily seem worth it yet.

            let ev = mk();
            trace!("event: {:?}", ev);

            if tx.lossy_send(ev).is_err() {
                debug!("dropped event");
            }
        }
    }
}

impl Sensors {
    pub(super) fn new(h: Sender<event::Event>) -> Self {
        Sensors(Handle(Some(h)))
    }

    pub fn null() -> Sensors {
        Sensors(Handle(None))
    }

    pub fn accept<T>(
        &self,
        io: T,
        opened_at: Instant,
        ctx: &Arc<ctx::transport::Server>,
    ) -> Transport<T>
    where
        T: AsyncRead + AsyncWrite,
    {
        debug!("server connection open");
        let ctx = Arc::new(ctx::transport::Ctx::Server(Arc::clone(ctx)));
        Transport::open(io, opened_at, &self.0, ctx)
    }

    pub fn connect<C>(&self, connect: C, ctx: &Arc<ctx::transport::Client>) -> Connect<C>
    where
        C: tokio_connect::Connect<Connected = ::connection::Connection>,
    {
        Connect::new(connect, &self.0, ctx)
    }

    pub fn http<N, A, B>(
        &self,
        new_service: N,
        client_ctx: &Arc<ctx::transport::Client>,
    ) -> NewHttp<N, A, B>
    where
        A: Body + 'static,
        B: Body + 'static,
        N: NewService<
            Request = Request<http::RequestBody<A>>,
            Response = Response<B>,
            Error = client::Error
        >
            + 'static,
    {
        NewHttp::new(new_service, &self.0, client_ctx)
    }

    pub fn tls_config(&self) -> TlsConfig {
        TlsConfig(self.0.clone())
    }
}

impl TlsConfig {

    pub fn reloaded(&mut self) {
        self.0.send(|| event::Event::TlsConfigReloaded)
    }

    pub fn failed(&mut self, err: tls::ConfigError) {
        self.0.send(|| event::Event::TlsConfigReloadFailed(err.into()))
    }
}
