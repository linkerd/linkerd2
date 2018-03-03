use futures::{Future, Poll};
use std::io;
use std::sync::Arc;
use std::time::Instant;
use tokio_connect;
use tokio_io::{AsyncRead, AsyncWrite};

use ctx;
use telemetry::event;
use time::Timer;

/// Wraps a transport with telemetry.
#[derive(Debug)]
pub struct Transport<I, T>(I, Option<Inner<T>>);

#[derive(Debug)]
struct Inner<T> {
    handle: super::Handle<T>,
    ctx: Arc<ctx::transport::Ctx>,
    opened_at: Instant,

    // TODO
    //rx_bytes: usize,
    //tx_bytes: usize,
}

/// Builds client transports with telemetry.
#[derive(Clone, Debug)]
pub struct Connect<C, T> {
    underlying: C,
    handle: super::Handle<T>,
    ctx: Arc<ctx::transport::Client>,
}

/// Adds telemetry to a pending client transport.
#[derive(Clone, Debug)]
pub struct Connecting<C: tokio_connect::Connect, T> {
    underlying: C::Future,
    handle: super::Handle<T>,
    ctx: Arc<ctx::transport::Client>,
}

// === impl Transport ===

impl<I, T> Transport<I, T>
where
    I: AsyncRead + AsyncWrite,
    T: Timer,
{
    /// Wraps a transport with telemetry and emits a transport open event.
    pub(super) fn open(
        io: I,
        opened_at: Instant,
        handle: &super::Handle<T>,
        ctx: Arc<ctx::transport::Ctx>,
    ) -> Self {
        let mut handle = handle.clone();

        handle.send(|| event::Event::TransportOpen(Arc::clone(&ctx)));

        Transport(
            io,
            Some(Inner {
                ctx,
                handle,
                opened_at,
            }),
        )
    }

    /// Wraps an operation on the underlying transport with error telemetry.
    ///
    /// If the transport operation results in a non-recoverable error, a transport close
    /// event is emitted.
    fn sense_err<F, U>(&mut self, op: F) -> io::Result<U>
    where
        F: FnOnce(&mut I) -> io::Result<U>,
    {
        match op(&mut self.0) {
            Ok(v) => Ok(v),
            Err(e) => {
                if e.kind() != io::ErrorKind::WouldBlock {
                    if let Some(Inner {
                        mut handle,
                        ctx,
                        opened_at,
                    }) = self.1.take()
                    {
                        let duration = handle.timer.elapsed(opened_at);
                        handle.send(move || {
                            let ev = event::TransportClose {
                                duration,
                                clean: false,
                            };
                            event::Event::TransportClose(ctx, ev)
                        });
                    }
                }

                Err(e)
            }
        }
    }
}

impl<I, T> Drop for Transport<I, T> {
    fn drop(&mut self) {
        if let Some(Inner {
            mut handle,
            ctx,
            opened_at,
        }) = self.1.take()
        {
            handle.send(move || {
                let duration = opened_at.elapsed();
                let ev = event::TransportClose {
                    clean: true,
                    duration,
                };
                event::Event::TransportClose(ctx, ev)
            });
        }
    }
}

impl<I, T> io::Read for Transport<I, T>
where
    I: AsyncRead + AsyncWrite,
    T: Timer,
{
    fn read(&mut self, mut buf: &mut [u8]) -> io::Result<usize> {
        self.sense_err(move |io| io.read(buf))
    }
}

impl<I, T> io::Write for Transport<I, T>
where
    I: AsyncRead + AsyncWrite,
    T: Timer,
{
    fn flush(&mut self) -> io::Result<()> {
        self.sense_err(|io| io.flush())
    }

    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.sense_err(move |io| io.write(buf))
    }
}

impl<I, T> AsyncRead for Transport<I, T>
where
    I: AsyncRead + AsyncWrite,
    T: Timer,
{
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        self.0.prepare_uninitialized_buffer(buf)
    }
}

impl<I, T> AsyncWrite for Transport<I, T>
where
    I: AsyncRead + AsyncWrite,
    T: Timer,
{
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        self.sense_err(|io| io.shutdown())
    }
}

// === impl Connect ===

impl<C, T> Connect<C, T>
where
    C: tokio_connect::Connect,
    T: Clone,
{
    /// Returns a `Connect` to `addr` and `handle`.
    pub(super) fn new(
        underlying: C,
        handle: &super::Handle<T>,
        ctx: &Arc<ctx::transport::Client>,
    ) -> Self {
        Connect {
            underlying,
            handle: handle.clone(),
            ctx: Arc::clone(ctx),
        }
    }
}

impl<C, T> tokio_connect::Connect for Connect<C, T>
where
    C: tokio_connect::Connect,
    T: Timer,
{
    type Connected = Transport<C::Connected, T>;
    type Error = C::Error;
    type Future = Connecting<C, T>;

    fn connect(&self) -> Self::Future {
        Connecting {
            underlying: self.underlying.connect(),
            handle: self.handle.clone(),
            ctx: Arc::clone(&self.ctx),
        }
    }
}

// === impl Connecting ===

impl<C, T> Future for Connecting<C, T>
where
    C: tokio_connect::Connect,
    T: Timer,
{
    type Item = Transport<C::Connected, T>;
    type Error = C::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let io = try_ready!(self.underlying.poll());
        debug!("client connection open");
        let ctx = Arc::new(Arc::clone(&self.ctx).into());
        let opened_at = self.handle.timer.now();
        let trans = Transport::open(io, opened_at, &self.handle, ctx);
        Ok(trans.into())
    }
}
