use bytes::{Buf, BytesMut};
use futures::*;
use std;
use std::cmp;
use std::io;
use std::net::SocketAddr;
use tokio::{
    io::{AsyncRead, AsyncWrite},
    net::{TcpListener, TcpStream, ConnectFuture},
    reactor::Handle,
};

use config::Addr;
use transport::GetOriginalDst;
use transport::Io;

pub type PlaintextSocket = TcpStream;

pub struct BoundPort {
    inner: std::net::TcpListener,
    local_addr: SocketAddr,
}

/// Initiates a client connection to the given address.
pub fn connect(addr: &SocketAddr) -> Connecting {
    Connecting(PlaintextSocket::connect(addr))
}

/// A socket that is in the process of connecting.
pub struct Connecting(ConnectFuture);

/// Abstracts a plaintext socket vs. a TLS decorated one.
///
/// A `Connection` has the `TCP_NODELAY` option set automatically. Also
/// it strictly controls access to information about the underlying
/// socket to reduce the chance of TLS protections being accidentally
/// subverted.
#[derive(Debug)]
pub struct Connection {
    io: Box<Io>,
    /// This buffer gets filled up when "peeking" bytes on this Connection.
    ///
    /// This is used instead of MSG_PEEK in order to support TLS streams.
    ///
    /// When calling `read`, it's important to consume bytes from this buffer
    /// before calling `io.read`.
    peek_buf: BytesMut,
}

/// A trait describing that a type can peek bytes.
pub trait Peek {
    /// An async attempt to peek bytes of this type without consuming.
    ///
    /// Returns number of bytes that have been peeked.
    fn poll_peek(&mut self) -> Poll<usize, io::Error>;

    /// Returns a reference to the bytes that have been peeked.
    // Instead of passing a buffer into `peek()`, the bytes are kept in
    // a buffer owned by the `Peek` type. This allows looking at the
    // peeked bytes cheaply, instead of needing to copy into a new
    // buffer.
    fn peeked(&self) -> &[u8];

    /// A `Future` around `poll_peek`, returning this type instead.
    fn peek(self) -> PeekFuture<Self> where Self: Sized {
        PeekFuture {
            inner: Some(self),
        }
    }
}

/// A future of when some `Peek` fulfills with some bytes.
#[derive(Debug)]
pub struct PeekFuture<T> {
    inner: Option<T>,
}

// ===== impl BoundPort =====

impl BoundPort {
    pub fn new(addr: Addr) -> Result<Self, io::Error> {
        let inner = std::net::TcpListener::bind(SocketAddr::from(addr))?;
        let local_addr = inner.local_addr()?;
        Ok(BoundPort {
            inner,
            local_addr,
        })
    }

    pub fn local_addr(&self) -> SocketAddr {
        self.local_addr
    }

    // Listen for incoming connections and dispatch them to the handler `f`.
    //
    // This ensures that every incoming connection has the correct options set.
    // In the future it will also ensure that the connection is upgraded with
    // TLS when needed.
    pub fn listen_and_fold<T, F, Fut>(self, initial: T, f: F)
        -> impl Future<Item = (), Error = io::Error> + Send + 'static
    where
        F: Fn(T, (Connection, SocketAddr)) -> Fut + Send + 'static,
        T: Send + 'static,
        Fut: IntoFuture<Item = T, Error = std::io::Error> + Send + 'static,
        <Fut as IntoFuture>::Future: Send, {
        future::lazy(move || {
            // Create the TCP listener lazily, so that it's not bound to a
            // reactor until the future is run. This will avoid
            // `Handle::current()` creating a mew thread for the global
            // background reactor if `listen_and_fold` is called before we've
            // initialized the runtime.
            TcpListener::from_std(self.inner, &Handle::current())
        }).and_then(|listener|
            listener.incoming()
                .and_then(move |socket| {
                    let remote_addr = socket.peer_addr()
                        .expect("couldn't get remote addr!");
                    // TODO: On Linux and most other platforms it would be better
                    // to set the `TCP_NODELAY` option on the bound socket and
                    // then have the listening sockets inherit it. However, that
                    // doesn't work on all platforms and also the underlying
                    // libraries don't have the necessary API for that, so just
                    // do it here.
                    set_nodelay_or_warn(&socket);
                    let connection = Connection::plain(socket);
                    future::ok((connection, remote_addr))
                })
                .fold(initial, f)
        )
        .map(|_| ())
    }
}

// ===== impl Connecting =====

impl Future for Connecting {
    type Item = Connection;
    type Error = io::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let socket = try_ready!(self.0.poll());
        set_nodelay_or_warn(&socket);
        Ok(Async::Ready(Connection::plain(socket)))
    }
}

// ===== impl Connection =====

impl Connection {
    /// A constructor of `Connection` with a plain text TCP socket.
    pub fn plain(socket: PlaintextSocket) -> Self {
        Connection {
            io: Box::new(socket),
            peek_buf: BytesMut::new(),
        }
    }

    pub fn original_dst_addr<T: GetOriginalDst>(&self, get: &T) -> Option<SocketAddr> {
        get.get_original_dst(&self.io)
    }

    pub fn local_addr(&self) -> Result<SocketAddr, std::io::Error> {
        self.io.local_addr()
    }
}

impl io::Read for Connection {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        // Check the length only once, since looking as the length
        // of a BytesMut isn't as cheap as the length of a &[u8].
        let peeked_len = self.peek_buf.len();

        if peeked_len == 0 {
            self.io.read(buf)
        } else {
            let len = cmp::min(buf.len(), peeked_len);
            buf[..len].copy_from_slice(&self.peek_buf.as_ref()[..len]);
            self.peek_buf.advance(len);
            // If we've finally emptied the peek_buf, drop it so we don't
            // hold onto the allocated memory any longer. We won't peek
            // again.
            if peeked_len == len {
                self.peek_buf = BytesMut::new();
            }
            Ok(len)
        }
    }
}

impl AsyncRead for Connection {
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        self.io.prepare_uninitialized_buffer(buf)
    }
}

impl io::Write for Connection {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.io.write(buf)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.io.flush()
    }
}

impl AsyncWrite for Connection {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        try_ready!(AsyncWrite::shutdown(&mut self.io));

        // TCP shutdown the write side.
        //
        // If we're shutting down, then we definitely won't write
        // anymore. So, we should tell the remote about this. This
        // is relied upon in our TCP proxy, to start shutting down
        // the pipe if one side closes.
        self.io.shutdown_write().map(Async::Ready)
    }

    fn write_buf<B: Buf>(&mut self, buf: &mut B) -> Poll<usize, io::Error> {
        self.io.write_buf(buf)
    }
}

impl Peek for Connection {
    fn poll_peek(&mut self) -> Poll<usize, io::Error> {
        if self.peek_buf.is_empty() {
            self.peek_buf.reserve(8192);
            self.io.read_buf(&mut self.peek_buf)
        } else {
            Ok(Async::Ready(self.peek_buf.len()))
        }
    }

    fn peeked(&self) -> &[u8] {
        self.peek_buf.as_ref()
    }
}

// impl PeekFuture

impl<T: Peek> Future for PeekFuture<T> {
    type Item = T;
    type Error = std::io::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let mut io = self.inner.take().expect("polled after completed");
        match io.poll_peek() {
            Ok(Async::Ready(_)) => Ok(Async::Ready(io)),
            Ok(Async::NotReady) => {
                self.inner = Some(io);
                Ok(Async::NotReady)
            },
            Err(e) => Err(e),
        }
    }
}

// Misc.

fn set_nodelay_or_warn(socket: &PlaintextSocket) {
    if let Err(e) = socket.set_nodelay(true) {
        warn!(
            "could not set TCP_NODELAY on {:?}/{:?}: {}",
            socket.local_addr(),
            socket.peer_addr(),
            e
        );
    }
}
