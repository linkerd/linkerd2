use std::sync::Arc;

use http;

use tower;
use tower_balance::{self, choose, Balance};
use tower_buffer::Buffer;
use tower_h2;
use tower_router::Recognize;

use bind::{self, Bind, Protocol};
use control::{self, discovery};
use ctx;
use fully_qualified_authority::FullyQualifiedAuthority;

type BindProtocol<B> = bind::BindProtocol<Arc<ctx::Proxy>, B>;

type Discovery<B> = discovery::Watch<BindProtocol<B>>;

pub struct Outbound<B> {
    bind: Bind<Arc<ctx::Proxy>, B>,
    discovery: control::Control,
    default_namespace: Option<String>,
    default_zone: Option<String>,
}

// ===== impl Outbound =====

impl<B> Outbound<B> {
    pub fn new(bind: Bind<Arc<ctx::Proxy>, B>, discovery: control::Control,
               default_namespace: Option<String>, default_zone: Option<String>)
               -> Outbound<B> {
        Self {
            bind,
            discovery,
            default_namespace,
            default_zone,
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
    type Key = (FullyQualifiedAuthority, Protocol);
    type RouteError = ();
    type Service = Buffer<Balance<
        Discovery<B>,
        choose::RoundRobin, // TODO: better load balancer.
    >>;

    fn recognize(&self, req: &Self::Request) -> Option<Self::Key> {
        req.uri().authority_part().map(|authority| {
            let auth = FullyQualifiedAuthority::new(
                authority,
                self.default_namespace.as_ref().map(|s| s.as_ref()),
                self.default_zone.as_ref().map(|s| s.as_ref()));

            let proto = match req.version() {
                http::Version::HTTP_2 => Protocol::Http2,
                _ => Protocol::Http1,
            };
            (auth, proto)
        })
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
        key: &Self::Key,
    ) -> Result<Self::Service, Self::RouteError> {
        let &(ref authority, protocol) = key;
        debug!("building outbound {:?} client to {:?}", protocol, authority);

        let resolve = self.discovery.resolve(
            authority,
            self.bind.clone().with_protocol(protocol),
        );

        // TODO: move to p2c lb.
        let balance = tower_balance::round_robin(resolve);

        // Wrap with buffering. This currently is an unbounded buffer,
        // which is not ideal.
        //
        // TODO: Don't use unbounded buffering.
        Buffer::new(balance, self.bind.executor()).map_err(|_| {})
    }
}
