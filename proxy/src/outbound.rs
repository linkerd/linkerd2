use std::sync::Arc;

use tower;
use tower_balance::{self, choose, Balance};
use tower_buffer::Buffer;
use tower_h2;
use tower_router::Recognize;

use bind::Bind;
use control;
use ctx;
use fully_qualified_authority::FullyQualifiedAuthority;

type Discovery<B> = control::discovery::Watch<Bind<Arc<ctx::Proxy>, B>>;

// todo: better lb
type LoadBalance<B> = Balance<Discovery<B>, choose::RoundRobin>;

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
    B: tower_h2::Body + 'static
{
    type Request = <Buffer<LoadBalance<B>> as tower::Service>::Request;
    type Response = <Buffer<LoadBalance<B>> as tower::Service>::Response;
    type Error = <Buffer<LoadBalance<B>> as tower::Service>::Error;
    type Key = FullyQualifiedAuthority;
    type RouteError = ();
    type Service = Buffer<LoadBalance<B>>;

    fn recognize(&self, req: &Self::Request) -> Option<Self::Key> {
        req.uri().authority_part().map(|authority|
            FullyQualifiedAuthority::new(
                authority,
                self.default_namespace.as_ref().map(|s| s.as_ref()),
                self.default_zone.as_ref().map(|s| s.as_ref()))
        )
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
        authority: &FullyQualifiedAuthority,
    ) -> Result<Self::Service, Self::RouteError> {
        debug!("building outbound client to {:?}", authority);

        let resolve = self.discovery.resolve(authority, self.bind.clone());

        // TODO: move to p2c lb.
        let balance = tower_balance::round_robin(resolve);

        // Wrap with buffering. This currently is an unbounded buffer,
        // which is not ideal.
        //
        // TODO: Don't use unbounded buffering.
        Buffer::new(balance, self.bind.executor()).map_err(|_| {})
    }
}
