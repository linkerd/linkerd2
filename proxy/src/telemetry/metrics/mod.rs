//! Aggregates and serves Prometheus metrics.
//!
//! # A note on label formatting
//!
//! Prometheus labels are represented as a comma-separated list of values
//! Since the Conduit proxy labels its metrics with a fixed set of labels
//! which we know in advance, we represent these labels using a number of
//! `struct`s, all of which implement `fmt::Display`. Some of the label
//! `struct`s contain other structs which represent a subset of the labels
//! which can be present on metrics in that scope. In this case, the
//! `fmt::Display` impls for those structs call the `fmt::Display` impls for
//! the structs that they own. This has the potential to complicate the
//! insertion of commas to separate label values.
//!
//! In order to ensure that commas are added correctly to separate labels,
//! we expect the `fmt::Display` implementations for label types to behave in
//! a consistent way: A label struct is *never* responsible for printing
//! leading or trailing commas before or after the label values it contains.
//! If it contains multiple labels, it *is* responsible for ensuring any
//! labels it owns are comma-separated. This way, the `fmt::Display` impl for
//! any struct that represents a subset of the labels are position-agnostic;
//! they don't need to know if there are other labels before or after them in
//! the formatted output. The owner is responsible for managing that.
//!
//! If this rule is followed consistently across all structs representing
//! labels, we can add new labels or modify the existing ones without having
//! to worry about missing commas, double commas, or trailing commas at the
//! end of the label set (all of which will make Prometheus angry).
use std::default::Default;
use std::{fmt, ops, time};
use std::hash::Hash;
use std::num::Wrapping;
use std::sync::{Arc, Mutex};

use futures::future::{self, FutureResult};
use hyper;
use hyper::header::{ContentLength, ContentType};
use hyper::StatusCode;
use hyper::server::{
    Service as HyperService,
    Request as HyperRequest,
    Response as HyperResponse
};
use indexmap::{IndexMap};

use ctx;
use telemetry::event::Event;

mod labels;
mod latency;

use self::labels::{
    RequestLabels,
    ResponseLabels,
    TransportLabels,
    TransportCloseLabels
};
use self::latency::{BUCKET_BOUNDS, Histogram};
pub use self::labels::DstLabels;

#[derive(Debug, Clone)]
struct Metrics {
    request_total: Metric<Counter, Arc<RequestLabels>>,

    response_total: Metric<Counter, Arc<ResponseLabels>>,
    response_latency: Metric<Histogram, Arc<ResponseLabels>>,

    tcp_accept_open_total: Metric<Counter, Arc<TransportLabels>>,
    tcp_accept_close_total: Metric<Counter, Arc<TransportCloseLabels>>,

    tcp_connect_open_total: Metric<Counter, Arc<TransportLabels>>,
    tcp_connect_close_total: Metric<Counter, Arc<TransportCloseLabels>>,

    tcp_connection_duration: Metric<Histogram, Arc<TransportCloseLabels>>,
    tcp_connections_open: Metric<Gauge, Arc<TransportLabels>>,

    sent_bytes: Metric<Counter, Arc<TransportCloseLabels>>,
    received_bytes: Metric<Counter, Arc<TransportCloseLabels>>,

    start_time: u64,
}

#[derive(Debug, Clone)]
struct Metric<M, L: Hash + Eq> {
    name: &'static str,
    help: &'static str,
    values: IndexMap<L, M>
}

/// A Prometheus counter is represented by a `Wrapping` unsigned 64-bit int.
///
/// Counters always explicitly wrap on overflows rather than panicking in
/// debug builds. Prometheus' [`rate()`] and [`irate()`] queries handle breaks
/// in monotonicity gracefully  (see also [`resets()`]), so wrapping is less
/// problematic than panicking in this case.
///
/// Note, however, that Prometheus represents counters using 64-bit
/// floating-point numbers. The correct semantics are to ensure the counter
/// always gets reset to zero after Prometheus reads it, before it would ever
/// overflow a 52-bit `f64` mantissa.
///
/// [`rate()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#rate()
/// [`irate()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#irate()
/// [`resets()`]: https://prometheus.io/docs/prometheus/latest/querying/functions/#resets
///
// TODO: Implement Prometheus reset semantics correctly, taking into
//       consideration that Prometheus models counters as `f64` and so
//       there are only 52 significant bits.
#[derive(Copy, Debug, Default, Clone, Eq, PartialEq)]
pub struct Counter(Wrapping<u64>);

/// A Prometheus gauge
#[derive(Copy, Debug, Default, Clone, Eq, PartialEq)]
pub struct Gauge(u64);

/// Tracks Prometheus metrics
#[derive(Debug)]
pub struct Aggregate {
    metrics: Arc<Mutex<Metrics>>,
}

/// Serve Prometheues metrics.
#[derive(Debug, Clone)]
pub struct Serve {
    metrics: Arc<Mutex<Metrics>>,
}

/// Construct the Prometheus metrics.
///
/// Returns the `Aggregate` and `Serve` sides. The `Serve` side
/// is a Hyper service which can be used to create the server for the
/// scrape endpoint, while the `Aggregate` side can receive updates to the
/// metrics by calling `record_event`.
pub fn new(process: &Arc<ctx::Process>) -> (Aggregate, Serve) {
    let metrics = Arc::new(Mutex::new(Metrics::new(process)));
    (Aggregate::new(&metrics), Serve::new(&metrics))
}

// ===== impl Metrics =====

impl Metrics {

    pub fn new(process: &Arc<ctx::Process>) -> Self {

        let start_time = process.start_time
            .duration_since(time::UNIX_EPOCH)
            .expect(
                "process start time should not be before the beginning \
                 of the Unix epoch"
            )
            .as_secs();

        let request_total = Metric::<Counter, Arc<RequestLabels>>::new(
            "request_total",
            "A counter of the number of requests the proxy has received.",
        );

        let response_total = Metric::<Counter, Arc<ResponseLabels>>::new(
            "response_total",
            "A counter of the number of responses the proxy has received.",
        );

        let response_latency = Metric::<Histogram, Arc<ResponseLabels>>::new(
            "response_latency_ms",
            "A histogram of the total latency of a response. This is measured \
            from when the request headers are received to when the response \
            stream has completed.",
        );

        let tcp_accept_open_total = Metric::<Counter, Arc<TransportLabels>>::new(
            "tcp_accept_open_total",
            "A counter of the total number of transport connections which \
             have been accepted by the proxy.",
        );

        let tcp_accept_close_total = Metric::<Counter, Arc<TransportCloseLabels>>::new(
            "tcp_accept_close_total",
            "A counter of the total number of transport connections accepted \
             by the proxy which have been closed.",
        );

        let tcp_connect_open_total = Metric::<Counter, Arc<TransportLabels>>::new(
            "tcp_connect_open_total",
            "A counter of the total number of transport connections which \
             have been opened by the proxy.",
        );

        let tcp_connect_close_total = Metric::<Counter, Arc<TransportCloseLabels>>::new(
            "tcp_connect_close_total",
            "A counter of the total number of transport connections opened \
             by the proxy which have been closed.",
        );

        let tcp_connection_duration = Metric::<Histogram, Arc<TransportCloseLabels>>::new(
            "tcp_connection_duration_ms",
            "A histogram of the duration of the lifetime of a connection, in milliseconds",
        );

        let tcp_connections_open = Metric::<Gauge, Arc<TransportLabels>>::new(
            "tcp_connections_open",
            "A gauge of the number of transport connections currently open.",
        );

        let received_bytes = Metric::<Counter, Arc<TransportCloseLabels>>::new(
            "received_bytes",
            "A counter of the total number of recieved bytes."
        );

        let sent_bytes = Metric::<Counter, Arc<TransportCloseLabels>>::new(
            "sent_bytes",
            "A counter of the total number of sent bytes."
        );

        Metrics {
            request_total,
            response_total,
            response_latency,
            tcp_accept_open_total,
            tcp_accept_close_total,
            tcp_connect_open_total,
            tcp_connect_close_total,
            tcp_connection_duration,
            tcp_connections_open,
            received_bytes,
            sent_bytes,
            start_time,
        }
    }

    fn request_total(&mut self,
                     labels: &Arc<RequestLabels>)
                     -> &mut Counter {
        self.request_total.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }

    fn response_latency(&mut self,
                        labels: &Arc<ResponseLabels>)
                        -> &mut Histogram {
        self.response_latency.values
            .entry(labels.clone())
            .or_insert_with(Histogram::default)
    }

    fn response_total(&mut self,
                      labels: &Arc<ResponseLabels>)
                      -> &mut Counter {
        self.response_total.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }

    fn tcp_accept_open_total(&mut self,
                             labels: &Arc<TransportLabels>)
                             -> &mut Counter {
        self.tcp_accept_open_total.values
            .entry(labels.clone())
            .or_insert_with(Default::default)
    }

    fn tcp_accept_close_total(&mut self,
                              labels: &Arc<TransportCloseLabels>)
                              -> &mut Counter {
        self.tcp_accept_close_total.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }

    fn tcp_connect_open_total(&mut self,
                             labels: &Arc<TransportLabels>)
                             -> &mut Counter {
        self.tcp_connect_open_total.values
            .entry(labels.clone())
            .or_insert_with(Default::default)
    }

    fn tcp_connect_close_total(&mut self,
                              labels: &Arc<TransportCloseLabels>)
                              -> &mut Counter {
        self.tcp_connect_close_total.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }

    fn tcp_connection_duration(&mut self,
                                labels: &Arc<TransportCloseLabels>)
                                -> &mut Histogram {
        self.tcp_connection_duration.values
            .entry(labels.clone())
            .or_insert_with(Histogram::default)
    }

    fn tcp_connections_open(&mut self,
                            labels: &Arc<TransportLabels>)
                            -> &mut Gauge {
        self.tcp_connections_open.values
            .entry(labels.clone())
            .or_insert_with(Gauge::default)
    }

    fn sent_bytes(&mut self,
                  labels: &Arc<TransportCloseLabels>)
                  -> &mut Counter {
        self.sent_bytes.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }

    fn received_bytes(&mut self,
                      labels: &Arc<TransportCloseLabels>)
                      -> &mut Counter {
        self.received_bytes.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }
}

impl fmt::Display for Metrics {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        writeln!(f, "{}", self.request_total)?;
        writeln!(f, "{}", self.response_total)?;
        writeln!(f, "{}", self.response_latency)?;
        writeln!(f, "{}", self.tcp_accept_open_total)?;
        writeln!(f, "{}", self.tcp_accept_close_total)?;
        writeln!(f, "{}", self.tcp_connect_open_total)?;
        writeln!(f, "{}", self.tcp_connect_close_total)?;
        writeln!(f, "{}", self.tcp_connection_duration)?;
        writeln!(f, "{}", self.tcp_connections_open)?;
        writeln!(f, "{}", self.sent_bytes)?;
        writeln!(f, "{}", self.received_bytes)?;
        writeln!(f, "process_start_time_seconds {}", self.start_time)?;
        Ok(())
    }
}


// ===== impl Counter =====

impl fmt::Display for Counter {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}", (self.0).0 as f64)
    }
}

impl Into<u64> for Counter {
    fn into(self) -> u64 {
        (self.0).0 as u64
    }
}

impl ops::Add for Counter {
    type Output = Self;
    fn add(self, Counter(rhs): Self) -> Self::Output {
        Counter(self.0 + rhs)
    }
}

impl ops::AddAssign<u64> for Counter {
    fn add_assign(&mut self, rhs: u64) {
        (*self).0 += Wrapping(rhs)
    }
}


impl Counter {

    /// Increment the counter by one.
    ///
    /// This function wraps on overflows.
    pub fn incr(&mut self) -> &mut Self {
        (*self).0 += Wrapping(1);
        self
    }
}

// ===== impl Gauge =====

impl fmt::Display for Gauge {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl Gauge {
    /// Increment the gauge by one.
    pub fn incr(&mut self) -> &mut Self {
        if let Some(new_value) = self.0.checked_add(1) {
            (*self).0 = new_value;
        } else {
            warn!("Gauge::incr() would wrap!");
        }
        self
    }

    /// Decrement the gauge by one.
    pub fn decr(&mut self) -> &mut Self {
        if let Some(new_value) = self.0.checked_sub(1) {
            (*self).0 = new_value;
        } else {
            warn!("Gauge::decr() called on a gauge with value 0");
        }
        self
    }

}

// ===== impl Metric =====

impl<M, L: Hash + Eq> Metric<M, L> {

    pub fn new(name: &'static str, help: &'static str) -> Self {
        Metric {
            name,
            help,
            values: IndexMap::new(),
        }
    }

}

impl<L> fmt::Display for Metric<Counter, L>
where
    L: fmt::Display,
    L: Hash + Eq,
{
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f,
            "# HELP {name} {help}\n# TYPE {name} counter\n",
            name = self.name,
            help = self.help,
        )?;

        for (labels, value) in &self.values {
            write!(f, "{name}{{{labels}}} {value}\n",
                name = self.name,
                labels = labels,
                value = value,
            )?;
        }

        Ok(())
    }
}

impl<L> fmt::Display for Metric<Gauge, L>
where
    L: fmt::Display,
    L: Hash + Eq,
{
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f,
            "# HELP {name} {help}\n# TYPE {name} gauge\n",
            name = self.name,
            help = self.help,
        )?;

        for (labels, value) in &self.values {
            write!(f, "{name}{{{labels}}} {value}\n",
                name = self.name,
                labels = labels,
                value = value,
            )?;
        }

        Ok(())
    }
}

impl<L> fmt::Display for Metric<Histogram, L> where
    L: fmt::Display,
    L: Hash + Eq,
{
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f,
            "# HELP {name} {help}\n# TYPE {name} histogram\n",
            name = self.name,
            help = self.help,
        )?;

        for (labels, histogram) in &self.values {
            // Look up the bucket numbers against the BUCKET_BOUNDS array
            // to turn them into upper bounds.
            let bounds_and_counts = histogram.into_iter()
                .enumerate()
                .map(|(num, count)| (BUCKET_BOUNDS[num], count));

            // Since Prometheus expects each bucket's value to be the sum of
            // the number of values in this bucket and all lower buckets,
            // track the total count here.
            let mut total_count = 0;
            for (le, count) in bounds_and_counts {
                // Add this bucket's count to the total count.
                total_count += count;
                write!(f, "{name}_bucket{{{labels},le=\"{le}\"}} {count}\n",
                    name = self.name,
                    labels = labels,
                    le = le,
                    // Print the total count *as of this iteration*.
                    count = total_count,
                )?;
            }

            // Print the total count and histogram sum stats.
            write!(f,
                "{name}_count{{{labels}}} {count}\n\
                 {name}_sum{{{labels}}} {sum}\n",
                name = self.name,
                labels = labels,
                count = total_count,
                sum = histogram.sum_in_ms(),
            )?;
        }

        Ok(())
    }
}

// ===== impl Aggregate =====

impl Aggregate {

    fn new(metrics: &Arc<Mutex<Metrics>>) -> Self {
        Aggregate {
            metrics: metrics.clone(),
        }
    }

    #[inline]
    fn update<F: Fn(&mut Metrics)>(&mut self, f: F) {
        let mut lock = self.metrics.lock()
            .expect("metrics lock poisoned");
        f(&mut *lock);
    }

    /// Observe the given event.
    pub fn record_event(&mut self, event: &Event) {
        trace!("Metrics::record({:?})", event);
        match *event {

            Event::StreamRequestOpen(_) | Event::StreamResponseOpen(_, _) => {
                // Do nothing; we'll record metrics for the request or response
                //  when the stream *finishes*.
            },

            Event::StreamRequestFail(ref req, _) => {
                let labels = Arc::new(RequestLabels::new(req));
                self.update(|metrics| {
                    *metrics.request_total(&labels).incr();
                })
            },

            Event::StreamRequestEnd(ref req, _) => {
                let labels = Arc::new(RequestLabels::new(req));
                self.update(|metrics| {
                    *metrics.request_total(&labels).incr();
                })
            },

            Event::StreamResponseEnd(ref res, ref end) => {
                let labels = Arc::new(ResponseLabels::new(
                    res,
                    end.grpc_status,
                ));
                self.update(|metrics| {
                    *metrics.response_total(&labels).incr();
                    *metrics.response_latency(&labels) += end.since_request_open;
                });
            },

            Event::StreamResponseFail(ref res, ref fail) => {
                // TODO: do we care about the failure's error code here?
                let labels = Arc::new(ResponseLabels::fail(res));
                self.update(|metrics| {
                    *metrics.response_total(&labels).incr();
                    *metrics.response_latency(&labels) += fail.since_request_open;
                });
            },

            Event::TransportOpen(ref ctx) => {
                let labels = Arc::new(TransportLabels::new(ctx));
                self.update(|metrics| {
                    match ctx.as_ref() {
                        &ctx::transport::Ctx::Server(_) => {
                            *metrics.tcp_accept_open_total(&labels).incr();
                            *metrics.tcp_connections_open(&labels).incr();
                        },
                        &ctx::transport::Ctx::Client(_) => {
                            *metrics.tcp_connect_open_total(&labels).incr();
                        },
                    }
                })
            },

            Event::TransportClose(ref ctx, ref close) => {
                let labels = Arc::new(TransportCloseLabels::new(ctx, close));
                self.update(|metrics| {
                    *metrics.tcp_connection_duration(&labels) += close.duration;
                    *metrics.sent_bytes(&labels) += close.tx_bytes as u64;
                    *metrics.received_bytes(&labels) += close.tx_bytes as u64;
                    match ctx.as_ref() {
                        &ctx::transport::Ctx::Server(_) => {
                            *metrics.tcp_accept_close_total(&labels).incr();
                            // We don't use the accessor method here like we do
                            // for all the other metrics so that we can just
                            // use the transport open labels in `labels`
                            // without having to ref-count it separately. Since
                            // we're handling a close event, we expect those
                            // labels to already be in the map from when the
                            // transport was opened.
                            *metrics.tcp_connections_open.values
                                .get_mut(&labels.transport)
                                .expect(
                                    "observed TransportClose event for a \
                                     transport that was not counted"
                                )
                                .decr();
                        },
                        &ctx::transport::Ctx::Client(_) => {
                            *metrics.tcp_connect_close_total(&labels).incr();
                        },
                    };
                })
            },
        };
    }
}


// ===== impl Serve =====

impl Serve {
    fn new(metrics: &Arc<Mutex<Metrics>>) -> Self {
        Serve { metrics: metrics.clone() }
    }
}

impl HyperService for Serve {
    type Request = HyperRequest;
    type Response = HyperResponse;
    type Error = hyper::Error;
    type Future = FutureResult<Self::Response, Self::Error>;

    fn call(&self, req: Self::Request) -> Self::Future {
        if req.path() != "/metrics" {
            return future::ok(HyperResponse::new()
                .with_status(StatusCode::NotFound));
        }

        let body = {
            let metrics = self.metrics.lock()
                .expect("metrics lock poisoned");
            format!("{}", *metrics)
        };
        future::ok(HyperResponse::new()
            .with_header(ContentLength(body.len() as u64))
            .with_header(ContentType::plaintext())
            .with_body(body))
    }
}
