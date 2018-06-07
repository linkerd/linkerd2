use futures::Future;
use tokio_connect;

use std::{fmt, io};
use std::net::{IpAddr, SocketAddr};
use std::str::FromStr;

use http;

use connection;
use convert::TryFrom;
use dns;
use transport::tls;

#[derive(Debug, Clone)]
pub struct Connect {
    addr: SocketAddr,
}

#[derive(Clone, Debug)]
pub struct HostAndPort {
    pub host: Host,
    pub port: u16,
}

#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct DnsNameAndPort {
    pub host: dns::Name,
    pub port: u16,
}


#[derive(Clone, Debug)]
pub enum Host {
    DnsName(dns::Name),
    Ip(IpAddr),
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum HostAndPortError {
    /// The host is not a valid DNS name or IP address.
    InvalidHost,

    /// The port is missing.
    MissingPort,
}

#[derive(Debug, Clone)]
pub struct LookupAddressAndConnect {
    host_and_port: HostAndPort,
    dns_resolver: dns::Resolver,
}

// ===== impl HostAndPort =====

impl HostAndPort {
    pub fn normalize(a: &http::uri::Authority, default_port: Option<u16>)
        -> Result<Self, HostAndPortError>
    {
        let host = IpAddr::from_str(a.host())
            .map(Host::Ip)
            .or_else(|_|
                dns::Name::try_from(a.host())
                    .map(Host::DnsName)
                    .map_err(|_| HostAndPortError::InvalidHost))?;
        let port = a.port()
            .or(default_port)
            .ok_or_else(|| HostAndPortError::MissingPort)?;
        Ok(HostAndPort {
            host,
            port
        })
    }
}

impl<'a> From<&'a HostAndPort> for http::uri::Authority {
    fn from(a: &HostAndPort) -> Self {
        let s = match a.host {
            Host::DnsName(ref n) => format!("{}:{}", n, a.port),
            Host::Ip(ref ip) => format!("{}:{}", ip, a.port),
        };
        http::uri::Authority::from_str(&s).unwrap()
    }
}

impl fmt::Display for HostAndPort {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self.host {
            Host::DnsName(ref dns) => {
                write!(f, "{}:{}", dns, self.port)
            }
            Host::Ip(ref ip) => {
                write!(f, "{}:{}", ip, self.port)
            }
        }
    }
}

// ===== impl Connect =====

impl Connect {
    /// Returns a `Connect` to `addr`.
    pub fn new(
        addr: SocketAddr,
        tls_identity: Option<tls::Identity>,
    ) -> Self {
        // TODO: this is currently unused.
        let _ = tls_identity;
        Self {
            addr,
        }
    }
}

impl tokio_connect::Connect for Connect {
    type Connected = connection::Connection;
    type Error = io::Error;
    type Future = connection::Connecting;

    fn connect(&self) -> Self::Future {
        connection::connect(&self.addr)
    }
}

// ===== impl LookupAddressAndConnect =====

impl LookupAddressAndConnect {
    pub fn new(
        host_and_port: HostAndPort,
        dns_resolver: dns::Resolver,
    ) -> Self {
        Self {
            host_and_port,
            dns_resolver,
        }
    }
}

impl tokio_connect::Connect for LookupAddressAndConnect {
    type Connected = connection::Connection;
    type Error = io::Error;
    type Future = Box<Future<Item = connection::Connection, Error = io::Error> + Send>;

    fn connect(&self) -> Self::Future {
        let port = self.host_and_port.port;
        let host = self.host_and_port.host.clone();
        let c = self.dns_resolver
            .resolve_one_ip(&self.host_and_port.host)
            .map_err(|_| {
                io::Error::new(io::ErrorKind::NotFound, "DNS resolution failed")
            })
            .and_then(move |ip_addr: IpAddr| {
                info!("DNS resolved {:?} to {}", host, ip_addr);
                let addr = SocketAddr::from((ip_addr, port));
                trace!("connect {}", addr);
                connection::connect(&addr)
            });
        Box::new(c)
    }
}
