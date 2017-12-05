use futures::*;
use std::io;
use tokio_core;
use tokio_io::{AsyncRead, AsyncWrite};

pub type PlaintextSocket = tokio_core::net::TcpStream;

/// Abstracts a plaintext socket vs. a TLS decorated one.
#[derive(Debug)]
pub enum Connection {
    Plain(PlaintextSocket),
}

/// A connection handshake.
///
/// Resolves to a connection ready to be used at the next layer.
pub struct Handshake {
    plaintext_socket: Option<PlaintextSocket>,
}

// ===== impl Connection =====

impl Connection {
    /// Establish a connection backed by the provided `io`.
    pub fn handshake(io: PlaintextSocket) -> Handshake {
        Handshake {
            plaintext_socket: Some(io),
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

// ===== impl Handshake =====

impl Future for Handshake {
    type Item = Connection;
    type Error = io::Error;

    fn poll(&mut self) -> Poll<Self::Item, io::Error> {
        let plaintext_socket = self.plaintext_socket.take().expect("poll after complete");
        Ok(Connection::Plain(plaintext_socket).into())
    }
}
