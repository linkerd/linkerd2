use std::io;
use std::sync::Arc;

use http;
use tower_balance::{self, Balance};
use tower_buffer::{self, Buffer};
use tower_h2;
use tower_reconnect;
use tower_router::Recognize;

use bind::Bind;
use control;
use ctx;
use telemetry;
use transport;

type Discovery<B> = control::discovery::Watch<Bind<Arc<ctx::Proxy>, B>>;

type Error = tower_buffer::Error<
    tower_balance::Error<
        tower_reconnect::Error<
            tower_h2::client::Error,
            tower_h2::client::ConnectError<transport::TimeoutError<io::Error>>
        >,
        (),
    >
>;

pub struct Outbound<B> {
    bind: Bind<Arc<ctx::Proxy>, B>,
    discovery: control::Control,
}

// ===== impl Outbound =====

impl<B> Outbound<B> {
    pub fn new(bind: Bind<Arc<ctx::Proxy>, B>, discovery: control::Control) -> Self {
        Self {
            bind,
            discovery,
        }
    }
}

impl<B> Recognize for Outbound<B>
where
    B: tower_h2::Body + 'static
{
    type Request = http::Request<B>;
    type Response = http::Response<telemetry::sensor::http::ResponseBody<tower_h2::RecvBody>>;
    type Error = Error;
    type Key = http::uri::Authority;
    type RouteError = ();
    type Service = Buffer<Balance<Discovery<B>>>;

    fn recognize(&self, req: &Self::Request) -> Option<Self::Key> {
        req.uri().authority_part().cloned()
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
    fn bind_service(&mut self, authority: &http::uri::Authority)
        -> Result<Self::Service, Self::RouteError>
    {
        debug!("building outbound client to {:?}", authority);

        let resolve = self.discovery.resolve(authority, self.bind.clone());

        let balance = Balance::new(resolve);

        // Wrap with buffering. This currently is an unbounded buffer,
        // which is not ideal.
        //
        // TODO: Don't use unbounded buffering.
        Buffer::new(balance, self.bind.executor())
            .map_err(|_| {})
    }
}
