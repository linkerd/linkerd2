use bytes::{Buf, BytesMut};
use futures::*;
use std;
use std::cmp;
use std::io;
use std::net::SocketAddr;
use tokio_core::net::{TcpListener, TcpStreamNew, TcpStream};
use tokio_core::reactor::Handle;
use tokio_io::{AsyncRead, AsyncWrite};

use config::Addr;
use transport::GetOriginalDst;

pub type PlaintextSocket = TcpStream;

pub struct BoundPort {
    inner: std::net::TcpListener,
    local_addr: SocketAddr,
}

/// Initiates a client connection to the given address.
pub fn connect(addr: &SocketAddr, executor: &Handle) -> Connecting {
    Connecting(PlaintextSocket::connect(addr, executor))
}

/// A socket that is in the process of connecting.
pub struct Connecting(TcpStreamNew);

/// Abstracts a plaintext socket vs. a TLS decorated one.
///
/// A `Connection` has the `TCP_NODELAY` option set automatically. Also
/// it strictly controls access to information about the underlying
/// socket to reduce the chance of TLS protections being accidentally
/// subverted.
#[derive(Debug)]
pub struct Connection {
    io: Io,
    /// This buffer gets filled up when "peeking" bytes on this Connection.
    ///
    /// This is used instead of MSG_PEEK in order to support TLS streams.
    ///
    /// When calling `read`, it's important to consume bytes from this buffer
    /// before calling `io.read`.
    peek_buf: BytesMut,
}

#[derive(Debug)]
enum Io {
    Plain(PlaintextSocket),
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
    pub fn listen_and_fold<T, F, Fut>(self, executor: &Handle, initial: T, f: F)
        -> Box<Future<Item = (), Error = io::Error> + 'static>
        where
        F: Fn(T, (Connection, SocketAddr)) -> Fut + 'static,
        T: 'static,
        Fut: IntoFuture<Item = T, Error = std::io::Error> + 'static {
        let fut = TcpListener::from_listener(self.inner, &self.local_addr, &executor)
            .expect("from_listener") // TODO: get rid of this `expect()`.
            .incoming()
            .fold(initial, move |b, (socket, remote_addr)| {
                // TODO: On Linux and most other platforms it would be better
                // to set the `TCP_NODELAY` option on the bound socket and
                // then have the listening sockets inherit it. However, that
                // doesn't work on all platforms and also the underlying
                // libraries don't have the necessary API for that, so just
                // do it here.
                set_nodelay_or_warn(&socket);
                f(b, (Connection::plain(socket), remote_addr))
            });

        Box::new(fut.map(|_| ()))
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
            io: Io::Plain(socket),
            peek_buf: BytesMut::new(),
        }
    }

    pub fn original_dst_addr<T: GetOriginalDst>(&self, get: &T) -> Option<SocketAddr> {
        get.get_original_dst(self.socket())
    }

    pub fn local_addr(&self) -> Result<SocketAddr, std::io::Error> {
        self.socket().local_addr()
    }

    // This must never be made public so that in the future `Connection` can
    // control access to the plaintext socket for TLS, to ensure no private
    // data is accidentally writen to the socket and to ensure no unprotected
    // data is read from the socket. Each piece of information needed about the
    // underlying socket should be exposed by its own minimal accessor function
    // as is done above.
    fn socket(&self) -> &PlaintextSocket {
        match self.io {
            Io::Plain(ref socket) => socket
        }
    }
}

impl io::Read for Connection {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        // Check the length only once, since looking as the length
        // of a BytesMut isn't as cheap as the length of a &[u8].
        let peeked_len = self.peek_buf.len();

        if peeked_len == 0 {
            match self.io {
                Io::Plain(ref mut t) => t.read(buf),
            }
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
        match self.io {
            Io::Plain(ref t) => t.prepare_uninitialized_buffer(buf),
        }
    }
}

impl io::Write for Connection {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        match self.io {
            Io::Plain(ref mut t) => t.write(buf),
        }
    }

    fn flush(&mut self) -> io::Result<()> {
        match self.io {
            Io::Plain(ref mut t) => t.flush(),
        }
    }
}

impl AsyncWrite for Connection {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        use std::net::Shutdown;
        match self.io {
            Io::Plain(ref mut t) => {
                try_ready!(AsyncWrite::shutdown(t));
                // TCP shutdown the write side.
                //
                // If we're shutting down, then we definitely won't write
                // anymore. So, we should tell the remote about this. This
                // is relied upon in our TCP proxy, to start shutting down
                // the pipe if one side closes.
                TcpStream::shutdown(t, Shutdown::Write).map(Async::Ready)
            },
        }
    }

    fn write_buf<B: Buf>(&mut self, buf: &mut B) -> Poll<usize, io::Error> {
        match self.io {
            Io::Plain(ref mut t) => t.write_buf(buf),
        }
    }
}

impl Peek for Connection {
    fn poll_peek(&mut self) -> Poll<usize, io::Error> {
        if self.peek_buf.is_empty() {
            self.peek_buf.reserve(8192);
            match self.io {
                Io::Plain(ref mut t) => t.read_buf(&mut self.peek_buf),
            }
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
