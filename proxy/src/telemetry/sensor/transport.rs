use futures::{Future, Poll};
use std::io;
use std::sync::Arc;
use std::time::Instant;
use tokio_connect;
use tokio_io::{AsyncRead, AsyncWrite};

use ctx;
use telemetry::event;

/// Wraps a transport with telemetry.
#[derive(Debug)]
pub struct Transport<T>(T, Option<Inner>);

#[derive(Debug)]
struct Inner {
    handle: super::Handle,
    ctx: Arc<ctx::transport::Ctx>,
    opened_at: Instant,

    // TODO
    //rx_bytes: usize,
    //tx_bytes: usize,
}

/// Builds client transports with telemetry.
#[derive(Clone, Debug)]
pub struct Connect<C> {
    underlying: C,
    handle: super::Handle,
    ctx: Arc<ctx::transport::Client>,
}

/// Adds telemetry to a pending client transport.
#[derive(Clone, Debug)]
pub struct Connecting<C: tokio_connect::Connect> {
    underlying: C::Future,
    handle: super::Handle,
    ctx: Arc<ctx::transport::Client>,
}

// === impl Transport ===

impl<T: AsyncRead + AsyncWrite> Transport<T> {
    /// Wraps a transport with telemetry and emits a transport open event.
    pub(super) fn open(
        io: T,
        opened_at: Instant,
        handle: &super::Handle,
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
        F: FnOnce(&mut T) -> io::Result<U>,
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
                        handle.send(move || {
                            let duration = opened_at.elapsed();
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

impl<T> Drop for Transport<T> {
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

impl<T: AsyncRead + AsyncWrite> io::Read for Transport<T> {
    fn read(&mut self, mut buf: &mut [u8]) -> io::Result<usize> {
        self.sense_err(move |io| io.read(buf))
    }
}

impl<T: AsyncRead + AsyncWrite> io::Write for Transport<T> {
    fn flush(&mut self) -> io::Result<()> {
        self.sense_err(|io| io.flush())
    }

    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.sense_err(move |io| io.write(buf))
    }
}

impl<T: AsyncRead + AsyncWrite> AsyncRead for Transport<T> {
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        self.0.prepare_uninitialized_buffer(buf)
    }
}

impl<T: AsyncRead + AsyncWrite> AsyncWrite for Transport<T> {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        self.sense_err(|io| io.shutdown())
    }
}

// === impl Connect ===

impl<C: tokio_connect::Connect> Connect<C> {
    /// Returns a `Connect` to `addr` and `handle`.
    pub(super) fn new(
        underlying: C,
        handle: &super::Handle,
        ctx: &Arc<ctx::transport::Client>,
    ) -> Self {
        Connect {
            underlying,
            handle: handle.clone(),
            ctx: Arc::clone(ctx),
        }
    }
}

impl<C: tokio_connect::Connect> tokio_connect::Connect for Connect<C> {
    type Connected = Transport<C::Connected>;
    type Error = C::Error;
    type Future = Connecting<C>;

    fn connect(&self) -> Self::Future {
        Connecting {
            underlying: self.underlying.connect(),
            handle: self.handle.clone(),
            ctx: Arc::clone(&self.ctx),
        }
    }
}

// === impl Connecting ===

impl<C: tokio_connect::Connect> Future for Connecting<C> {
    type Item = Transport<C::Connected>;
    type Error = C::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let io = try_ready!(self.underlying.poll());
        debug!("client connection open");
        let ctx = Arc::new(Arc::clone(&self.ctx).into());
        let trans = Transport::open(io, Instant::now(), &self.handle, ctx);
        Ok(trans.into())
    }
}
