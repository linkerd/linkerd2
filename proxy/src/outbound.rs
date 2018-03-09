use std::{error, fmt};
use std::net::SocketAddr;
use std::time::Duration;
use std::sync::Arc;

use conduit_proxy_router::{Reuse, Recognize};
use futures::{Async, Poll};
use http;
use rand;
use tower;
use tower_balance::{self, choose, load, Balance};
use tower_buffer::Buffer;
use tower_discover::{Change, Discover};
use tower_in_flight_limit::InFlightLimit;
use tower_h2;

use bind::{self, Bind, Host, Protocol};
use control::{self, discovery};
use control::discovery::Bind as BindTrait;
use ctx;
use fully_qualified_authority::FullyQualifiedAuthority;
use timeout::Timeout;
use transparency::h1;

type BindProtocol<B> = bind::BindProtocol<Arc<ctx::Proxy>, B>;

pub struct Outbound<B> {
    bind: Bind<Arc<ctx::Proxy>, B>,
    discovery: control::Control,
    default_namespace: String,
    bind_timeout: Duration,
}

const MAX_IN_FLIGHT: usize = 10_000;

// ===== impl Outbound =====

impl<B> Outbound<B> {
    pub fn new(bind: Bind<Arc<ctx::Proxy>, B>,
               discovery: control::Control,
               default_namespace: String,
               bind_timeout: Duration,)
               -> Outbound<B> {
        Self {
            bind,
            discovery,
            default_namespace,
            bind_timeout,
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Dst {
    Unbound(FullyQualifiedAuthority, Protocol),
    Bound(SocketAddr, Protocol),
}

impl<B> Recognize for Outbound<B>
where
    B: tower_h2::Body + 'static,
{
    type Request = http::Request<B>;
    type Response = bind::HttpResponse;
    type Error = <Self::Service as tower::Service>::Error;
    type Key = Dst;
    type RouteError = bind::BufferSpawnError;
    type Service = InFlightLimit<Timeout<Buffer<Balance<
        load::WithPendingRequests<Discovery<B>>,
        choose::PowerOfTwoChoices<rand::ThreadRng>
    >>>>;

    /// Determines the destination service for a given Request.
    ///
    /// Destinations are either bound or unbound. Unbound destinations must be resolved
    /// through the controller to produce a list of endpoints. Bound destinations use the
    /// client-provided destination address.
    ///
    /// For HTTP/2 requests, a service may support several :authority values. HTTP/1
    /// requests, on the other hand, may not share a service across authorities.
    ///
    /// Services may not be reused if they do not include an authority.
    fn recognize(&self, req: &Self::Request) -> Option<Reuse<Self::Key>> {
        let proto = bind::Protocol::detect(req);

        // Determine the request's `:authority` (or `Host`), either from its headers or
        // from the request's URI.
        //
        // The authority is normalized to determine whether or not it should be resolved
        // through the controller.
        let qualified_name = req.uri().authority_part()
            .cloned()
            .or_else(|| h1::authority_from_host(req))
            .map(|authority| {
                FullyQualifiedAuthority::normalize(&authority, &self.default_namespace)
            });

        // If the request has an authority that looks like something the destination
        // service might be able to resolve, treat the name as unbound. Note that this
        // will result in different keys for different original authority values, so
        // requests for `foo` and `foo.ns` won't be satisfied by the same connections.
        if let Some(qn) = qualified_name {
            if qn.use_destination_service {
                return Some(Reuse::Reusable(Dst::Unbound(qn.name, proto)));
            }
        }

        // Otherwise, send the request to the client-bound address.
        //
        // If there is no original dst, then we have nothing! In that case, we return
        // `None`, which results an "unrecognized" error. In practice, this shouldn't ever
        // happen, since we expect the proxy to be run on Linux servers, with iptables
        // setup, so there should always be an original destination.
        let bound_addr = req.extensions()
            .get::<Arc<ctx::transport::Server>>()
            .and_then(|ctx| ctx.orig_dst_if_not_local())?;

        // Connections can only be re-used if we know the authority.
        let key = match proto {
            proto@Protocol::Http1(Host::NoAuthority) => {
                Reuse::SingleUse(Dst::Bound(bound_addr, proto))
            }
            proto => Reuse::Reusable(Dst::Bound(bound_addr, proto)),
        };
        Some(key)
    }

    /// Builds a dynamic, load balancing service.
    ///
    /// Resolves the authority in service discovery and initializes a service that buffers
    /// and load balances requests across.
    ///
    /// # TODO
    ///
    /// Buffering is currently unbounded and does not apply timeouts. This must be
    /// changed.
    fn bind_service(
        &mut self,
        dst: &Self::Key,
    ) -> Result<Self::Service, Self::RouteError> {
        debug!("building outbound client to {:?}", dst);

        let resolve = match *dst {
            Dst::Unbound(ref authority, ref protocol) => {
                Discovery::LocalSvc(self.discovery.resolve(
                    authority,
                    self.bind.clone().with_protocol(protocol.clone()),
                ))
            },
            Dst::Bound(addr, ref protocol) => {
                Discovery::External(Some((addr, self.bind.clone()
                    .with_protocol(protocol.clone()))))
            }
        };

        let loaded = tower_balance::load::WithPendingRequests::new(resolve);

        let balance = tower_balance::power_of_two_choices(loaded, rand::thread_rng());

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
    LocalSvc(discovery::Watch<BindProtocol<B>>),
    External(Option<(SocketAddr, BindProtocol<B>)>),
}

impl<B> Discover for Discovery<B>
where
    B: tower_h2::Body + 'static,
{
    type Key = SocketAddr;
    type Request = http::Request<B>;
    type Response = bind::HttpResponse;
    type Error = <bind::Service<B> as tower::Service>::Error;
    type Service = bind::Service<B>;
    type DiscoverError = BindError;

    fn poll(&mut self) -> Poll<Change<Self::Key, Self::Service>, Self::DiscoverError> {
        match *self {
            Discovery::LocalSvc(ref mut w) => w.poll()
                .map_err(|_| BindError::Internal),
            Discovery::External(ref mut opt) => {
                // This "discovers" a single address for an external service
                // that never has another change. This can mean it floats
                // in the Balancer forever. However, when we finally add
                // circuit-breaking, this should be able to take care of itself,
                // closing down when the connection is no longer usable.
                if let Some((addr, bind)) = opt.take() {
                    let svc = bind.bind(&addr)
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
