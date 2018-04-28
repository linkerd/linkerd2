//! Records and serves Prometheus metrics.
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
use std::{fmt, time};
use std::hash::Hash;
use std::sync::{Arc, Mutex};
use std::io::Write;

use deflate::CompressionOptions;
use deflate::write::GzEncoder;
use futures::future::{self, FutureResult};
use hyper::{self, Body, StatusCode};
use hyper::header::{AcceptEncoding, ContentEncoding, ContentType, Encoding, QualityItem};
use hyper::server::{
    Response as HyperResponse,
    Request as HyperRequest,
    Service as HyperService,
};
use indexmap::IndexMap;

use ctx;

mod counter;
mod gauge;
mod histogram;
mod labels;
mod latency;
mod record;

use self::counter::Counter;
use self::gauge::Gauge;
use self::histogram::Histogram;
use self::labels::{
    RequestLabels,
    ResponseLabels,
    TransportLabels,
    TransportCloseLabels
};
pub use self::labels::DstLabels;
pub use self::record::Record;

#[derive(Debug, Clone)]
struct Metrics {
    request_total: Metric<Counter, Arc<RequestLabels>>,

    response_total: Metric<Counter, Arc<ResponseLabels>>,
    response_latency: Metric<Histogram<latency::Ms>, Arc<ResponseLabels>>,

    tcp: TcpMetrics,

    start_time: u64,
}

#[derive(Debug, Clone)]
struct TcpMetrics {
    open_total: Metric<Counter, Arc<TransportLabels>>,
    close_total: Metric<Counter, Arc<TransportCloseLabels>>,

    connection_duration: Metric<Histogram<latency::Ms>, Arc<TransportCloseLabels>>,
    open_connections: Metric<Gauge, Arc<TransportLabels>>,

    write_bytes_total: Metric<Counter, Arc<TransportLabels>>,
    read_bytes_total: Metric<Counter, Arc<TransportLabels>>,
}

#[derive(Debug, Clone)]
struct Metric<M, L: Hash + Eq> {
    name: &'static str,
    help: &'static str,
    values: IndexMap<L, M>
}

/// Serve Prometheues metrics.
#[derive(Debug, Clone)]
pub struct Serve {
    metrics: Arc<Mutex<Metrics>>,
}

/// Construct the Prometheus metrics.
///
/// Returns the `Record` and `Serve` sides. The `Serve` side
/// is a Hyper service which can be used to create the server for the
/// scrape endpoint, while the `Record` side can receive updates to the
/// metrics by calling `record_event`.
pub fn new(process: &Arc<ctx::Process>) -> (Record, Serve){
    let metrics = Arc::new(Mutex::new(Metrics::new(process)));
    (Record::new(&metrics), Serve::new(&metrics))
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

        let response_latency = Metric::<Histogram<latency::Ms>, Arc<ResponseLabels>>::new(
            "response_latency_ms",
            "A histogram of the total latency of a response. This is measured \
            from when the request headers are received to when the response \
            stream has completed.",
        );

        Metrics {
            request_total,
            response_total,
            response_latency,
            tcp: TcpMetrics::new(),
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
                        -> &mut Histogram<latency::Ms> {
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

    fn tcp(&mut self) -> &mut TcpMetrics {
        &mut self.tcp
    }
}

impl fmt::Display for Metrics {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        writeln!(f, "{}", self.request_total)?;
        writeln!(f, "{}", self.response_total)?;
        writeln!(f, "{}", self.response_latency)?;
        writeln!(f, "{}", self.tcp)?;

        writeln!(f, "process_start_time_seconds {}", self.start_time)?;
        Ok(())
    }
}

// ===== impl TcpMetrics =====

impl TcpMetrics {
    pub fn new() -> TcpMetrics {
        let open_total = Metric::<Counter, Arc<TransportLabels>>::new(
            "tcp_open_total",
            "A counter of the total number of transport connections.",
        );

        let close_total = Metric::<Counter, Arc<TransportCloseLabels>>::new(
            "tcp_close_total",
            "A counter of the total number of transport connections.",
        );

        let connection_duration = Metric::<Histogram<latency::Ms>, Arc<TransportCloseLabels>>::new(
            "tcp_connection_duration_ms",
            "A histogram of the duration of the lifetime of connections, in milliseconds",
        );

        let open_connections = Metric::<Gauge, Arc<TransportLabels>>::new(
            "tcp_open_connections",
            "A gauge of the number of transport connections currently open.",
        );

        let read_bytes_total = Metric::<Counter, Arc<TransportLabels>>::new(
            "tcp_read_bytes_total",
            "A counter of the total number of recieved bytes."
        );

        let write_bytes_total = Metric::<Counter, Arc<TransportLabels>>::new(
            "tcp_write_bytes_total",
            "A counter of the total number of sent bytes."
        );

         Self {
            open_total,
            close_total,
            connection_duration,
            open_connections,
            read_bytes_total,
            write_bytes_total,
        }
    }

    fn open_total(&mut self, labels: &Arc<TransportLabels>) -> &mut Counter {
        self.open_total.values
            .entry(labels.clone())
            .or_insert_with(Default::default)
    }

    fn close_total(&mut self, labels: &Arc<TransportCloseLabels>) -> &mut Counter {
        self.close_total.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }

    fn connection_duration(&mut self, labels: &Arc<TransportCloseLabels>) -> &mut Histogram<latency::Ms> {
        self.connection_duration.values
            .entry(labels.clone())
            .or_insert_with(Histogram::default)
    }

    fn open_connections(&mut self, labels: &Arc<TransportLabels>) -> &mut Gauge {
        self.open_connections.values
            .entry(labels.clone())
            .or_insert_with(Gauge::default)
    }

    fn write_bytes_total(&mut self, labels: &Arc<TransportLabels>) -> &mut Counter {
        self.write_bytes_total.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }

    fn read_bytes_total(&mut self, labels: &Arc<TransportLabels>) -> &mut Counter {
        self.read_bytes_total.values
            .entry(labels.clone())
            .or_insert_with(Counter::default)
    }
}

impl fmt::Display for TcpMetrics {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        writeln!(f, "{}", self.open_total)?;
        writeln!(f, "{}", self.close_total)?;
        writeln!(f, "{}", self.connection_duration)?;
        writeln!(f, "{}", self.open_connections)?;
        writeln!(f, "{}", self.write_bytes_total)?;
        writeln!(f, "{}", self.read_bytes_total)?;

        Ok(())
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

impl<L, V> fmt::Display for Metric<Histogram<V>, L> where
    L: fmt::Display,
    L: Hash + Eq,
    V: Into<u64>,
{
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f,
            "# HELP {name} {help}\n# TYPE {name} histogram\n",
            name = self.name,
            help = self.help,
        )?;

        for (labels, histogram) in &self.values {
            // Since Prometheus expects each bucket's value to be the sum of the number of
            // values in this bucket and all lower buckets, track the total count here.
            let mut total_count = 0u64;
            for (le, count) in histogram.into_iter() {
                // Add this bucket's count to the total count.
                let c: u64 = (*count).into();
                total_count += c;
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
                sum = histogram.sum(),
            )?;
        }

        Ok(())
    }
}

// ===== impl Serve =====

impl Serve {
    fn new(metrics: &Arc<Mutex<Metrics>>) -> Self {
        Serve {
            metrics: metrics.clone(),
        }
    }
}

fn is_gzip(req: &HyperRequest) -> bool {
    if let Some(accept_encodings) = req
        .headers()
        .get::<AcceptEncoding>()
    {
        return accept_encodings
            .iter()
            .any(|&QualityItem { ref item, .. }| item == &Encoding::Gzip)
    }
    false
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

        let metrics = self.metrics.lock()
            .expect("metrics lock poisoned");

        let resp = if is_gzip(&req) {
            trace!("gzipping metrics");
            let mut writer = GzEncoder::new(Vec::<u8>::new(), CompressionOptions::fast());
            write!(&mut writer, "{}", *metrics)
                .and_then(|_| writer.finish())
                .map(|body| {
                    HyperResponse::new()
                        .with_header(ContentEncoding(vec![Encoding::Gzip]))
                        .with_header(ContentType::plaintext())
                        .with_body(Body::from(body))
                })
        } else {
            let mut writer = Vec::<u8>::new();
            write!(&mut writer, "{}", *metrics)
                .map(|_| {
                    HyperResponse::new()
                        .with_header(ContentType::plaintext())
                        .with_body(Body::from(writer))
                })
        };

        future::result(resp.map_err(hyper::Error::Io))
    }
}
