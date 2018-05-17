use std::{error, fmt};
use std::net::SocketAddr;
use std::time::Duration;
use std::sync::Arc;

use http;
use futures::{Async, Poll};
use tower_service as tower;
use tower_balance::{self, choose, load, Balance};
use tower_buffer::Buffer;
use tower_discover::{Change, Discover};
use tower_in_flight_limit::InFlightLimit;
use tower_h2;
use conduit_proxy_router::Recognize;

use bind::{self, Bind, Protocol};
use control;
use control::destination::{Bind as BindTrait, Resolution};
use ctx;
use timeout::Timeout;
use transparency::h1;
use transport::{DnsNameAndPort, Host, HostAndPort};
use rng::LazyThreadRng;

type BindProtocol<B> = bind::BindProtocol<Arc<ctx::Proxy>, B>;

pub struct Outbound<B> {
    bind: Bind<Arc<ctx::Proxy>, B>,
    discovery: control::Control,
    bind_timeout: Duration,
}

const MAX_IN_FLIGHT: usize = 10_000;

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Destination {
    Hostname(DnsNameAndPort),
    ImplicitOriginalDst(SocketAddr),
}

// ===== impl Outbound =====

impl<B> Outbound<B> {
    pub fn new(bind: Bind<Arc<ctx::Proxy>, B>,
               discovery: control::Control,
               bind_timeout: Duration,)
               -> Outbound<B> {
        Self {
            bind,
            discovery,
            bind_timeout,
        }
    }
}

impl<B> Clone for Outbound<B>
where
    B: tower_h2::Body + 'static,
{
    fn clone(&self) -> Self {
        Self {
            bind: self.bind.clone(),
            discovery: self.discovery.clone(),
            bind_timeout: self.bind_timeout.clone(),
        }
    }
}

impl<B> Recognize for Outbound<B>
where
    B: tower_h2::Body + 'static,
{
    type Request = http::Request<B>;
    type Response = bind::HttpResponse;
    type Error = <Self::Service as tower::Service>::Error;
    type Key = (Destination, Protocol);
    type RouteError = bind::BufferSpawnError;
    type Service = InFlightLimit<Timeout<Buffer<Balance<
        load::WithPendingRequests<Discovery<B>>,
        choose::PowerOfTwoChoices<LazyThreadRng>
    >>>>;

    fn recognize(&self, req: &Self::Request) -> Option<Self::Key> {
        let proto = bind::Protocol::detect(req);

        // The request URI and Host: header have not yet been normalized
        // by `NormalizeUri`, as we need to know whether the request will
        // be routed by Host/authority or by SO_ORIGINAL_DST, in order to
        // determine whether the service is reusable.
        let authority = req.uri().authority_part()
            .cloned()
        // Therefore, we need to check the host header as well as the URI
        // for a valid authority, before we fall back to SO_ORIGINAL_DST.
            .or_else(|| h1::authority_from_host(req));

        // TODO: Return error when `HostAndPort::normalize()` fails.
        let mut dest = match authority.as_ref()
            .and_then(|auth| HostAndPort::normalize(auth, Some(80)).ok()) {
            Some(HostAndPort { host: Host::DnsName(dns_name), port }) =>
                Some(Destination::Hostname(DnsNameAndPort { host: dns_name, port })),
            Some(HostAndPort { host: Host::Ip(_), .. }) |
            None => None,
        };

        if dest.is_none() {
            dest = req.extensions()
                .get::<Arc<ctx::transport::Server>>()
                .and_then(|ctx| {
                    ctx.orig_dst_if_not_local()
                })
                .map(Destination::ImplicitOriginalDst)
        };

        // If there is no authority in the request URI or in the Host header,
        // and there is no original dst, then we have nothing! In that case,
        // return `None`, which results an "unrecognized" error. In practice,
        // this shouldn't ever happen, since we expect the proxy to be run on
        // Linux servers, with iptables setup, so there should always be an
        // original destination.
        let dest = dest?;

        Some((dest, proto))
    }

    /// Builds a dynamic, load balancing service.
    ///
    /// Resolves the authority in service discovery and initializes a service that buffers
    /// and load balances requests across.
    fn bind_service(
        &self,
        key: &Self::Key,
    ) -> Result<Self::Service, Self::RouteError> {
        let &(ref dest, ref protocol) = key;
        debug!("building outbound {:?} client to {:?}", protocol, dest);

        let resolve = match *dest {
            Destination::Hostname(ref authority) => {
                Discovery::NamedSvc(self.discovery.resolve(
                    authority,
                    self.bind.clone().with_protocol(protocol.clone()),
                ))
            },
            Destination::ImplicitOriginalDst(addr) => {
                Discovery::ImplicitOriginalDst(Some((addr, self.bind.clone()
                    .with_protocol(protocol.clone()))))
            }
        };

        let loaded = tower_balance::load::WithPendingRequests::new(resolve);

        let balance = tower_balance::power_of_two_choices(loaded, LazyThreadRng);

        // use the same executor as the underlying `Bind` for the `Buffer` and
        // `Timeout`.
        let handle = self.bind.executor();

        let buffer = Buffer::new(balance, handle)
            .map_err(|_| bind::BufferSpawnError::Outbound)?;

        let timeout = Timeout::new(buffer, self.bind_timeout, handle);

        Ok(InFlightLimit::new(timeout, MAX_IN_FLIGHT))

    }
}

pub enum Discovery<B> {
    NamedSvc(Resolution<BindProtocol<B>>),
    ImplicitOriginalDst(Option<(SocketAddr, BindProtocol<B>)>),
}

impl<B> Discover for Discovery<B>
where
    B: tower_h2::Body + 'static,
{
    type Key = SocketAddr;
    type Request = http::Request<B>;
    type Response = bind::HttpResponse;
    type Error = <Self::Service as tower::Service>::Error;
    type Service = bind::Service<B>;
    type DiscoverError = BindError;

    fn poll(&mut self) -> Poll<Change<Self::Key, Self::Service>, Self::DiscoverError> {
        match *self {
            Discovery::NamedSvc(ref mut w) => w.poll()
                .map_err(|_| BindError::Internal),
            Discovery::ImplicitOriginalDst(ref mut opt) => {
                // This "discovers" a single address for an external service
                // that never has another change. This can mean it floats
                // in the Balancer forever. However, when we finally add
                // circuit-breaking, this should be able to take care of itself,
                // closing down when the connection is no longer usable.
                if let Some((addr, bind)) = opt.take() {
                    let svc = bind.bind(&addr.into())
                        .map_err(|_| BindError::External{ addr })?;
                    Ok(Async::Ready(Change::Insert(addr, svc)))
                } else {
                    Ok(Async::NotReady)
                }
            }
        }
    }
}
#[derive(Copy, Clone, Debug)]
pub enum BindError {
    External { addr: SocketAddr },
    Internal,
}

impl fmt::Display for BindError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            BindError::External { addr } =>
                write!(f, "binding external service for {:?} failed", addr),
            BindError::Internal =>
                write!(f, "binding internal service failed"),
        }
    }

}

impl error::Error for BindError {
    fn description(&self) -> &str {
        match *self {
            BindError::External { .. } => "binding external service failed",
            BindError::Internal => "binding internal service failed",
        }
    }

    fn cause(&self) -> Option<&error::Error> { None }
}
