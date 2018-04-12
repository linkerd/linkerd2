use abstract_ns;
use abstract_ns::HostResolve;
pub use abstract_ns::IpList;
use domain;
use futures::prelude::*;
use ns_dns_tokio;
use std::fmt;
use std::net::IpAddr;
use std::path::Path;
use std::str::FromStr;
use std::time::Duration;
use tokio_core::reactor::{Handle, Timeout};
use transport;

#[derive(Clone, Debug)]
pub struct Config(domain::resolv::ResolvConf);

#[derive(Clone, Debug)]
pub struct Resolver {
    resolver: ns_dns_tokio::DnsResolver,
    executor: Handle,
}

pub enum IpAddrFuture {
    DNS(ns_dns_tokio::HostFuture),
    Fixed(IpAddr),
}

pub enum Error {
    NoAddressesFound,
    ResolutionFailed(<ns_dns_tokio::HostFuture as Future>::Error),
}

pub enum Response {
    Exists(IpList),
    DoesNotExist,
}

// `Box<Future>` implements `Future` so it doesn't need to be implemented manually.
pub type IpAddrListFuture = Box<Future<Item=Response, Error=abstract_ns::Error>>;

/// A DNS name.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct Name(abstract_ns::Name);

impl fmt::Display for Name {
    fn fmt(&self, f: &mut fmt::Formatter) -> Result<(), fmt::Error> {
        self.0.fmt(f)
    }
}

impl Name {
    /// Parses the input string as a DNS name, normalizing it to lowercase.
    pub fn normalize(s: &str) -> Result<Self, ()> {
        // XXX: `abstract_ns::Name::from_str()` wrongly accepts IP addresses as
        // domain names. Protect against this. TODO: Fix abstract_ns.
        if let Ok(_) = IpAddr::from_str(s) {
            return Err(());
        }
        // XXX: `abstract_ns::Name::from_str()` doesn't accept uppercase letters.
        //  TODO: Avoid this extra allocation.
        let s = s.to_ascii_lowercase();
        abstract_ns::Name::from_str(&s)
            .map(Name)
            .map_err(|_| ())
    }
}

impl AsRef<str> for Name {
    fn as_ref(&self) -> &str {
        self.0.as_ref()
    }
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
        Resolver {
            resolver: ns_dns_tokio::DnsResolver::new_from_resolver(
                domain::resolv::Resolver::from_conf(executor, config.0)),
            executor: executor.clone()
        }
    }

    pub fn resolve_one_ip(&self, host: &transport::Host) -> IpAddrFuture {
        match *host {
            transport::Host::DnsName(ref name) => {
                trace!("resolve_one_ip {}", name);
                IpAddrFuture::DNS(self.resolver.resolve_host(&name.0))
            }
            transport::Host::Ip(addr) => IpAddrFuture::Fixed(addr),
        }
    }

    pub fn resolve_all_ips(&self, delay: Duration, host: &Name) -> IpAddrListFuture {
        let name = host.0.clone();
        let name_clone = name.clone();
        trace!("resolve_all_ips {}", &name);
        let resolver = self.resolver.clone();
        let f = Timeout::new(delay, &self.executor)
            .expect("Timeout::new() won't fail")
            .then(move |_| {
                trace!("resolve_all_ips {} after delay", &name);
                resolver.resolve_host(&name)
            })
            .then(move |result| {
                trace!("resolve_all_ips {}: completed with {:?}", name_clone, &result);
                match result {
                    Ok(ips) => Ok(Response::Exists(ips)),
                    Err(abstract_ns::Error::NameNotFound) => Ok(Response::DoesNotExist),
                    Err(e) => Err(e),
                }
            });
        Box::new(f)
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
        }
    }
}

#[cfg(test)]
mod tests {
    use super::Name;

    #[test]
    fn test_dns_name_parsing() {
        struct Case {
            input: &'static str,
            output: &'static str,
        }

        static VALID: &[Case] = &[
            // Almost all digits and dots, similar to IPv4 addresses.
            Case { input: "1.2.3.x", output: "1.2.3.x", },
            Case { input: "1.2.3.x", output: "1.2.3.x", },
            Case { input: "1.2.3.4A", output: "1.2.3.4a", },
            Case { input: "a.1.2.3", output: "a.1.2.3", },
            Case { input: "1.2.x.3", output: "1.2.x.3", },
            Case { input: "a.b.c.d", output: "a.b.c.d", },

            // Uppercase letters in labels
            Case { input: "A.b.c.d", output: "a.b.c.d", },
            Case { input: "a.mIddle.c", output: "a.middle.c", },
            Case { input: "a.b.c.D", output: "a.b.c.d", },
        ];

        for case in VALID {
            let name = Name::normalize(case.input).expect("is a valid DNS name");
            assert_eq!(name.as_ref(), case.output);
        }

        static INVALID: &[&str] = &[
            "",
            "1.2.3.4",
            "::1",
            "[::1]",
            ":1234",
            "1.2.3.4:11234",
            "abc.com:1234",
        ];

        for case in INVALID {
            assert!(Name::normalize(case).is_err(),
                    "{} is invalid", case);
        }
    }
}
