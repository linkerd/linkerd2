use std::fmt;
use std::io;
use std::time::{Duration, Instant};

use bytes::Bytes;
use futures::{future, Async, Future, Poll};
use h2;
use http;
use tokio::timer::Delay;
use tower_service::Service;
use tower_h2;
use tower_reconnect::{Error as ReconnectError, Reconnect};

use dns;
use task::LazyExecutor;
use transport::{DnsNameAndPort, HostAndPort, LookupAddressAndConnect};
use timeout::{Timeout, TimeoutError};

mod cache;
pub mod destination;
mod fully_qualified_authority;
mod observe;
pub mod pb;
mod remote_stream;

use self::destination::{Resolver, Resolution};
pub use self::destination::Bind;
pub use self::observe::Observe;

#[derive(Clone)]
pub struct Control {
    disco: Resolver,
}

pub struct Background {
    disco: destination::background::Config,
}

pub fn new(dns_config: dns::Config, default_destination_namespace: String) -> (Control, Background)
{
    let (tx, rx) = self::destination::new(dns_config, default_destination_namespace);

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
    pub fn resolve<B>(&self, auth: &DnsNameAndPort, bind: B) -> Resolution<B> {
        self.disco.resolve(auth, bind)
    }
}

// ===== impl Background =====

impl Background {
    pub fn bind(
        self,
        host_and_port: HostAndPort,
        dns_config: dns::Config,
    ) -> Box<Future<Item = (), Error = ()>> {
        // Build up the Controller Client Stack
        let mut client = {
            let ctx = ("controller-client", format!("{:?}", host_and_port));
            let scheme = http::uri::Scheme::from_shared(Bytes::from_static(b"http")).unwrap();
            let authority = http::uri::Authority::from(&host_and_port);
            let dns_resolver = dns::Resolver::new(dns_config);
            let connect = Timeout::new(
                LookupAddressAndConnect::new(host_and_port, dns_resolver),
                Duration::from_secs(3),
            );

            let h2_client = tower_h2::client::Connect::new(
                connect,
                h2::client::Builder::default(),
                ::logging::context_executor(ctx, LazyExecutor),
            );

            let reconnect = Reconnect::new(h2_client);
            let log_errors = LogErrors::new(reconnect);
            let backoff = Backoff::new(log_errors, Duration::from_secs(5));
            // TODO: Use AddOrigin in tower-http
            AddOrigin::new(scheme, authority, backoff)
        };

        let mut disco = self.disco.process();

        let fut = future::poll_fn(move || {
            disco.poll_rpc(&mut client);

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
    timer: Delay,
    waiting: bool,
    wait_dur: Duration,
}

impl<S> Backoff<S> {
    fn new(inner: S, wait_dur: Duration) -> Self {
        Backoff {
            inner,
            timer: Delay::new(Instant::now() + wait_dur),
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
            Err(_err) => {
                trace!("backoff: controller error, waiting {:?}", self.wait_dur);
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

// ===== impl LogErrors

/// Log errors talking to the controller in human format.
struct LogErrors<S> {
    inner: S,
}

// We want some friendly logs, but the stack of services don't have fmt::Display
// errors, so we have to build that ourselves. For now, this hard codes the
// expected error stack, and so any new middleware added will need to adjust this.
//
// The dead_code allowance is because rustc is being stupid and doesn't see it
// is used down below.
#[allow(dead_code)]
type LogError = ReconnectError<
    tower_h2::client::Error,
    tower_h2::client::ConnectError<
        TimeoutError<
            io::Error
        >
    >
>;

impl<S> LogErrors<S>
where
    S: Service<Error=LogError>,
{
    fn new(service: S) -> Self {
        LogErrors {
            inner: service,
        }
    }
}

impl<S> Service for LogErrors<S>
where
    S: Service<Error=LogError>,
{
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready().map_err(|e| {
            error!("controller error: {}", HumanError(&e));
            e
        })
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        self.inner.call(req)
    }
}

struct HumanError<'a>(&'a LogError);

impl<'a> fmt::Display for HumanError<'a> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self.0 {
            ReconnectError::Inner(ref e) => {
                fmt::Display::fmt(e, f)
            },
            ReconnectError::Connect(ref e) => {
                fmt::Display::fmt(e, f)
            },
            ReconnectError::NotReady => {
                // this error should only happen if we `call` the service
                // when it isn't ready, which is really more of a bug on
                // our side...
                f.pad("bug: called service when not ready")
            },
        }
    }
}
