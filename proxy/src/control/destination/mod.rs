use std::collections::HashMap;
use std::net::SocketAddr;

use futures::sync::mpsc;
use futures::{Async, Poll, Stream};
use futures_watch::{Store, Watch};
use http;
use tower_discover::{Change, Discover};
use tower_service::Service;

use dns;
use telemetry::metrics::DstLabels;
use transport::DnsNameAndPort;

pub mod background;
mod endpoint;

pub use self::endpoint::{DstLabelsWatch, Endpoint};

/// A handle to request resolutions from a `Background`.
#[derive(Clone, Debug)]
pub struct Resolver {
    request_tx: mpsc::UnboundedSender<ResolveRequest>,
}

#[derive(Debug)]
struct ResolveRequest {
    authority: DnsNameAndPort,
    update_tx: mpsc::UnboundedSender<Update>,
}

/// A `tower_discover::Discover`, given to a `tower_balance::Balance`.
#[derive(Debug)]
pub struct Resolution<B> {
    update_rx: mpsc::UnboundedReceiver<Update>,
    /// Map associating addresses with the `Store` for the watch on that
    /// service's metric labels (as provided by the Destination service).
    ///
    /// This is used to update the `Labeled` middleware on those services
    /// without requiring the service stack to be re-bound.
    metric_labels: HashMap<SocketAddr, Store<Option<DstLabels>>>,
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

/// Creates a "channel" of `Resolver` to `Background` handles.
///
/// The `Resolver` is used by a listener, the `Background` is consumed
/// on the controller thread.
pub fn new(
    dns_config: dns::Config,
    default_destination_namespace: String,
) -> (Resolver, background::Config) {
    let (request_tx, rx) = mpsc::unbounded();
    let disco = Resolver { request_tx };
    let bg = background::Config::new(rx, dns_config, default_destination_namespace);
    (disco, bg)
}

// ==== impl Resolver =====

impl Resolver {
    /// Start watching for address changes for a certain authority.
    pub fn resolve<B>(&self, authority: &DnsNameAndPort, bind: B) -> Resolution<B> {
        trace!("resolve; authority={:?}", authority);
        let (update_tx, update_rx) = mpsc::unbounded();
        let req = {
            let authority = authority.clone();
            ResolveRequest {
                authority,
                update_tx,
            }
        };
        self.request_tx
            .unbounded_send(req)
            .expect("unbounded can't fail");

        Resolution {
            update_rx,
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

// ===== impl Metadata =====

impl Metadata {
    fn no_metadata() -> Self {
        Metadata {
            metric_labels: None,
        }
    }
}
