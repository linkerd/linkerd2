use std::marker::PhantomData;
use std::time::{Duration, Instant};

use bytes::Bytes;
use futures::{future, Async, Future, Poll, Stream};
use h2;
use http;
use tokio_core::reactor::{Handle, Timeout};
use tower::Service;
use tower_h2;
use tower_reconnect::Reconnect;
use url::HostAndPort;

use dns;
use transport::{LookupAddressAndConnect, TimeoutConnect};

mod codec;
pub mod discovery;
mod observe;
pub mod pb;
mod telemetry;

use self::discovery::{Background as DiscoBg, Discovery, Watch};
pub use self::discovery::Bind;
pub use self::observe::Observe;
use self::pb::proxy::telemetry::ReportRequest;
use self::telemetry::Telemetry;

pub struct Control {
    disco: Discovery,
}

pub struct Background {
    disco: DiscoBg,
}

pub fn new() -> (Control, Background) {
    let (tx, rx) = self::discovery::new();

    let c = Control {
        disco: tx,
    };

    let b = Background {
        disco: rx,
    };

    (c, b)
}

// ===== impl Control =====

impl Control {
    pub fn resolve<B>(&self, auth: &http::uri::Authority, bind: B) -> Watch<B> {
        self.disco.resolve(auth, bind)
    }
}

// ===== impl Background =====

impl Background {
    pub fn bind<S>(
        self,
        events: S,
        host_and_port: HostAndPort,
        dns_config: dns::Config,
        executor: &Handle,
    ) -> Box<Future<Item = (), Error = ()>>
    where
        S: Stream<Item = ReportRequest, Error = ()> + 'static,
    {
        // Build up the Controller Client Stack
        let mut client = {
            let ctx = ("controller-client", format!("{}", host_and_port));
            let scheme = http::uri::Scheme::from_shared(Bytes::from_static(b"http")).unwrap();
            let authority =
                http::uri::Authority::from_shared(format!("{}", host_and_port).into()).unwrap();

            let dns_resolver = dns::Resolver::new(dns_config, executor);
            let connect = TimeoutConnect::new(
                LookupAddressAndConnect::new(host_and_port, dns_resolver, executor),
                Duration::from_secs(3),
                executor,
            );
            let h2_client = tower_h2::client::Client::new(
                connect,
                h2::client::Builder::default(),
                ::logging::context_executor(ctx, executor.clone()),
            );


            let reconnect = Reconnect::new(h2_client);
            let backoff = Backoff::new(reconnect, Duration::from_secs(5), executor);
            AddOrigin::new(scheme, authority, backoff)
        };

        let mut disco = self.disco.work();
        let mut telemetry = Telemetry::new(events);

        let fut = future::poll_fn(move || {
            trace!("poll rpc services");
            disco.poll_rpc(&mut EnumService(&mut client, PhantomData));
            telemetry.poll_rpc(&mut EnumService(&mut client, PhantomData));

            Ok(Async::NotReady)
        });
        Box::new(fut)
    }
}

// ===== Backoff =====

/// Wait a duration if inner `poll_ready` returns an error.
//TODO: move to tower-backoff
struct Backoff<S> {
    inner: S,
    timer: Timeout,
    waiting: bool,
    wait_dur: Duration,
}

impl<S> Backoff<S> {
    fn new(inner: S, wait_dur: Duration, handle: &Handle) -> Self {
        Backoff {
            inner,
            timer: Timeout::new(wait_dur, handle).unwrap(),
            waiting: false,
            wait_dur,
        }
    }
}

impl<S> Service for Backoff<S>
where
    S: Service,
    S::Error: ::std::fmt::Debug,
{
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        if self.waiting {
            if self.timer.poll().unwrap().is_not_ready() {
                return Ok(Async::NotReady);
            }

            self.waiting = false;
        }

        match self.inner.poll_ready() {
            Err(err) => {
                warn!("controller error: {:?}", err);
                self.waiting = true;
                self.timer.reset(Instant::now() + self.wait_dur);
                Ok(Async::NotReady)
            }
            ok => ok,
        }
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        self.inner.call(req)
    }
}

/// Wraps an HTTP service, injecting authority and scheme on every request.
struct AddOrigin<S> {
    authority: http::uri::Authority,
    inner: S,
    scheme: http::uri::Scheme,
}

impl<S> AddOrigin<S> {
    fn new(scheme: http::uri::Scheme, auth: http::uri::Authority, service: S) -> Self {
        AddOrigin {
            authority: auth,
            inner: service,
            scheme,
        }
    }
}

impl<S, B> Service for AddOrigin<S>
where
    S: Service<Request = http::Request<B>>,
{
    type Request = http::Request<B>;
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready()
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        let (mut head, body) = req.into_parts();
        let mut uri: http::uri::Parts = head.uri.into();
        uri.scheme = Some(self.scheme.clone());
        uri.authority = Some(self.authority.clone());
        head.uri = http::Uri::from_parts(uri).expect("valid uri");

        self.inner.call(http::Request::from_parts(head, body))
    }
}

// ===== impl  EnumService =====

struct EnumService<S, B>(S, PhantomData<B>);

impl<S, B> Service for EnumService<S, B>
where
    S: Service<Request = http::Request<GrpcEncodingBody>>,
    B: Into<GrpcEncodingBody>,
{
    type Request = http::Request<B>;
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.0.poll_ready()
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        let (head, body) = req.into_parts();
        self.0.call(http::Request::from_parts(head, body.into()))
    }
}



enum GrpcEncodingBody {
    TelemetryReport(self::telemetry::ClientBody),
    DestinationGet(self::discovery::ClientBody),
}

impl tower_h2::Body for GrpcEncodingBody {
    type Data = Bytes;

    #[inline]
    fn is_end_stream(&self) -> bool {
        match *self {
            GrpcEncodingBody::TelemetryReport(ref b) => b.is_end_stream(),
            GrpcEncodingBody::DestinationGet(ref b) => b.is_end_stream(),
        }
    }

    #[inline]
    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        match *self {
            GrpcEncodingBody::TelemetryReport(ref mut b) => b.poll_data(),
            GrpcEncodingBody::DestinationGet(ref mut b) => b.poll_data(),
        }
    }

    #[inline]
    fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, h2::Error> {
        match *self {
            GrpcEncodingBody::TelemetryReport(ref mut b) => b.poll_trailers(),
            GrpcEncodingBody::DestinationGet(ref mut b) => b.poll_trailers(),
        }
    }
}

impl From<self::telemetry::ClientBody> for GrpcEncodingBody {
    fn from(body: self::telemetry::ClientBody) -> Self {
        GrpcEncodingBody::TelemetryReport(body)
    }
}

impl From<self::discovery::ClientBody> for GrpcEncodingBody {
    fn from(body: self::discovery::ClientBody) -> Self {
        GrpcEncodingBody::DestinationGet(body)
    }
}
