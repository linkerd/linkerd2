use std::sync::{self, Arc, RwLock};
use std::hash::{Hash, Hasher};
use std::iter::{self, IntoIterator, Iterator};
use std::fmt;

use futures::future::{self, Either, Future, FutureResult};
use futures::{Async, Poll};
use http;
use hyper;
use hyper::StatusCode;
use hyper::server::{
    Service as HyperService,
    Request as HyperRequest,
    Response as HyperResponse
};
use indexmap::{IndexMap};

use ctx;
use telemetry::event::Event;

#[derive(Debug, Clone, Eq, PartialEq, Hash)]
pub struct Labels {
    inner: Arc<LabelsInner>
}

#[derive(Debug, Clone, Eq, PartialEq, Default)]
struct LabelsInner {
    labels: IndexMap<&'static str, String>,
    parent: Option<Labels>,
}

#[derive(Debug, Clone, Default)]
pub struct Stats {

    /// `request_total` counter metric.
    request_total: usize,
}

#[derive(Debug, Clone, Default)]
struct Metrics(IndexMap<Labels, Stats>);

/// Tracks Prometheus metrics
#[derive(Debug)]
pub struct Aggregate {
    root_labels: Labels,
    metrics: Arc<RwLock<Metrics>>,
}

/// Serve scrapable metrics.
#[derive(Debug)]
pub struct Serve {
    metrics: Arc<RwLock<Metrics>>,
}

pub fn new(process_ctx: Arc<ctx::Process>) -> (Aggregate, Serve) {
    let metrics = Arc::new(RwLock::new(Metrics::default()));
    let serve = Serve::new(&metrics);
    (Aggregate::new(process_ctx, metrics), serve)
}

impl Serve {
    fn new(metrics: &Arc<RwLock<Metrics>>) -> Self {
        Serve { metrics: metrics.clone() }
    }
}


// ===== impl Aggregate =====

impl Aggregate {
    fn new(process_ctx: Arc<ctx::Process>,
               metrics: Arc<RwLock<Metrics>>)
               -> Self
    {
        Aggregate {
            root_labels: Labels::from(process_ctx),
            metrics,
        }
    }

    /// Observe the given event.
    pub fn record_event(&mut self, event: &Event) {
        trace!("Metrics::record({:?})", event);
        let proxy_labels = self.root_labels.with_proxy_ctx(event.proxy());
        let mut metrics = self.metrics.write()
            .expect("RwLock can only be poisoned by a writer panicking,\
                     and Serve tasks do not take the write lock.");
        match *event {
            Event::StreamRequestOpen(ref req) => {
                let labels = proxy_labels.with_request_ctx(req);
                metrics.0.entry(labels)
                    .or_insert_with(Default::default)
                    .request_total += 1;
            },
            _ => {
                // TODO: record other events.
            }
        };

    }
}

impl fmt::Display for Metrics {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        for (labels, stats) in self.0.iter() {
            write!(f, "request_total{{{}}} {}\n", labels, stats.request_total)?;
        }
        Ok(())
    }
}


#[derive(Debug)]
pub struct ServeFuture {
    lock: Arc<RwLock<Metrics>>,
}

impl ServeFuture {
    fn new(lock: &Arc<RwLock<Metrics>>) -> Self {
        ServeFuture {
            lock: lock.clone(),
        }
    }
}

impl Future for ServeFuture {
    type Item = HyperResponse;
    type Error = hyper::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        // TODO: can the "turn try_read into a Future" part of this be factored
        //       out into something reusable?
        match self.lock.try_read() {
            // Guard acquired!
            Ok(metrics) => {
                // TODO: handle both text and protobuf serialization.
                let rsp = HyperResponse::new()
                    .with_status(StatusCode::Ok)
                    .with_body(format!("{}", *metrics));
                Ok(Async::Ready(rsp))
            },
            // The locked object is being written to, so acquiring a read
            // lock would block. Yield until the executor polls us again.
            Err(sync::TryLockError::WouldBlock) => Ok(Async::NotReady),
            // If the lock was poisoned, that's bad news.
            Err(sync::TryLockError::Poisoned(err)) => {
                // Bad news!
                error!("metrics RwLock was poisoned: {:?}", err);
                let rsp = HyperResponse::new()
                    .with_status(StatusCode::InternalServerError);
                Ok(Async::Ready(rsp))
            },
        }
    }
}


// ===== impl Serve =====

impl HyperService for Serve {
    type Request = HyperRequest;
    type Response = HyperResponse;
    type Error = hyper::Error;
    type Future = Either<
        ServeFuture,
        FutureResult<Self::Response, Self::Error>,
    >;

    fn call(&self, req: Self::Request) -> Self::Future {
        if req.path() != "/metrics" {
            let rsp = HyperResponse::new().with_status(StatusCode::NotFound);
            return Either::B(future::ok(rsp));
        }
        Either::A(ServeFuture::new(&self.metrics))
    }
}


// ===== impl Labels =====

use std::ops;

impl fmt::Display for Labels {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        let mut labels = self.into_iter();

        if let Some((label, value)) = labels.next() {
            // format the first label pair without a comma.
            write!(f, "{}=\"{}\"", label, value)?;

            // format the remaining pairs with a comma preceeding them.
            for (label, value) in labels {
                write!(f, ",{}=\"{}\"", label, value)?;
            }
        }

        Ok(())
    }
}

impl From<Arc<ctx::Process>> for Labels {
    fn from(_ctx: Arc<ctx::Process>) -> Self {
        let inner = Arc::new(LabelsInner::new());
        // TODO: when PR #448 lands, use CONDUIT_PROMETHEUS_LABELS to get the
        // owning pod spec labels.
        Labels {
            inner,
        }
    }
}

impl<'a> IntoIterator for &'a Labels {
    type Item = (&'a str, &'a str);
    // TODO: remove box.
    type IntoIter = Box<Iterator<Item=Self::Item> + 'a>;

    fn into_iter(self) -> Self::IntoIter {
        self.inner.into_iter()
    }
}

impl Labels {
    pub fn with_proxy_ctx<'a>(&self, proxy_ctx: &'a Arc<ctx::Proxy>) -> Labels {
        let direction = if proxy_ctx.is_inbound() {
            "inbound"
        } else {
            "outbound"
        };
        let inner = self.child() + ("direction", direction.to_owned());
        let inner = Arc::new(inner);
        Labels {
            inner,
        }
    }

    pub fn with_request_ctx<'a>(&self, request_ctx: &'a Arc<ctx::http::Request>)
        -> Labels {
        let authority = request_ctx.uri
            .authority_part()
            .map(http::uri::Authority::to_string)
            .unwrap_or_else(String::new);
        let inner = self.child() + ("authority", authority);
        let inner = Arc::new(inner);
        Labels {
            inner,
        }
    }

    fn child(&self) -> LabelsInner {
        LabelsInner {
            parent: Some(self.clone()),
            ..Default::default()
        }
    }
}

// ===== impl LabelsInner =====


impl LabelsInner {

    fn new() -> Self {
        Default::default()
    }

    fn parent_iter<'a>(&'a self)
        -> Box<Iterator<Item=(&'a str, &'a str)> + 'a>
    {
        self.parent.as_ref()
            .map(|parent| {
                let iter = parent.into_iter()
                    .filter_map(move |(key, val)|
                        // skip keys contained in this scope.
                        if self.labels.contains_key(key) {
                            None
                        }
                        else {
                            Some((key, val.as_ref()))
                        });
                Box::new(iter) as Box<Iterator<Item=(&'a str, &'a str)> + 'a>
            })
            .unwrap_or_else(|| Box::new(iter::empty()))
    }

}


impl ops::Add<(&'static str, String)> for LabelsInner {
    type Output = Self;
    fn add(mut self, (label, value): (&'static str, String)) -> Self::Output {
        self.labels.insert(label, value);
        self
    }
}

impl<'a> IntoIterator for &'a LabelsInner {
    type Item = (&'a str, &'a str);
    // TODO: remove box.
    type IntoIter = Box<Iterator<Item=Self::Item> + 'a>;

    fn into_iter(self) -> Self::IntoIter {
        Box::new(self.labels.iter()
            .map(|(&key, val)| (key, val.as_ref()))
            .chain(self.parent_iter()))
    }
}

impl Hash for LabelsInner {
    fn hash<H: Hasher>(&self, state: &mut H) {
        for pair in self {
            pair.hash(state);
        }
    }
}

