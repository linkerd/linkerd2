use futures::prelude::*;
use std::fmt;
use std::net::IpAddr;
use std::time::Duration;
use tokio::reactor::{Handle, Timeout};
use transport;
use trust_dns_resolver;
use trust_dns_resolver::config::{ResolverConfig, ResolverOpts};
use trust_dns_resolver::error::{ResolveError, ResolveErrorKind};
use trust_dns_resolver::ResolverFuture;
use trust_dns_resolver::lookup_ip::{LookupIp, LookupIpFuture};

#[derive(Clone, Debug)]
pub struct Config {
    config: ResolverConfig,
    opts: ResolverOpts,
}

#[derive(Clone, Debug)]
pub struct Resolver {
    config: Config,
    executor: Handle,
}

pub enum IpAddrFuture {
    DNS(LookupIpFuture),
    Fixed(IpAddr),
}

pub enum Error {
    NoAddressesFound,
    ResolutionFailed(ResolveError),
}

pub enum Response {
    Exists(LookupIp),
    DoesNotExist,
}

// `Box<Future>` implements `Future` so it doesn't need to be implemented manually.
pub type IpAddrListFuture = Box<Future<Item=Response, Error=ResolveError>>;

/// A DNS name.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct Name(String);

impl fmt::Display for Name {
    fn fmt(&self, f: &mut fmt::Formatter) -> Result<(), fmt::Error> {
        self.0.fmt(f)
    }
}

impl<'a> From<&'a str> for Name {
    fn from(s: &str) -> Self {
        // TODO: Verify the name is a valid DNS name.
        // TODO: Avoid this extra allocation.
        Name(s.to_ascii_lowercase())
    }
}

impl AsRef<str> for Name {
    fn as_ref(&self) -> &str {
        self.0.as_ref()
    }
}

impl Config {
    /// TODO: Make this infallible, like it is in the `domain` crate.
    pub fn from_system_config() -> Result<Self, ResolveError> {
        let (config, opts) = trust_dns_resolver::system_conf::read_system_conf()?;
        trace!("DNS config: {:?}", &config);
        trace!("DNS opts: {:?}", &opts);
        Ok(Config {
            config,
            opts
        })
    }
}

impl Resolver {
    pub fn new(config: Config, executor: &Handle) -> Self {
        Resolver {
            config,
            executor: executor.clone(),
        }
    }

    pub fn resolve_one_ip(&self, host: &transport::Host) -> IpAddrFuture {
        match *host {
            transport::Host::DnsName(ref name) => {
                trace!("resolve_one_ip {}", name);
                IpAddrFuture::DNS(self.clone().lookup_ip(name))
            }
            transport::Host::Ip(addr) => IpAddrFuture::Fixed(addr),
        }
    }

    pub fn resolve_all_ips(&self, delay: Duration, host: &Name) -> IpAddrListFuture {
        let name = host.clone();
        let name_clone = name.clone();
        trace!("resolve_all_ips {}", &name);
        let resolver = self.clone();
        let f = Timeout::new(delay, &resolver.executor)
            .expect("Timeout::new() won't fail")
            .then(move |_| {
                trace!("resolve_all_ips {} after delay", &name);
                resolver.lookup_ip(&name)
            })
            .then(move |result| {
                trace!("resolve_all_ips {}: completed with {:?}", name_clone, &result);
                match result {
                    Ok(ips) => Ok(Response::Exists(ips)),
                    Err(e) => {
                        if let &ResolveErrorKind::NoRecordsFound(_) = e.kind() {
                            Ok(Response::DoesNotExist)
                        } else {
                            Err(e)
                        }
                    }
                }
            });
        Box::new(f)
    }

    // `ResolverFuture` can only be used for one lookup, so we have to clone all
    // the state during each resolution.
    fn lookup_ip(self, &Name(ref name): &Name) -> LookupIpFuture {
        let resolver = ResolverFuture::new(
            self.config.config,
            self.config.opts,
            &self.executor);
        resolver.lookup_ip(name)
    }
}

impl Future for IpAddrFuture {
    type Item = IpAddr;
    type Error = Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        match *self {
            IpAddrFuture::DNS(ref mut inner) => match inner.poll() {
                Ok(Async::NotReady) => Ok(Async::NotReady),
                Ok(Async::Ready(ips)) => {
                    match ips.iter().next() {
                        Some(ip) => {
                            trace!("DNS resolution found: {:?}", ip);
                            Ok(Async::Ready(ip))
                        },
                        None => {
                            trace!("DNS resolution did not find anything");
                            Err(Error::NoAddressesFound)
                        }
                    }
                },
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
            let name = Name::from(case.input);
            assert_eq!(name.as_ref(), case.output);
        }
    }
}
