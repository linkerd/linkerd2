use std::io;
use std::net::SocketAddr;

use bytes::Buf;
use futures::Future;
use tokio::prelude::*;
use tokio::net::TcpStream;

use transport::{AddrInfo, io::internal::Io};

use super::{
    identity::Identity,
    rustls,
    tokio_rustls::{self, ClientConfigExt, ServerConfigExt, TlsStream},

    ClientConfig,
    ServerConfig,
};
use std::fmt::Debug;

pub use self::rustls::Session;

// In theory we could replace `TcpStream` with `Io`. However, it is likely that
// in the future we'll need to do things specific to `TcpStream`, so optimize
// for that unless/until there is some benefit to doing otherwise.
#[derive(Debug)]
pub struct Connection<S: Session>(TlsStream<TcpStream, S>);

pub struct UpgradeToTls<S, F>(F)
    where S: Session,
          F: Future<Item = TlsStream<TcpStream, S>, Error = io::Error>;

impl<S, F> Future for UpgradeToTls<S, F>
    where S: Session,
          F: Future<Item = TlsStream<TcpStream, S>, Error = io::Error>
{
    type Item = Connection<S>;
    type Error = io::Error;

    fn poll(&mut self) -> Result<Async<Self::Item>, Self::Error> {
        let tls_stream = try_ready!(self.0.poll());
        return Ok(Async::Ready(Connection(tls_stream)));
    }
}

pub type UpgradeClientToTls =
    UpgradeToTls<rustls::ClientSession, tokio_rustls::ConnectAsync<TcpStream>>;

pub type UpgradeServerToTls =
    UpgradeToTls<rustls::ServerSession, tokio_rustls::AcceptAsync<TcpStream>>;

impl Connection<rustls::ClientSession> {
    pub fn connect(socket: TcpStream, identity: &Identity, ClientConfig(config): ClientConfig)
        -> UpgradeClientToTls
    {
        UpgradeToTls(config.connect_async(identity.as_dns_name_ref(), socket))
    }
}

impl Connection<rustls::ServerSession> {
    pub fn accept(socket: TcpStream, ServerConfig(config): ServerConfig) -> UpgradeServerToTls
    {
        UpgradeToTls(config.accept_async(socket))
    }
}

impl<S: Session> io::Read for Connection<S> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        self.0.read(buf)
    }
}

impl<S: Session> AsyncRead for Connection<S> {
    unsafe fn prepare_uninitialized_buffer(&self, buf: &mut [u8]) -> bool {
        self.0.prepare_uninitialized_buffer(buf)
    }
}

impl<S: Session> io::Write for Connection<S> {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.0.write(buf)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.0.flush()
    }
}

impl<S: Session> AsyncWrite for Connection<S> {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        self.0.shutdown()
    }

    fn write_buf<B: Buf>(&mut self, buf: &mut B) -> Poll<usize, io::Error> {
        self.0.write_buf(buf)
    }
}

impl<S: Session + Debug> AddrInfo for Connection<S> {
    fn local_addr(&self) -> Result<SocketAddr, io::Error> {
        self.0.get_ref().0.local_addr()
    }

    fn get_original_dst(&self) -> Option<SocketAddr> {
        self.0.get_ref().0.get_original_dst()
    }
}

impl<S: Session + Debug> Io for Connection<S> {
    fn shutdown_write(&mut self) -> Result<(), io::Error> {
        self.0.get_mut().0.shutdown_write()
    }

    fn write_buf_erased(&mut self, mut buf: &mut Buf) -> Poll<usize, io::Error> {
        self.0.write_buf(&mut buf)
    }
}
