use futures::Future;
use tokio_connect;
use tokio_core::reactor::Handle;
use url;

use std::io;
use std::net::{IpAddr, SocketAddr};

use connection;
use dns;
use ::timeout;

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
    type Connected = connection::Connection;
    type Error = io::Error;
    type Future = connection::Connecting;

    fn connect(&self) -> Self::Future {
        connection::connect(&self.addr, &self.handle)
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
    type Connected = connection::Connection;
    type Error = io::Error;
    type Future = Box<Future<Item = connection::Connection, Error = io::Error>>;

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
                connection::connect(&addr, &handle)
            });
        Box::new(c)
    }
}
