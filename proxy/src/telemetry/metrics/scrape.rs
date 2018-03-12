use std::default::Default;
use std::fmt;
use std::hash::{Hash, Hasher};
use std::iter::{self, IntoIterator, Iterator};
use std::sync::{self, Arc, RwLock};

use futures::future::{self, Either, Future, FutureResult};
use futures::sync::{BiLock, BiLockGuard};
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
use tokio_core::reactor::Handle;

use conduit_proxy_prometheus_export as prometheus;
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
    request_total: u64,
}

#[derive(Debug, Clone, Default)]
struct Metrics(IndexMap<Labels, Stats>);

#[derive(Debug, Clone)]
struct MetricsSnapshot {
    request_total: prometheus::MetricFamily,
}

/// Tracks Prometheus metrics
#[derive(Debug)]
pub struct Aggregate {
    root_labels: Labels,
    metrics: Option<BiLock<Metrics>>,
    handle: Handle,

}

/// Serve scrapable metrics.
#[derive(Debug, Clone)]
pub struct Serve {
    metrics: Arc<BiLock<Metrics>>,
}

pub fn new(process_ctx: Arc<ctx::Process>, handle: &Handle)
          -> (Aggregate, Serve)
{
    let (read, write) = BiLock::new(Metrics::default());
    (Aggregate::new(process_ctx, write, handle), Serve::new(read))
}

impl Serve {

    fn new(metrics: BiLock<Metrics>) -> Self {
        Serve { metrics: Arc::new(metrics) }
    }

    fn future(&self) -> MetricsFuture {
        MetricsFuture { lock: self.metrics.clone() }
    }
}

// ===== impl Metrics =====

impl Metrics {
    fn export(&self) -> MetricsSnapshot {
        let mut snap = MetricsSnapshot::new();

        for (labels, stats) in &self.0 {
            let labels: Vec<prometheus::LabelPair> = labels.into();
            snap.push_request_total(labels.clone(), stats.request_total);
        }

        snap
    }
}

// ===== impl MetricsFamilies =====

impl MetricsSnapshot {

    fn push_request_total(&mut self,
                          label: Vec<prometheus::LabelPair>,
                          value: u64)
                          -> &mut Self
    {
        self.request_total.metric.push(
            prometheus::Metric {
                label,
                counter: Some(prometheus::Counter {
                    value: Some(value as f64)
                }),
                ..Default::default()
            }
        );
        self
    }

    fn new() -> Self {
        let request_total = prometheus::MetricFamily {
            name: Some("request_total".into()),
            help: Some("A counter of the number of requests the proxy has \
                        received.".into()),
            type_: Some(prometheus::MetricType::Counter.into()),
            metric: Vec::new(),
        };

        MetricsSnapshot {
            request_total,
        }
    }
}

impl fmt::Display for MetricsSnapshot {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}", self.request_total)
    }
}

// ===== impl Aggregate =====

impl Aggregate {
    fn new(process_ctx: Arc<ctx::Process>,
            metrics: BiLock<Metrics>,
            handle: &Handle,)
               -> Self
    {
        Aggregate {
            root_labels: Labels::from(process_ctx),
            metrics: Some(metrics),
            handle: handle.clone(),
        }
    }

    /// Observe the given event.
    pub fn record_event(&mut self, event: Event) {
        trace!("Metrics::record({:?})", event);
        let proxy_labels = self.root_labels.with_proxy_ctx(event.proxy());
        let metrics = self.metrics.take();
        let work = metrics.unwrap().lock().map(move |mut metrics| {
            match event {
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
            self.metrics = Some(metrics.unlock());
        });
        work.wait().expect("update metrics");
    }
}

#[derive(Debug)]
struct MetricsFuture {
    lock: Arc<BiLock<Metrics>>,
}

impl Future for MetricsFuture {
    type Item = MetricsSnapshot;
    type Error = hyper::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        // TODO: can all readers share the lock?
        Ok(self.lock.poll_lock().map(|metrics| metrics.export()))
    }
}


// ===== impl Serve =====

impl HyperService for Serve {
    type Request = HyperRequest;
    type Response = HyperResponse;
    type Error = hyper::Error;
    type Future = Either<
        Box<Future<Item=Self::Response, Error=Self::Error>>,
        FutureResult<Self::Response, Self::Error>
    >;

    fn call(&self, req: Self::Request) -> Self::Future {
        if req.path() != "/metrics" {
            let rsp = HyperResponse::new().with_status(StatusCode::NotFound);
            return Either::B(future::ok(rsp));
        }
        let rsp = self.future().map(|metrics| {
            HyperResponse::new()
                .with_status(StatusCode::Ok)
                .with_body(format!("{}", metrics))
        });
        Either::A(Box::new(rsp))
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

impl<'a> Into<Vec<prometheus::LabelPair>> for &'a Labels {
    fn into(self) -> Vec<prometheus::LabelPair> {
        self.into_iter().map(|(name, value)| prometheus::LabelPair {
            name: Some(name.into()),
            value: Some(value.into())
        })
        .collect()
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

