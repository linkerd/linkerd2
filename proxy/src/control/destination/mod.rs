//! A client for the controller's Destination service.
//!
//! This client is split into two primary components: A `Resolver`, that routers use to
//! initiate service discovery for a given name, and a `background::Process` that
//! satisfies these resolution requests. These components are separated by a channel so
//! that the thread responsible for proxying data need not also do this administrative
//! work of communicating with the control plane.
//!
//! The number of active resolutions is not currently bounded by this module. Instead, we
//! trust that callers of `Resolver` enforce such a constraint (for example, via
//! `conduit_proxy_router`'s LRU cache). Additionally, users of this module must ensure
//! they consume resolutions as they are sent so that the response channels don't grow
//! without bounds.
//!
//! Furthermore, there are not currently any bounds on the number of endpoints that may be
//! returned for a single resolution. It is expected that the Destination service enforce
//! some reasonable upper bounds.
//!
//! ## TODO
//!
//! - Given that the underlying gRPC client has some max number of concurrent streams, we
//!   actually do have an upper bound on concurrent resolutions. This needs to be made
//!   more explicit.
//! - We need some means to limit the number of endpoints that can be returned for a
//!   single resolution so that `control::Cache` is not effectively unbounded.

use std::net::SocketAddr;
use std::sync::{Arc, Weak};
use tls;

use futures::{
    sync::mpsc,
    Future,
    Async,
    Poll,
    Stream
};
use http;
use tower_discover::{Change, Discover};
use tower_service::Service;

use dns;
use telemetry::metrics::DstLabels;
use transport::{DnsNameAndPort, HostAndPort};

pub mod background;
mod endpoint;

pub use self::endpoint::Endpoint;
use config::Namespaces;
use conditional::Conditional;

/// A handle to request resolutions from the background discovery task.
#[derive(Clone, Debug)]
pub struct Resolver {
    request_tx: mpsc::UnboundedSender<ResolveRequest>,
}

/// Requests that resolution updaes for `authority` be sent on `responder`.
#[derive(Debug)]
struct ResolveRequest {
    authority: DnsNameAndPort,
    responder: Responder,
}

/// A handle through which response updates may be sent.
#[derive(Debug)]
struct Responder {
    /// Sends updates from the controller to a `Resolution`.
    update_tx: mpsc::UnboundedSender<Update>,

    /// Indicates whether the corresponding `Resolution` is still active.
    active: Weak<()>,
}

/// A `tower_discover::Discover`, given to a `tower_balance::Balance`.
#[derive(Debug)]
pub struct Resolution<B> {
    /// Receives updates from the controller.
    update_rx: mpsc::UnboundedReceiver<Update>,

    /// Allows `Responder` to detect when its `Resolution` has been lost.
    ///
    /// `Responder` holds a weak reference to this `Arc` and can determine when this
    /// reference has been dropped.
    _active: Arc<()>,

    /// Binds an update endpoint to a Service.
    bind: B,
}

/// Metadata describing an endpoint.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Metadata {
    /// A set of Prometheus metric labels describing the destination.
    dst_labels: Option<DstLabels>,

    /// How to verify TLS for the endpoint.
    tls_identity: Conditional<tls::Identity, tls::ReasonForNoIdentity>,
}


#[derive(Debug, Clone)]
enum Update {
    /// Indicates that an endpoint should be bound to `SocketAddr` with the
    /// provided `Metadata`.
    ///
    /// If there was already an endpoint in the load balancer for this
    /// address, it should be replaced with the new one.
    Bind(SocketAddr, Metadata),
    /// Indicates that the endpoint for this `SocketAddr` should be removed.
    Remove(SocketAddr),
}

/// Bind a `SocketAddr` with a protocol.
pub trait Bind {
    /// The type of endpoint upon which a `Service` is bound.
    type Endpoint;

    /// Requests handled by the discovered services
    type Request;

    /// Responses given by the discovered services
    type Response;

    /// Errors produced by the discovered services
    type Error;

    type BindError;

    /// The discovered `Service` instance.
    type Service: Service<Request = Self::Request, Response = Self::Response, Error = Self::Error>;

    /// Bind a service from an endpoint.
    fn bind(&self, addr: &Self::Endpoint) -> Result<Self::Service, Self::BindError>;
}

/// Returns a `Resolver` and a background task future.
///
/// The `Resolver` is used by a listener to request resolutions, while
/// the background future is executed on the controller thread's executor
/// to drive the background task.
pub fn new(
    dns_resolver: dns::Resolver,
    namespaces: Namespaces,
    host_and_port: Option<HostAndPort>,
    controller_tls: tls::ConditionalConnectionConfig<tls::ClientConfigWatch>,
) -> (Resolver, impl Future<Item = (), Error = ()>) {
    let (request_tx, rx) = mpsc::unbounded();
    let disco = Resolver { request_tx };
    let bg = background::task(
        rx,
        dns_resolver,
        namespaces,
        host_and_port,
        controller_tls,
    );
    (disco, bg)
}

// ==== impl Resolver =====

impl Resolver {
    /// Start watching for address changes for a certain authority.
    pub fn resolve<B>(&self, authority: &DnsNameAndPort, bind: B) -> Resolution<B> {
        trace!("resolve; authority={:?}", authority);
        let (update_tx, update_rx) = mpsc::unbounded();
        let active = Arc::new(());
        let req = {
            let authority = authority.clone();
            ResolveRequest {
                authority,
                responder: Responder {
                    update_tx,
                    active: Arc::downgrade(&active),
                },
            }
        };
        self.request_tx
            .unbounded_send(req)
            .expect("unbounded can't fail");

        Resolution {
            update_rx,
            _active: active,
            bind,
        }
    }
}

// ==== impl Resolution =====

impl<B, A> Discover for Resolution<B>
where
    B: Bind<Endpoint = Endpoint, Request = http::Request<A>>,
{
    type Key = SocketAddr;
    type Request = B::Request;
    type Response = B::Response;
    type Error = B::Error;
    type Service = B::Service;
    type DiscoverError = ();

    fn poll(&mut self) -> Poll<Change<Self::Key, Self::Service>, Self::DiscoverError> {
        loop {
            let up = self.update_rx.poll();
            trace!("watch: {:?}", up);
            let update = try_ready!(up).expect("destination stream must be infinite");

            match update {
                Update::Bind(addr, meta) => {
                    // We expect the load balancer to handle duplicate inserts
                    // by replacing the old endpoint with the new one, so
                    // insertions of new endpoints and metadata changes for
                    // existing ones can be handled in the same way.
                    let endpoint = Endpoint::new(addr, meta);

                    let service = self.bind.bind(&endpoint).map_err(|_| ())?;

                    return Ok(Async::Ready(Change::Insert(addr, service)));
                },
                Update::Remove(addr) => {
                    return Ok(Async::Ready(Change::Remove(addr)));
                },
            }
        }
    }
}

// ===== impl Responder =====

impl Responder {
    fn is_active(&self) -> bool {
        self.active.upgrade().is_some()
    }
}

// ===== impl Metadata =====

impl Metadata {
    /// Construct a Metadata struct representing an endpoint with no metadata.
    pub fn no_metadata() -> Self {
        Metadata {
            dst_labels: None,
            // If we have no metadata on an endpoint, assume it does not support TLS.
            tls_identity:
                Conditional::None(tls::ReasonForNoIdentity::NotProvidedByServiceDiscovery),
        }
    }

    pub fn new(
        dst_labels: Option<DstLabels>,
        tls_identity: Conditional<tls::Identity, tls::ReasonForNoIdentity>
    ) -> Self {
        Metadata {
            dst_labels,
            tls_identity,
        }
    }

    /// Returns the endpoint's labels from the destination service, if it has them.
    pub fn dst_labels(&self) -> Option<&DstLabels> {
        self.dst_labels.as_ref()
    }

    pub fn tls_identity(&self) -> Conditional<&tls::Identity, tls::ReasonForNoIdentity> {
        self.tls_identity.as_ref()
    }
}
