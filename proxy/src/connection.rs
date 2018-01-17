use bytes::Buf;
use futures::*;
use std;
use std::io;
use std::net::SocketAddr;
use tokio_core;
use tokio_core::net::{TcpListener, TcpStreamNew};
use tokio_core::reactor::Handle;
use tokio_io::{AsyncRead, AsyncWrite};

use config::Addr;
use transport::GetOriginalDst;

pub type PlaintextSocket = tokio_core::net::TcpStream;

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
pub enum Connection {
    Plain(PlaintextSocket),
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
                f(b, (Connection::Plain(socket), remote_addr))
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
        Ok(Async::Ready(Connection::Plain(socket)))
    }
}

// ===== impl Connection =====

impl Connection {
    pub fn original_dst_addr<T: GetOriginalDst>(&self, get: &T) -> Option<SocketAddr> {
        get.get_original_dst(self.socket())
    }

    pub fn local_addr(&self) -> Result<SocketAddr, std::io::Error> {
        self.socket().local_addr()
    }

    pub fn peek_future<T: AsMut<[u8]>>(self, buf: T) -> Peek<T> {
        Peek {
            inner: Some((self, buf))
        }
    }

    // This must never be made public so that in the future `Connection` can
    // control access to the plaintext socket for TLS, to ensure no private
    // data is accidentally writen to the socket and to ensure no unprotected
    // data is read from the socket. Each piece of information needed about the
    // underlying socket should be exposed by its own minimal accessor function
    // as is done above.
    fn socket(&self) -> &PlaintextSocket {
        match self {
            &Connection::Plain(ref socket) => socket
        }
    }
}

impl io::Read for Connection {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        use self::Connection::*;

        match *self {
            Plain(ref mut t) => t.read(buf),
        }
    }
}

impl AsyncRead for Connection {
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        use self::Connection::*;

        match *self {
            Plain(ref t) => t.prepare_uninitialized_buffer(buf),
        }
    }
}

impl io::Write for Connection {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        use self::Connection::*;

        match *self {
            Plain(ref mut t) => t.write(buf),
        }
    }

    fn flush(&mut self) -> io::Result<()> {
        use self::Connection::*;

        match *self {
            Plain(ref mut t) => t.flush(),
        }
    }
}

impl AsyncWrite for Connection {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        use self::Connection::*;

        match *self {
            Plain(ref mut t) => t.shutdown(),
        }
    }

    fn write_buf<B: Buf>(&mut self, buf: &mut B) -> Poll<usize, io::Error> {
        use self::Connection::*;

        match *self {
            Plain(ref mut t) => t.write_buf(buf),
        }
    }
}

// impl Peek

pub struct Peek<T> {
    inner: Option<(Connection, T)>,
}

impl<T: AsMut<[u8]>> Future for Peek<T> {
    type Item = (Connection, T, usize);
    type Error = std::io::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let (conn, mut buf) = self.inner.take().expect("polled after completed");
        match conn.socket().peek(buf.as_mut()) {
            Ok(n) => Ok(Async::Ready((conn, buf, n))),
            Err(e) => match e.kind() {
                std::io::ErrorKind::WouldBlock => {
                    self.inner = Some((conn, buf));
                    Ok(Async::NotReady)
                },
                _ => Err(e)
            },
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
