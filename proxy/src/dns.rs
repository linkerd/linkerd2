use futures::prelude::*;
use std::fmt;
use std::net::IpAddr;
use std::time::Instant;
use tokio::timer::Delay;
use transport;
use trust_dns_resolver::{
    self,
    config::{ResolverConfig, ResolverOpts},
    error::{ResolveError, ResolveErrorKind},
    lookup_ip::LookupIp,
    AsyncResolver,
    BackgroundLookupIp,
};

use config::Config;

#[derive(Clone, Debug)]
pub struct Resolver {
    resolver: AsyncResolver,
}

pub enum IpAddrFuture {
    DNS(Box<Future<Item = LookupIp, Error = ResolveError> + Send>),
    Fixed(IpAddr),
}

pub enum Error {
    NoAddressesFound,
    ResolutionFailed(ResolveError),
}

#[derive(Debug)]
pub enum Response {
    Exists(LookupIp),
    DoesNotExist(Option<Instant>),
}

pub struct IpAddrListFuture {
    name: Name,
    state: Option<State>,
}

enum State {
    Delay { delay: Delay, lookup: BackgroundLookupIp },
    Lookup(BackgroundLookupIp),
}

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


impl Resolver {
    /// Construct a new `Resolver` from the system configuration and Conduit's
    /// environment variables.
    ///
    /// # Returns
    ///
    /// Either a tuple containing a new `Resolver` and the background task to
    /// drive that resolver's futures, or an error if the system configuration
    /// could not be parsed.
    ///
    /// TODO: Make this infallible, like it is in the `domain` crate.
    pub fn from_system_config_and_env(env_config: &Config)
        -> Result<(Self, impl Future<Item = (), Error = ()> + Send), ResolveError> {
        let (config, opts) = trust_dns_resolver::system_conf::read_system_conf()?;
        let opts = env_config.configure_resolver_opts(opts);
        trace!("DNS config: {:?}", &config);
        trace!("DNS opts: {:?}", &opts);
        Ok(Self::new(config, opts))
    }


    /// NOTE: It would be nice to be able to return a named type rather than
    ///       `impl Future` for the background future; it would be called
    ///       `Background` or `ResolverBackground` if that were possible.
    pub fn new(config: ResolverConfig,  mut opts: ResolverOpts)
        -> (Self, impl Future<Item = (), Error = ()> + Send)
    {
        // Disable Trust-DNS's caching.
        opts.cache_size = 0;
        let (resolver, background) = AsyncResolver::new(config, opts);
        let resolver = Resolver {
            resolver,
        };
        (resolver, background)
    }

    pub fn resolve_one_ip(&self, host: &transport::Host) -> IpAddrFuture {
        match *host {
            transport::Host::DnsName(ref name) => {
                trace!("resolve_one_ip {}", name);
                IpAddrFuture::DNS(Box::new(self.clone().lookup_ip(name)))
            }
            transport::Host::Ip(addr) => IpAddrFuture::Fixed(addr),
        }
    }

    pub fn resolve_all_ips(&self, deadline: Instant, host: &Name) -> IpAddrListFuture {
        let name = host.clone();
        trace!("resolve_all_ips {}", &name);
        let lookup = self.clone().lookup_ip(&name);
        let delay = Delay::new(deadline);
        IpAddrListFuture {
            name,
            state: Some(State::Delay { delay, lookup }),
        }
    }

    fn lookup_ip(self, &Name(ref name): &Name) -> BackgroundLookupIp {
        self.resolver.lookup_ip(name.as_str())
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

impl Future for IpAddrListFuture {
    type Item = Response;
    type Error = ResolveError;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        loop {
            match self.state.take().expect("state taken twice!") {
                State::Delay { mut delay, lookup } => match delay.poll() {
                    Ok(Async::Ready(())) => {
                        trace!(
                            "resolve_all_ips {}: looking up IP after delay",
                            &self.name
                        );
                        self.state = Some(State::Lookup(lookup));
                    },
                    Err(e) => {
                        warn!(
                            "resolve_all_ips {}: timer failed ({:?}), \
                             advancing to lookup anyway",
                             self.name,
                             e
                            );
                        self.state = Some(State::Lookup(lookup));
                    },
                    Ok(Async::NotReady) => {
                        self.state = Some(State::Delay { delay, lookup });
                        return Ok(Async::NotReady);
                    },
                },
                State::Lookup(ref mut lookup) => {
                    return match lookup.poll() {
                        Ok(Async::NotReady) => Ok(Async::NotReady),
                        Ok(Async::Ready(ips)) => {
                            trace!(
                                "resolve_all_ips {}: completed with {:?}",
                                &self.name, ips
                            );
                            Ok(Async::Ready(Response::Exists(ips)))
                        },
                        Err(e) => if let &ResolveErrorKind::NoRecordsFound {
                            valid_until, ..
                        } = e.kind() {
                            trace!(
                                "resolve_all_ips {}: completed with {:?}",
                                self.name, e
                            );
                            Ok(Async::Ready(Response::DoesNotExist(valid_until)))
                        } else {
                            warn!(
                                "resolve_all_ips {}: failed with: {:?}",
                                self.name, e
                            );
                            Err(e)
                        }
                    };
                }
            }
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
