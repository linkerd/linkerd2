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

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, Weak};

use futures::{
    sync::mpsc,
    Future,
    Async,
    Poll,
    Stream
};
use futures_watch::{Store, Watch};
use http;
use tower_discover::{Change, Discover};
use tower_service::Service;

use dns;
use telemetry::metrics::DstLabels;
use transport::{DnsNameAndPort, HostAndPort};

pub mod background;
mod endpoint;

pub use self::endpoint::{DstLabelsWatch, Endpoint};

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

    /// Map associating addresses with the `Store` for the watch on that
    /// service's metric labels (as provided by the Destination service).
    ///
    /// This is used to update the `Labeled` middleware on those services
    /// without requiring the service stack to be re-bound.
    metric_labels: HashMap<SocketAddr, Store<Option<DstLabels>>>,

    /// Binds an update endpoint to a Service.
    bind: B,
}

/// .
#[derive(Clone, Debug, Hash, Eq, PartialEq)]
struct Metadata {
    /// A set of Prometheus metric labels describing the destination.
    metric_labels: Option<DstLabels>,
}

#[derive(Debug, Clone)]
enum Update {
    Insert(SocketAddr, Metadata),
    Remove(SocketAddr),
    ChangeMetadata(SocketAddr, Metadata),
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
    default_destination_namespace: String,
    host_and_port: HostAndPort,
) -> (Resolver, impl Future<Item = (), Error = ()>) {
    let (request_tx, rx) = mpsc::unbounded();
    let disco = Resolver { request_tx };
    let bg = background::task(
        rx,
        dns_resolver,
        default_destination_namespace,
        host_and_port,
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
            metric_labels: HashMap::new(),
            bind,
        }
    }
}

// ==== impl Resolution =====

impl<B> Resolution<B> {
    fn update_metadata(&mut self, addr: SocketAddr, meta: Metadata) -> Result<(), ()> {
        if let Some(store) = self.metric_labels.get_mut(&addr) {
            store
                .store(meta.metric_labels)
                .map_err(|e| {
                    error!("update_metadata: label store error: {:?}", e);
                })
                .map(|_| ())
        } else {
            // The store has already been removed, so nobody cares about
            // the metadata change. We expect that this shouldn't happen,
            // but if it does, log a warning and handle it gracefully.
            warn!(
                "update_metadata: ignoring ChangeMetadata for {:?} because the service no longer \
                 exists.",
                addr
            );
            Ok(())
        }
    }
}

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
                Update::Insert(addr, meta) => {
                    // Construct a watch for the `Labeled` middleware that will
                    // wrap the bound service, and insert the store into our map
                    // so it can be updated later.
                    let (labels_watch, labels_store) = Watch::new(meta.metric_labels);
                    self.metric_labels.insert(addr, labels_store);

                    let endpoint = Endpoint::new(addr, labels_watch.clone());

                    let service = self.bind.bind(&endpoint).map_err(|_| ())?;

                    return Ok(Async::Ready(Change::Insert(addr, service)));
                },
                Update::ChangeMetadata(addr, meta) => {
                    // Update metadata and continue polling `rx`.
                    self.update_metadata(addr, meta)?;
                },
                Update::Remove(addr) => {
                    // It's safe to drop the store handle here, even if
                    // the `Labeled` middleware using the watch handle
                    // still exists --- it will simply read the final
                    // value from the watch.
                    self.metric_labels.remove(&addr);
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
    fn no_metadata() -> Self {
        Metadata {
            metric_labels: None,
        }
    }
}
