use futures::{Async, Future, Poll};
use tokio_connect;
use tokio_core::net::{TcpStream, TcpStreamNew};
use tokio_core::reactor::{Handle, Timeout};
use url;

use std::io;
use std::net::{IpAddr, SocketAddr};
use std::time::Duration;

use dns;

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

#[derive(Debug, Clone)]
pub struct TimeoutConnect<C> {
    connect: C,
    timeout: Duration,
    handle: Handle,
}

pub struct TimeoutConnectFuture<F> {
    connect: F,
    duration: Duration,
    timeout: Timeout,
}

#[derive(Debug)]
pub enum TimeoutError<E> {
    Timeout(Duration),
    Connect(E),
}

// ===== impl TcpStreamNewNoDelay =====

impl Future for TcpStreamNewNoDelay {
    type Item = TcpStream;
    type Error = io::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let tcp = try_ready!(self.0.poll());
        if let Err(e) = tcp.set_nodelay(true) {
            warn!("could not set TCP_NODELAY on {:?}/{:?}: {}",
                    tcp.local_addr(), tcp.peer_addr(), e);
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
        handle: &Handle
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
        let c = self.dns_resolver.resolve_host(&self.host_and_port.host)
            .map_err(|_| io::Error::new(io::ErrorKind::NotFound, "DNS resolution failed"))
            .and_then(move |ip_addr: IpAddr| {
                info!("DNS resolved {} to {}", host, ip_addr);
                let addr = SocketAddr::from((ip_addr, port));
                trace!("connect {}", addr);
                TcpStreamNewNoDelay(TcpStream::connect(&addr, &handle))
            });
        Box::new(c)
    }
}

// ===== impl TimeoutConnect =====

impl<C: tokio_connect::Connect> TimeoutConnect<C> {
    /// Returns a `Connect` to `addr` and `handle`.
    pub fn new(connect: C, timeout: Duration, handle: &Handle) -> Self {
        Self {
            connect,
            timeout,
            handle: handle.clone(),
        }
    }
}

impl<C: tokio_connect::Connect> tokio_connect::Connect for TimeoutConnect<C> {
    type Connected = C::Connected;
    type Error = TimeoutError<C::Error>;
    type Future = TimeoutConnectFuture<C::Future>;

    fn connect(&self) -> Self::Future {
        let connect = self.connect.connect();
        let duration = self.timeout;
        let timeout = Timeout::new(duration, &self.handle).unwrap();
        TimeoutConnectFuture { connect, duration, timeout }
    }
}

impl<F: Future> Future for TimeoutConnectFuture<F> {
    type Item = F::Item;
    type Error = TimeoutError<F::Error>;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        if let Async::Ready(tcp) = self.connect.poll().map_err(TimeoutError::Connect)? {
            return Ok(Async::Ready(tcp));
        }

        if let Async::Ready(_) = self.timeout.poll().expect("timer failed") {
            return Err(TimeoutError::Timeout(self.duration));
        }

        Ok(Async::NotReady)
    }
}
