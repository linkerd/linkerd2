use futures::{Async, Future, Poll};
use tokio_connect;
use tokio_core::net::{TcpStream, TcpStreamNew};
use tokio_core::reactor::Handle;
use url;

use std::io;
use std::net::{IpAddr, SocketAddr};

use dns;
use ::timeout;

#[must_use = "futures do nothing unless polled"]
pub struct TcpStreamNewNoDelay(TcpStreamNew);

#[derive(Debug, Clone)]
pub struct Connect {
    addr: SocketAddr,
    handle: Handle,
}

#[derive(Debug, Clone)]
pub struct LookupAddressAndConnect {
    host_and_port: url::HostAndPort,
    dns_resolver: dns::Resolver,
    handle: Handle,
}

pub type TimeoutConnect<C> = timeout::Timeout<C>;
pub type TimeoutError<E> = timeout::TimeoutError<E>;

// ===== impl TcpStreamNewNoDelay =====

impl Future for TcpStreamNewNoDelay {
    type Item = TcpStream;
    type Error = io::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let tcp = try_ready!(self.0.poll());
        if let Err(e) = tcp.set_nodelay(true) {
            warn!(
                "could not set TCP_NODELAY on {:?}/{:?}: {}",
                tcp.local_addr(),
                tcp.peer_addr(),
                e
            );
        }
        Ok(Async::Ready(tcp))
    }
}

// ===== impl Connect =====

impl Connect {
    /// Returns a `Connect` to `addr` and `handle`.
    pub fn new(addr: SocketAddr, handle: &Handle) -> Self {
        Self {
            addr,
            handle: handle.clone(),
        }
    }
}

impl tokio_connect::Connect for Connect {
    type Connected = TcpStream;
    type Error = io::Error;
    type Future = TcpStreamNewNoDelay;

    fn connect(&self) -> Self::Future {
        trace!("connect {}", self.addr);
        TcpStreamNewNoDelay(TcpStream::connect(&self.addr, &self.handle))
    }
}

// ===== impl LookupAddressAndConnect =====

impl LookupAddressAndConnect {
    pub fn new(
        host_and_port: url::HostAndPort,
        dns_resolver: dns::Resolver,
        handle: &Handle,
    ) -> Self {
        Self {
            host_and_port,
            dns_resolver,
            handle: handle.clone(),
        }
    }
}

impl tokio_connect::Connect for LookupAddressAndConnect {
    type Connected = TcpStream;
    type Error = io::Error;
    type Future = Box<Future<Item = TcpStream, Error = io::Error>>;

    fn connect(&self) -> Self::Future {
        let port = self.host_and_port.port;
        let handle = self.handle.clone();
        let host = self.host_and_port.host.clone();
        let c = self.dns_resolver
            .resolve_host(&self.host_and_port.host)
            .map_err(|_| {
                io::Error::new(io::ErrorKind::NotFound, "DNS resolution failed")
            })
            .and_then(move |ip_addr: IpAddr| {
                info!("DNS resolved {} to {}", host, ip_addr);
                let addr = SocketAddr::from((ip_addr, port));
                trace!("connect {}", addr);
                TcpStreamNewNoDelay(TcpStream::connect(&addr, &handle))
            });
        Box::new(c)
    }
}