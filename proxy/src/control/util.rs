use futures::{Async, Future, Poll};
use http;
use std::fmt;
use std::io;
use std::time::{Duration, Instant};
use tokio::timer::Delay;
use tower_service::Service;
use tower_h2;
use tower_reconnect::{Error as ReconnectError};

use timeout::TimeoutError;

// ===== Backoff =====

/// Wait a duration if inner `poll_ready` returns an error.
//TODO: move to tower-backoff
pub(super) struct Backoff<S> {
    inner: S,
    timer: Delay,
    waiting: bool,
    wait_dur: Duration,
}

impl<S> Backoff<S> {
    pub(super) fn new(inner: S, wait_dur: Duration) -> Self {
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
    S::Error: fmt::Debug,
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
pub(super) struct AddOrigin<S> {
    authority: http::uri::Authority,
    inner: S,
    scheme: http::uri::Scheme,
}

impl<S> AddOrigin<S> {
    pub(super) fn new(scheme: http::uri::Scheme, auth: http::uri::Authority, service: S) -> Self {
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
pub(super) struct LogErrors<S> {
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
    pub(super) fn new(service: S) -> Self {
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

pub(super) struct HumanError<'a>(&'a LogError);

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
