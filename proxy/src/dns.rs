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
};
use tls;

use config::Config;

#[derive(Clone)]
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

pub enum Response {
    Exists(LookupIp),
    DoesNotExist { retry_after: Option<Instant> },
}

// `Box<Future>` implements `Future` so it doesn't need to be implemented manually.
pub type IpAddrListFuture = Box<Future<Item=Response, Error=ResolveError> + Send>;

/// A valid DNS name.
///
/// This is an alias of the strictly-validated `tls::DnsName` based on the
/// premise that we only need to support DNS names for which one could get a
/// valid certificate.
pub type Name = tls::DnsName;

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
        let name_clone = name.clone();
        trace!("resolve_all_ips {}", &name);
        let resolver = self.clone();
        let f = Delay::new(deadline)
            .then(move |_| {
                trace!("resolve_all_ips {} after delay", &name);
                resolver.lookup_ip(&name)
            })
            .then(move |result| {
                trace!("resolve_all_ips {}: completed with {:?}", name_clone, &result);
                match result {
                    Ok(ips) => Ok(Response::Exists(ips)),
                    Err(e) => {
                        if let &ResolveErrorKind::NoRecordsFound { valid_until, .. } = e.kind() {
                            Ok(Response::DoesNotExist { retry_after: valid_until })
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
    fn lookup_ip(self, name: &Name)
        -> impl Future<Item = LookupIp, Error = ResolveError>
    {
        self.resolver.lookup_ip(name.as_ref())
    }
}

/// Note: `AsyncResolver` does not implement `Debug`, so we must manually
///       implement this.
impl fmt::Debug for Resolver {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.debug_struct("Resolver")
            .field("resolver", &"...")
            .finish()
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
        // Make sure `dns::Name`'s validation isn't too strict. It is
        // implemented in terms of `webpki::DNSName` which has many more tests
        // at https://github.com/briansmith/webpki/blob/master/tests/dns_name_tests.rs.
        use convert::TryFrom;

        struct Case {
            input: &'static str,
            output: &'static str,
        }

        static VALID: &[Case] = &[
            // Almost all digits and dots, similar to IPv4 addresses.
            Case { input: "1.2.3.x", output: "1.2.3.x", },
            Case { input: "1.2.3.x", output: "1.2.3.x", },
            Case { input: "1.2.3.4A", output: "1.2.3.4a", },
            Case { input: "a.b.c.d", output: "a.b.c.d", },

            // Uppercase letters in labels
            Case { input: "A.b.c.d", output: "a.b.c.d", },
            Case { input: "a.mIddle.c", output: "a.middle.c", },
            Case { input: "a.b.c.D", output: "a.b.c.d", },

            // Absolute
            Case { input: "a.b.c.d.", output: "a.b.c.d.", },
        ];

        for case in VALID {
            let name = Name::try_from(case.input);
            assert_eq!(name.as_ref().map(|x| x.as_ref()), Ok(case.output));
        }

        static INVALID: &[&str] = &[
            // These are not in the "preferred name syntax" as defined by
            // https://tools.ietf.org/html/rfc1123#section-2.1. In particular
            // the last label only has digits.
            "1.2.3.4",
            "a.1.2.3",
            "1.2.x.3",
        ];

        for case in INVALID {
            assert!(Name::try_from(case).is_err());
        }
    }
}
