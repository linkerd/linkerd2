use std::net::{IpAddr, SocketAddr};
use std::sync::Arc;

use ctx;
use control::destination;
use telemetry::metrics::DstLabels;

#[derive(Debug)]
pub enum Ctx {
    Client(Arc<Client>),
    Server(Arc<Server>),
}

/// Identifies a connection from another process to a proxy listener.
#[derive(Debug)]
pub struct Server {
    pub proxy: Arc<ctx::Proxy>,
    pub remote: SocketAddr,
    pub local: SocketAddr,
    pub orig_dst: Option<SocketAddr>,
}

/// Identifies a connection from the proxy to another process.
#[derive(Debug)]
pub struct Client {
    pub proxy: Arc<ctx::Proxy>,
    pub remote: SocketAddr,
    pub metadata: destination::Metadata,
}

impl Ctx {
    pub fn proxy(&self) -> &Arc<ctx::Proxy> {
        match *self {
            Ctx::Client(ref ctx) => &ctx.proxy,
            Ctx::Server(ref ctx) => &ctx.proxy,
        }
    }
}

impl Server {
    pub fn new(
        proxy: &Arc<ctx::Proxy>,
        local: &SocketAddr,
        remote: &SocketAddr,
        orig_dst: &Option<SocketAddr>,
    ) -> Arc<Server> {
        let s = Server {
            proxy: Arc::clone(proxy),
            local: *local,
            remote: *remote,
            orig_dst: *orig_dst,
        };

        Arc::new(s)
    }

    pub fn orig_dst_if_not_local(&self) -> Option<SocketAddr> {
        match self.orig_dst {
            None => None,
            Some(orig_dst) => {
                // If the original destination is actually the listening socket,
                // we don't want to create a loop.
                if same_addr(&orig_dst, &self.local) {
                    None
                } else {
                    Some(orig_dst)
                }
            }
        }
    }
}

fn same_addr(a0: &SocketAddr, a1: &SocketAddr) -> bool {
    (a0.port() == a1.port()) && match (a0.ip(), a1.ip()) {
        (IpAddr::V6(a0), IpAddr::V4(a1)) => a0.to_ipv4() == Some(a1),
        (IpAddr::V4(a0), IpAddr::V6(a1)) => Some(a0) == a1.to_ipv4(),
        (a0, a1) => (a0 == a1),
    }
}

impl Client {
    pub fn new(
        proxy: &Arc<ctx::Proxy>,
        remote: &SocketAddr,
        metadata: destination::Metadata,
    ) -> Arc<Client> {
        let c = Client {
            proxy: Arc::clone(proxy),
            remote: *remote,
            metadata,
        };

        Arc::new(c)
    }

    pub fn tls_identity(&self) -> Option<&destination::TlsIdentity> {
        self.metadata.tls_identity()
    }

    pub fn dst_labels(&self) -> Option<&DstLabels> {
        self.metadata.dst_labels()
    }
}

impl From<Arc<Client>> for Ctx {
    fn from(c: Arc<Client>) -> Self {
        Ctx::Client(c)
    }
}

impl From<Arc<Server>> for Ctx {
    fn from(s: Arc<Server>) -> Self {
        Ctx::Server(s)
    }
}

#[cfg(test)]
mod tests {
    use std::net;

    use quickcheck::TestResult;

    use super::same_addr;

    quickcheck! {
        fn same_addr_ipv4(ip0: net::Ipv4Addr, ip1: net::Ipv4Addr, port0: u16, port1: u16) -> TestResult {
            if port0 == 0 || port0 == ::std::u16::MAX {
                return TestResult::discard();
            } else if port1 == 0 || port1 == ::std::u16::MAX {
                return TestResult::discard();
            }

            let addr0 = net::SocketAddr::new(net::IpAddr::V4(ip0), port0);
            let addr1 = net::SocketAddr::new(net::IpAddr::V4(ip1), port1);
            TestResult::from_bool(same_addr(&addr0, &addr1) == (addr0 == addr1))
        }

        fn same_addr_ipv6(ip0: net::Ipv6Addr, ip1: net::Ipv6Addr, port0: u16, port1: u16) -> TestResult {
            if port0 == 0 || port0 == ::std::u16::MAX {
                return TestResult::discard();
            } else if port1 == 0 || port1 == ::std::u16::MAX {
                return TestResult::discard();
            }

            let addr0 = net::SocketAddr::new(net::IpAddr::V6(ip0), port0);
            let addr1 = net::SocketAddr::new(net::IpAddr::V6(ip1), port1);
            TestResult::from_bool(same_addr(&addr0, &addr1) == (addr0 == addr1))
        }

        fn same_addr_ip6_mapped_ipv4(ip: net::Ipv4Addr, port: u16) -> TestResult {
            if port == 0 || port == ::std::u16::MAX {
                return TestResult::discard();
            }

            let addr4 = net::SocketAddr::new(net::IpAddr::V4(ip), port);
            let addr6 = net::SocketAddr::new(net::IpAddr::V6(ip.to_ipv6_mapped()), port);
            TestResult::from_bool(same_addr(&addr4, &addr6))
        }

        fn same_addr_ip6_compat_ipv4(ip: net::Ipv4Addr, port: u16) -> TestResult {
            if port == 0 || port == ::std::u16::MAX {
                return TestResult::discard();
            }

            let addr4 = net::SocketAddr::new(net::IpAddr::V4(ip), port);
            let addr6 = net::SocketAddr::new(net::IpAddr::V6(ip.to_ipv6_compatible()), port);
            TestResult::from_bool(same_addr(&addr4, &addr6))
        }
    }
}
