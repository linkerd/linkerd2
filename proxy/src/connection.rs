use futures::*;
use std;
use std::io;
use std::net::SocketAddr;
use tokio_core;
use tokio_core::net::TcpListener;
use tokio_core::reactor::Handle;
use tokio_io::{AsyncRead, AsyncWrite};

use config::Addr;
use transport;

pub type PlaintextSocket = tokio_core::net::TcpStream;

pub struct BoundPort {
    inner: std::net::TcpListener,
    local_addr: SocketAddr,
}

/// Abstracts a plaintext socket vs. a TLS decorated one.
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
                if let Err(e) = socket.set_nodelay(true) {
                    warn!(
                        "could not set TCP_NODELAY on {:?}/{:?}: {}",
                        socket.local_addr(),
                        socket.peer_addr(),
                        e
                    );
                }
                f(b, (Connection::Plain(socket), remote_addr))
            });

        Box::new(fut.map(|_| ()))
    }
}

// ===== impl Connection =====

impl Connection {
    pub fn original_dst_addr(&self) -> Option<SocketAddr> {
        transport::get_original_dst(self.socket())
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

// TODO: impl specialty functions
impl AsyncRead for Connection {}

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

// TODO: impl specialty functions
impl AsyncWrite for Connection {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        use self::Connection::*;

        match *self {
            Plain(ref mut t) => t.shutdown(),
        }
    }
}
