use abstract_ns;
use abstract_ns::HostResolve;
use domain;
use futures::prelude::*;
use ns_dns_tokio;
use std::net::IpAddr;
use std::path::Path;
use std::str::FromStr;
use tokio_core::reactor::Handle;
use transport;

#[derive(Clone, Debug)]
pub struct Config(domain::resolv::ResolvConf);

#[derive(Clone, Debug)]
pub struct Resolver(ns_dns_tokio::DnsResolver);

pub enum IpAddrFuture {
    DNS(ns_dns_tokio::HostFuture),
    Fixed(IpAddr),
    InvalidDNSName(String),
}

pub enum Error {
    InvalidDNSName(String),
    NoAddressesFound,
    ResolutionFailed(<ns_dns_tokio::HostFuture as Future>::Error),
}

impl Config {
    /// Note that this ignores any errors reading or parsing the resolve.conf
    /// file, just like the `domain` crate does.
    pub fn from_file(resolve_conf_path: &Path) -> Self {
        let mut resolv_conf = domain::resolv::ResolvConf::new();
        let _ = resolv_conf.parse_file(resolve_conf_path);
        resolv_conf.finalize();
        Config(resolv_conf)
    }
}

impl Resolver {
    pub fn new(config: Config, executor: &Handle) -> Self {
        Resolver(ns_dns_tokio::DnsResolver::new_from_resolver(
            domain::resolv::Resolver::from_conf(executor, config.0),
        ))
    }

    pub fn resolve_host(&self, host: &transport::Host) -> IpAddrFuture {
        match *host {
            transport::Host::DnsName(ref name) => {
                trace!("resolve {}", name);
                match abstract_ns::Name::from_str(name) {
                    Ok(name) => IpAddrFuture::DNS(self.0.resolve_host(&name)),
                    Err(_) => IpAddrFuture::InvalidDNSName(name.clone()),
                }
            }
            transport::Host::Ip(addr) => IpAddrFuture::Fixed(addr),
        }
    }
}

impl Future for IpAddrFuture {
    // TODO: Return the IpList so the user can try all of them.
    type Item = IpAddr;
    type Error = Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        match *self {
            IpAddrFuture::DNS(ref mut inner) => match inner.poll() {
                Ok(Async::NotReady) => Ok(Async::NotReady),
                Ok(Async::Ready(ips)) => ips.pick_one()
                    .map(Async::Ready)
                    .ok_or(Error::NoAddressesFound),
                Err(e) => Err(Error::ResolutionFailed(e)),
            },
            IpAddrFuture::Fixed(addr) => Ok(Async::Ready(addr)),
            IpAddrFuture::InvalidDNSName(ref name) => Err(Error::InvalidDNSName(name.clone())),
        }
    }
}
