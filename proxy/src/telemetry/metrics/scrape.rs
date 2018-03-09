use std::sync::Arc;
use std::sync::atomic::AtomicUsize;
use std::hash::{Hash, Hasher};
use std::iter::{self, IntoIterator, Iterator};


use futures::future::{self, FutureResult};
use hyper;
use hyper::StatusCode;
use hyper::server::{
    Service as HyperService,
    Request as HyperRequest,
    Response as HyperResponse
};
use indexmap::{self, IndexMap};

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
    request_total: AtomicUsize,
}

/// Tracks Prometheus metrics
#[derive(Debug, Clone)]
pub struct Metrics {
    metrics: IndexMap<Labels, Stats>,
}

/// Serve scrapable metrics.
#[derive(Debug, Clone)]
pub struct Server {
    metrics: Arc<Metrics>,
}

impl Server {
    pub fn new(metrics: &Arc<Metrics>) -> Self {
        Server { metrics: metrics.clone() }
    }
}

// ===== impl Metrics =====

impl Metrics {
    pub fn new() -> Self {
        Metrics {
            metrics: IndexMap::new(),
        }
    }

    /// Observe the given event.
    ///
    /// This borrows self immutably, so that individual metric fields
    /// can implement their own mutual exclusion strategy (i.e. counters
    /// can just use atomic integers).
    pub fn record_event(&self, event: &Event) {
        trace!("Metrics::record({:?})", event);
        // TODO: record the event.
    }
}

// ===== impl Server =====


impl HyperService for Server {
    type Request = HyperRequest;
    type Response = HyperResponse;
    type Error = hyper::Error;
    type Future = FutureResult<Self::Response, Self::Error>;

    fn call(& self, req: Self::Request) -> Self::Future {
        if req.path() != "/metrics" {
            let rsp = HyperResponse::new().with_status(StatusCode::NotFound);
            return future::ok(rsp);
        }

        future::ok(HyperResponse::new()
            .with_status(StatusCode::Ok)
            .with_body(""))
    }
}


// ===== impl Labels =====

use std::ops;

impl<'a> IntoIterator for &'a Labels {
    type Item = (&'a str, &'a str);
    // TODO: remove box.
    type IntoIter = Box<Iterator<Item=Self::Item> + 'a>;

    fn into_iter(self) -> Self::IntoIter {
        self.inner.into_iter()
    }
}

impl Labels {
    fn child(&self) -> Labels {
        let inner = Arc::new(LabelsInner {
            parent: Some(self.clone()),
            ..Default::default()
        });
        Labels {
            inner,
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

