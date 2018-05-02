// #![deny(missing_docs)]
use futures::{Async, Future, Poll};

use std::error::Error;
use std::{fmt, io};
use std::time::Duration;

use tokio_connect::Connect;
use tokio::reactor::{Timeout as ReactorTimeout, Handle};
use tokio::io::{AsyncRead, AsyncWrite};
use tower_service::Service;

/// A timeout that wraps an underlying operation.
#[derive(Debug, Clone)]
pub struct Timeout<U> {
    inner: U,
    duration: Duration,
    handle: Handle,
}

/// An error representing that an operation timed out.
#[derive(Debug)]
pub enum TimeoutError<E> {
    /// Indicates the underlying operation timed out.
    Timeout(Duration),
    /// Indicates that the underlying operation failed.
    Error(E),
}

/// A `Future` wrapped with a `Timeout`.
pub struct TimeoutFuture<F> {
    inner: F,
    duration: Duration,
    timeout: ReactorTimeout,
}

/// A duration which pretty-prints as fractional seconds.
///
/// This may not be the ideal display format for _all_ duration values,
/// but should be sufficient for most timeouts.
#[derive(Copy, Clone, Debug)]
pub struct HumanDuration(pub Duration);

//===== impl Timeout =====

impl<U> Timeout<U> {
    /// Construct a new `Timeout` wrapping `inner`.
    pub fn new(inner: U, duration: Duration, handle: &Handle) -> Self {
        Timeout {
            inner,
            duration,
            handle: handle.clone(),
        }
    }

}

impl<S, T, E> Service for Timeout<S>
where
    S: Service<Response=T, Error=E>,
{
    type Request = S::Request;
    type Response = T;
    type Error = TimeoutError<E>;
    type Future = TimeoutFuture<S::Future>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready().map_err(Self::Error::from)
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        let duration = self.duration;
        let timeout = ReactorTimeout::new(duration, &self.handle)
            .expect("reactor gone");
        let inner = self.inner.call(req);
        TimeoutFuture {
            inner,
            duration,
            timeout,
        }
    }
}


impl<C> Connect for Timeout<C>
where
    C: Connect,
{
    type Connected = C::Connected;
    type Error = TimeoutError<C::Error>;
    type Future = TimeoutFuture<C::Future>;

    fn connect(&self) -> Self::Future {
        let duration = self.duration;
        let timeout = ReactorTimeout::new(duration, &self.handle)
            .expect("reactor gone");
        let inner = self.inner.connect();
        TimeoutFuture {
            inner,
            duration,
            timeout,
        }
    }
}

impl<C> io::Read for Timeout<C>
where
    C: io::Read,
{
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        self.inner.read(buf)
    }
}

impl<C> io::Write for Timeout<C>
where
    C: io::Write,
{
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.inner.write(buf)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.inner.flush()
    }
}

impl<C> AsyncRead for Timeout<C>
where
    C: AsyncRead,
{
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        self.inner.prepare_uninitialized_buffer(buf)
    }
}

impl<C> AsyncWrite for Timeout<C>
where
    C: AsyncWrite,
{
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        self.inner.shutdown()
    }
}

//===== impl TimeoutError =====

impl<E> From<E> for TimeoutError<E> {
    #[inline]
    fn from(error: E) -> Self {
        TimeoutError::Error(error)
    }
}

impl<E> fmt::Display for TimeoutError<E>
where
    E: fmt::Display
{
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            TimeoutError::Timeout(ref d) =>
                write!(f, "operation timed out after {}", HumanDuration(*d)),
            TimeoutError::Error(ref err) => fmt::Display::fmt(err, f),
        }
    }
}

impl<E> Error for TimeoutError<E>
where
    E: Error
{
    fn cause(&self) -> Option<&Error> {
        match *self {
            TimeoutError::Error(ref err) => Some(err),
            _ => None,
        }
    }

    fn description(&self) -> &str {
        match *self {
            TimeoutError::Timeout(_) => "operation timed out",
            TimeoutError::Error(ref err) => err.description(),
        }
    }
}

//===== impl TimeoutFuture =====

impl<F> Future for TimeoutFuture<F>
where
    F: Future,
    // F::Error: Error,
{
    type Item = F::Item;
    type Error = TimeoutError<F::Error>;
    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        if let Async::Ready(item) = self.inner.poll().map_err(TimeoutError::from)? {
            Ok(Async::Ready(item))
        } else if let Async::Ready(_) = self.timeout.poll().expect("timer failed") {
            Err(TimeoutError::Timeout(self.duration))
        } else {
            Ok(Async::NotReady)
        }
    }
}

// We have to provide a custom implementation of Debug, because
// tokio::reactor::Timeout is not Debug.
impl<F> fmt::Debug for TimeoutFuture<F>
where
    F: fmt::Debug
{
    fn fmt(&self, fmt: &mut fmt::Formatter) -> fmt::Result {
        fmt.debug_struct("TimeoutFuture")
           .field("inner", &self.inner)
           .field("duration", &self.duration)
           .finish()
    }
}

//===== impl HumanDuration =====

impl fmt::Display for HumanDuration {
    fn fmt(&self, fmt: &mut fmt::Formatter) -> fmt::Result {
        let secs = self.0.as_secs();
        let subsec_ms = self.0.subsec_nanos() as f64 / 1_000_000f64;
        if secs == 0 {
            write!(fmt, "{}ms", subsec_ms)
        } else {
            write!(fmt, "{}s", secs as f64 + subsec_ms)
        }
    }
}

impl From<Duration> for HumanDuration {

    #[inline]
    fn from(d: Duration) -> Self {
        HumanDuration(d)
    }
}
