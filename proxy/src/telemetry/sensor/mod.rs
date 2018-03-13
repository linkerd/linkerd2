use std::sync::Arc;
use std::sync::atomic::AtomicUsize;
use std::time::Instant;

use futures_mpsc_lossy::Sender;
use http::{Request, Response};
use tokio_connect;
use tokio_io::{AsyncRead, AsyncWrite};
use tower::NewService;
use tower_h2::{client, Body};

use ctx;
use telemetry::event;
use time::Timer;

pub mod http;
mod transport;

pub use self::http::{Http, NewHttp};
pub use self::transport::{Connect, Transport};

/// Accepts events from sensors.
#[derive(Clone, Debug)]
struct Handle<T> {
    tx: Option<Sender<event::Event>>,
    timer: T,
}

/// Supports the creation of telemetry scopes.
#[derive(Clone, Debug)]
pub struct Sensors<T>(Handle<T>);

impl<T> Handle<T> {
    fn send<F>(&mut self, mk: F)
    where
        F: FnOnce() -> event::Event,
    {
        if let Some(tx) = self.tx.as_mut() {
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

impl<T: Timer> Sensors<T> {
    pub(super) fn new(tx: Sender<event::Event>, timer: &T) -> Self {
        Sensors(Handle {
            tx: Some(tx),
            timer: timer.clone()
        })
    }

    pub fn null(timer: &T) -> Sensors<T> {
        Sensors(Handle {
            tx: None,
            timer: timer.clone()
        })
    }

    pub fn accept<I>(
        &self,
        io: I,
        opened_at: Instant,
        ctx: &Arc<ctx::transport::Server>,
    ) -> Transport<I, T>
    where
        I: AsyncRead + AsyncWrite,
    {
        debug!("server connection open");
        let ctx = Arc::new(ctx::transport::Ctx::Server(Arc::clone(ctx)));
        Transport::open(io, opened_at, &self.0, ctx)
    }

    pub fn connect<C>(
        &self,
        connect: C,
        ctx: &Arc<ctx::transport::Client>,
    )  -> Connect<C, T>
    where
        C: tokio_connect::Connect,
    {
        Connect::new(connect, &self.0, ctx)
    }

    pub fn http<N, A, B>(
        &self,
        next_id: Arc<AtomicUsize>,
        new_service: N,
        client_ctx: &Arc<ctx::transport::Client>,
    ) -> NewHttp<N, A, B, T>
    where
        A: Body + 'static,
        B: Body + 'static,
        N: NewService<Request = Request<A>, Response = Response<B>, Error = client::Error>
            + 'static,
    {
        NewHttp::new(next_id, new_service, &self.0, client_ctx)
    }
}
