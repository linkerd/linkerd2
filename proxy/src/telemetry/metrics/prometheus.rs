use std::default::Default;
use std::{fmt, u32};
use std::hash::{Hash, Hasher};
use std::iter::{self, IntoIterator, Iterator};
use std::ops;
use std::sync::Arc;

use futures::future::{self, Either, Future, FutureResult};
use futures::sync::BiLock;
use futures::Poll;
use http;
use hyper;
use hyper::header::{ContentLength, ContentType};
use hyper::StatusCode;
use hyper::server::{
    Service as HyperService,
    Request as HyperRequest,
    Response as HyperResponse
};
use indexmap::{IndexMap};
use tokio_core::reactor::Handle;

use ctx;
use telemetry::event::Event;
use super::latency::{BUCKET_BOUNDS, Histogram};

/// A set of Prometheus label pairs.
#[derive(Debug, Clone, Eq, PartialEq, Hash)]
pub struct Labels {
    inner: Arc<LabelsInner>
}

#[derive(Debug, Clone, Eq, PartialEq, Default)]
struct LabelsInner {
    labels: IndexMap<&'static str, String>,
    parent: Option<Labels>,
}

#[derive(Debug, Clone)]
struct Metrics {
    request_total: Metric<Counter>,
    response_total: Metric<Counter>,
    response_duration: Metric<Histogram>,
    response_latency: Metric<Histogram>,
}

#[derive(Debug)]
pub struct ResponseFuture {
    lock: Arc<BiLock<Metrics>>,
}

#[derive(Debug, Clone)]
struct Metric<M> {
    name: &'static str,
    help: &'static str,
    values: IndexMap<Labels, M>
}

#[derive(Copy, Debug, Default, Clone, Eq, PartialEq)]
struct Counter(u64);

/// Tracks Prometheus metrics
#[derive(Debug)]
pub struct Aggregate {
    root_labels: Labels,
    metrics: BiLock<Metrics>,
    handle: Handle,
}

/// Future which will update the metrics state.
#[derive(Debug)]
pub struct Work<'a> {
    // TODO: add batching.
    lock: &'a BiLock<Metrics>,
    labels: Labels,
    event: Event,
}

/// Serve scrapable metrics.
#[derive(Debug, Clone)]
pub struct Serve {
    metrics: Arc<BiLock<Metrics>>,
}

/// Construct the Prometheus metrics.
///
/// Returns the `Aggregate` and `Serve` sides. The `Serve` side
/// is a Hyper service which can be used to create the server for the
/// scrape endpoint, while the `Aggregate` side can receive updates to the
/// metrics by calling `record_event`.
pub fn new(process_ctx: Arc<ctx::Process>, handle: &Handle)
    -> (Aggregate, Serve)
{
    let (read, write) = BiLock::new(Metrics::new());
    (Aggregate::new(process_ctx, write, handle), Serve::new(read))
}

// ===== impl Serve =====

impl Serve {

    fn new(metrics: BiLock<Metrics>) -> Self {
        Serve { metrics: Arc::new(metrics) }
    }

    fn future(&self) -> ResponseFuture {
        ResponseFuture { lock: self.metrics.clone() }
    }
}

// ===== impl Metrics =====

impl Metrics {

    pub fn new() -> Self {
        let response_total = Metric::<Counter>::new(
            "response_total",
            "A counter of the number of responses the proxy has received.",
        );
        let request_total = Metric::<Counter>::new(
            "request_total",
            "A counter of the number of requests the proxy has received.",
        );
        let response_duration = Metric::<Histogram>::new(
            "response_duration_ms",
            "A histogram of the duration of a response. This is measured from \
             when the response headers are received to when the response \
             stream has completed.",
        );
        let response_latency = Metric::<Histogram>::new(
            "response_latency_ms",
            "A histogram of the total latency of a response. This is measured \
            from when the request headers are received to when the response \
            stream has completed.",
        );
        Metrics {
            request_total,
            response_total,
            response_duration,
            response_latency,
        }
    }

    fn request_total(&mut self, labels: &Labels) -> &mut u64 {
        &mut self.request_total.values
            .entry(labels.clone())
            .or_insert_with(Default::default).0
    }

    fn response_duration(&mut self, labels: &Labels) -> &mut Histogram {
        self.response_duration.values
            .entry(labels.clone())
            .or_insert_with(Default::default)
    }


    fn response_latency(&mut self, labels: &Labels) -> &mut Histogram {
        self.response_latency.values
            .entry(labels.clone())
            .or_insert_with(Default::default)
    }

    fn response_total(&mut self, labels: &Labels) -> &mut u64 {
        &mut self.response_total.values
            .entry(labels.clone())
            .or_insert_with(Default::default).0
    }
}


impl fmt::Display for Metrics {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}\n{}\n{}\n{}",
            self.request_total,
            self.response_total,
            self.response_duration,
            self.response_latency,
        )
    }
}

impl fmt::Display for Counter {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}", self.0 as f64)
    }
}

// ===== impl Metric =====

impl<M> Metric<M> {

    pub fn new(name: &'static str, help: &'static str) -> Self {
        Metric {
            name,
            help,
            values: IndexMap::new(),
        }
    }

}

impl fmt::Display for Metric<Counter> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f,
            "# HELP {name} {help}\n# TYPE {name} counter\n",
            name = self.name,
            help = self.help,
        )?;

        for (labels, value) in &self.values {
            if labels.is_empty() {
                write!(f, "{} {}\n", self.name, value)
            } else {
                write!(f, "{name}{{{labels}}} {value}\n",
                    name = self.name,
                    labels = labels,
                    value = value,
                )
            }?;
        }

        Ok(())
    }
}

impl fmt::Display for Metric<Histogram> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f,
            "# HELP {name} {help}\n# TYPE {name} histogram\n",
            name = self.name,
            help = self.help,
        )?;

        for (labels, histogram) in &self.values {
            for (num, count) in histogram.into_iter().enumerate() {
                write!(f, "{name}{{{labels},le=\"{le}\"}} {value}\n",
                    name = self.name,
                    labels = labels,
                    le = BUCKET_BOUNDS[num],
                    value = count,
                )?;
            }
        }

        Ok(())
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
            metrics: metrics,
            handle: handle.clone(),
        }
    }

    /// Observe the given event.
    pub fn record_event(&mut self, event: Event) -> Option<Work> {
        trace!("Metrics::record({:?})", event);
        let labels = match event {
            Event::StreamRequestOpen(ref req) =>
                Some(self.root_labels
                    .with_proxy_ctx(event.proxy())
                    .with_request_ctx(req)),
            Event::StreamResponseEnd(ref res, ref end) =>
                Some(self.root_labels
                    .with_proxy_ctx(event.proxy())
                    .with_response_ctx(res)
                    .with_grpc_status(end.grpc_status)),
            Event::StreamResponseFail(ref res, _) =>
                // TODO: do we care about the failure's error code here, or
                //       does res.status_code cover that?
                Some(self.root_labels
                    .with_proxy_ctx(event.proxy())
                    .with_response_ctx(res)),
            _ =>
                // TODO: this is where we could count transport events,
                //       if there were any metrics that cared about them.
                // TODO: track request failures?
                None,
        };
        labels.map(move |labels| Work {
            lock: &self.metrics,
            labels,
            event: event
        })

    }
}

// ===== impl Work =====

#[must_use = "futures do nothing unless polled"]
impl<'a> Future for Work<'a> {
    type Item = ();
    type Error = ();

    fn poll(&mut self) -> Poll<(), ()> {
        Ok(self.lock.poll_lock().map(|mut metrics|
            match self.event {
                Event::StreamResponseEnd(_, ref end) => {
                    *metrics.response_total(&self.labels) += 1;
                    *metrics.response_duration(&self.labels) += end.since_response_open;
                    *metrics.response_latency(&self.labels) += end.since_request_open;
                },
                Event::StreamResponseFail(_, ref fail) => {
                    *metrics.response_total(&self.labels) += 1;
                    *metrics.response_duration(&self.labels) += fail.since_response_open;
                    *metrics.response_latency(&self.labels) += fail.since_request_open;
                },
                Event::StreamRequestOpen(_) => {
                    *metrics.request_total(&self.labels) += 1;
                },
                _ => {},
            }))
    }

}

// ===== impl ResponseFuture =====

#[must_use = "futures do nothing unless polled"]
impl Future for ResponseFuture {
    type Item = HyperResponse;
    type Error = hyper::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        Ok(self.lock.poll_lock().map(|metrics| {
            let body = format!("{}", *metrics);
            HyperResponse::new()
                .with_header(ContentLength(body.len() as u64))
                .with_header(ContentType::plaintext())
                .with_body(body)
        }))
    }
}


// ===== impl Serve =====

impl HyperService for Serve {
    type Request = HyperRequest;
    type Response = HyperResponse;
    type Error = hyper::Error;
    type Future = Either<
        ResponseFuture,
        FutureResult<Self::Response, Self::Error>
    >;

    fn call(&self, req: Self::Request) -> Self::Future {
        if req.path() != "/metrics" {
            let rsp = HyperResponse::new().with_status(StatusCode::NotFound);
            return Either::B(future::ok(rsp));
        }

        Either::A(self.future())
    }
}


// ===== impl Labels =====

impl From<Arc<ctx::Process>> for Labels {
    fn from(_ctx: Arc<ctx::Process>) -> Self {
        let inner = Arc::new(LabelsInner::new());
        // TODO: when PR #448 lands, use CONDUIT_PROMETHEUS_LABELS to get the
        //       owning pod spec labels.
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

    pub fn with_request_ctx<'a>(&self, req: &'a Arc<ctx::http::Request>)
        -> Labels
    {
        let authority = req.uri
            .authority_part()
            .map(http::uri::Authority::to_string)
            .unwrap_or_else(String::new);
        let inner = self.child() + ("authority", authority);
        let inner = Arc::new(inner);
        Labels {
            inner,
        }
    }

    pub fn with_response_ctx<'a>(&self, rsp: &'a Arc<ctx::http::Response>)
        -> Labels
    {
        let status = rsp.status.as_str();
        let authority = rsp.request.uri
            .authority_part()
            .map(http::uri::Authority::to_string)
            .unwrap_or_else(String::new);
        let inner = self.child() + ("authority", authority) +
            ("status_code", status.into());
        let inner = Arc::new(inner);
        Labels {
            inner,
        }
    }


    pub fn with_grpc_status(&self, status: Option<u32>)
        -> Labels {
        let inner = if let Some(code) = status {
            Arc::new(self.child() + ("grpc_status", format!("{}", code)))
        } else {
            self.inner.clone()
        };
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

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    pub fn len(&self) -> usize {
        self.inner.len()
    }
}

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

// ===== impl LabelsInner =====

impl LabelsInner {

    fn new() -> Self {
        Default::default()
    }

    fn len(&self) -> usize {
        self.labels.len() + self.parent
            .as_ref()
            .map(Labels::len)
            .unwrap_or(0)
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
