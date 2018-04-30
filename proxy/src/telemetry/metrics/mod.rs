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
use std::fmt::{self, Display};
use std::hash::Hash;
use std::sync::{Arc, Mutex};
use std::time;

use indexmap::IndexMap;

use ctx;

mod counter;
mod gauge;
mod histogram;
mod labels;
mod latency;
mod record;
mod serve;

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
pub use self::serve::Serve;

/// Writes a metric in prometheus-formatted output.
///
/// This trait is implemented by `Counter`, `Gauge`, and `Histogram` to account for the
/// differences in formatting each type of metric. Specifically, `Histogram` formats a
/// counter for each bucket, as well as a count and total sum.
trait FmtMetric {
    /// Writes a metric with the given name and no labels.
    fn fmt_metric<N: Display>(&self, f: &mut fmt::Formatter, name: N) -> fmt::Result;

    /// Writes a metric with the given name and labels.
    fn fmt_metric_labeled<N, L>(&self, f: &mut fmt::Formatter, name: N, labels: L) -> fmt::Result
    where
        N: Display,
        L: Display;
}

#[derive(Debug, Clone)]
struct Metrics {
    request_total: Metric<Counter, RequestLabels>,

    response_total: Metric<Counter, ResponseLabels>,
    response_latency: Metric<Histogram<latency::Ms>, ResponseLabels>,

    tcp: TcpMetrics,

    start_time: u64,
}

#[derive(Debug, Clone)]
struct TcpMetrics {
    open_total: Metric<Counter, TransportLabels>,
    close_total: Metric<Counter, TransportCloseLabels>,

    connection_duration: Metric<Histogram<latency::Ms>, TransportCloseLabels>,
    open_connections: Metric<Gauge, TransportLabels>,

    write_bytes_total: Metric<Counter, TransportLabels>,
    read_bytes_total: Metric<Counter, TransportLabels>,
}

#[derive(Debug, Clone)]
struct Metric<M, L: Hash + Eq> {
    name: &'static str,
    help: &'static str,
    values: IndexMap<L, M>
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

        let request_total = Metric::<Counter, RequestLabels>::new(
            "request_total",
            "A counter of the number of requests the proxy has received.",
        );

        let response_total = Metric::<Counter, ResponseLabels>::new(
            "response_total",
            "A counter of the number of responses the proxy has received.",
        );

        let response_latency = Metric::<Histogram<latency::Ms>, ResponseLabels>::new(
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
                     labels: RequestLabels)
                     -> &mut Counter {
        self.request_total.values
            .entry(labels)
            .or_insert_with(Counter::default)
    }

    fn response_latency(&mut self,
                        labels: ResponseLabels)
                        -> &mut Histogram<latency::Ms> {
        self.response_latency.values
            .entry(labels)
            .or_insert_with(Histogram::default)
    }

    fn response_total(&mut self,
                      labels: ResponseLabels)
                      -> &mut Counter {
        self.response_total.values
            .entry(labels)
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
        let open_total = Metric::<Counter, TransportLabels>::new(
            "tcp_open_total",
            "A counter of the total number of transport connections.",
        );

        let close_total = Metric::<Counter, TransportCloseLabels>::new(
            "tcp_close_total",
            "A counter of the total number of transport connections.",
        );

        let connection_duration = Metric::<Histogram<latency::Ms>, TransportCloseLabels>::new(
            "tcp_connection_duration_ms",
            "A histogram of the duration of the lifetime of connections, in milliseconds",
        );

        let open_connections = Metric::<Gauge, TransportLabels>::new(
            "tcp_open_connections",
            "A gauge of the number of transport connections currently open.",
        );

        let read_bytes_total = Metric::<Counter, TransportLabels>::new(
            "tcp_read_bytes_total",
            "A counter of the total number of recieved bytes."
        );

        let write_bytes_total = Metric::<Counter, TransportLabels>::new(
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

    fn open_total(&mut self, labels: TransportLabels) -> &mut Counter {
        self.open_total.values
            .entry(labels)
            .or_insert_with(Default::default)
    }

    fn close_total(&mut self, labels: TransportCloseLabels) -> &mut Counter {
        self.close_total.values
            .entry(labels)
            .or_insert_with(Counter::default)
    }

    fn connection_duration(&mut self, labels: TransportCloseLabels) -> &mut Histogram<latency::Ms> {
        self.connection_duration.values
            .entry(labels)
            .or_insert_with(Histogram::default)
    }

    fn open_connections(&mut self, labels: TransportLabels) -> &mut Gauge {
        self.open_connections.values
            .entry(labels)
            .or_insert_with(Gauge::default)
    }

    fn write_bytes_total(&mut self, labels: TransportLabels) -> &mut Counter {
        self.write_bytes_total.values
            .entry(labels)
            .or_insert_with(Counter::default)
    }

    fn read_bytes_total(&mut self, labels: TransportLabels) -> &mut Counter {
        self.read_bytes_total.values
            .entry(labels)
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
            value.fmt_metric_labeled(f, self.name, labels)?;
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
            value.fmt_metric_labeled(f, self.name, labels)?;
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
            histogram.fmt_metric_labeled(f, self.name, labels)?;
        }

        Ok(())
    }
}
