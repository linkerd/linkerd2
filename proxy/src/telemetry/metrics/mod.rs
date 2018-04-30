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
use std::marker::PhantomData;
use std::sync::{Arc, Mutex};
use std::time::{UNIX_EPOCH, Duration, Instant};

use indexmap::IndexMap;

use ctx;

macro_rules! metrics {
    { $( $name:ident : $kind:ty { $help:expr } ),+ } => {
        $(
            #[allow(non_upper_case_globals)]
            const $name: Metric<'static, $kind> = Metric {
                name: stringify!($name),
                help: $help,
                _p: ::std::marker::PhantomData,
            };
        )+
    }
}

mod counter;
mod gauge;
mod histogram;
mod http;
mod labels;
mod latency;
mod record;
mod serve;
mod transport;

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
    /// The metric's `TYPE` in help messages.
    const KIND: &'static str;

    /// Writes a metric with the given name and no labels.
    fn fmt_metric<N: Display>(&self, f: &mut fmt::Formatter, name: N) -> fmt::Result;

    /// Writes a metric with the given name and labels.
    fn fmt_metric_labeled<N, L>(&self, f: &mut fmt::Formatter, name: N, labels: L) -> fmt::Result
    where
        N: Display,
        L: Display;
}

/// Describes a metric statically.
///
/// Formats help messages and metric values for prometheus output.
struct Metric<'a, M: FmtMetric> {
    name: &'a str,
    help: &'a str,
    _p: PhantomData<M>,
}

/// The root scope for all runtime metrics.
#[derive(Debug, Default)]
struct Root {
    requests: http::RequestScopes,
    responses: http::ResponseScopes,
    transports: transport::OpenScopes,
    transport_closes: transport::CloseScopes,

    start_time: Gauge,
}

/// Holds an `S`-typed scope for each `L`-typed label set.
///
/// An `S` type typically holds one or more metrics.
#[derive(Debug)]
struct Scopes<L: Display + Hash + Eq, S> {
    scopes: IndexMap<L, S>,
}

#[derive(Debug)]
struct Stamped<T> {
    stamp: Instant,
    inner: T,
}

/// Construct the Prometheus metrics.
///
/// Returns the `Record` and `Serve` sides. The `Serve` side
/// is a Hyper service which can be used to create the server for the
/// scrape endpoint, while the `Record` side can receive updates to the
/// metrics by calling `record_event`.
pub fn new(process: &Arc<ctx::Process>, idle_retain: Duration) -> (Record, Serve){
    let metrics = Arc::new(Mutex::new(Root::new(process)));
    (Record::new(&metrics), Serve::new(&metrics, idle_retain))
}

// ===== impl Metric =====

impl<'a, M: FmtMetric> Metric<'a, M> {
    /// Formats help messages for this metric.
    pub fn fmt_help(&self, f: &mut fmt::Formatter) -> fmt::Result {
        writeln!(f, "# HELP {} {}", self.name, self.help)?;
        writeln!(f, "# TYPE {} {}", self.name, M::KIND)?;
        Ok(())
    }

    /// Formats a single metric without labels.
    pub fn fmt_metric(&self, f: &mut fmt::Formatter, metric: M) -> fmt::Result {
        metric.fmt_metric(f, self.name)
    }

    /// Formats a single metric across labeled scopes.
    pub fn fmt_scopes<L: Display + Hash + Eq, S, F: Fn(&S) -> &M>(
        &self,
        f: &mut fmt::Formatter,
        scopes: &Scopes<L, S>,
        to_metric: F
    )-> fmt::Result {
        for (labels, scope) in &scopes.scopes {
            to_metric(scope).fmt_metric_labeled(f, self.name, labels)?;
        }

        Ok(())
    }
}

// ===== impl Root =====

impl Root {
    metrics! {
        process_start_time_seconds: Gauge {
            "Time that the process started (in seconds since the UNIX epoch)"
        }
    }

    pub fn new(process: &Arc<ctx::Process>) -> Self {
        let t0 = process.start_time
            .duration_since(UNIX_EPOCH)
            .expect("process start time")
            .as_secs();

        Self {
            start_time: t0.into(),
            .. Root::default()
        }
    }

    fn request(&mut self, labels: RequestLabels) -> &mut http::RequestMetrics {
        self.requests.scopes.entry(labels)
            .or_insert_with(|| http::RequestMetrics::default().into())
            .stamped()
    }

    fn response(&mut self, labels: ResponseLabels) -> &mut http::ResponseMetrics {
        self.responses.scopes.entry(labels)
            .or_insert_with(|| http::ResponseMetrics::default().into())
            .stamped()
    }

    fn transport(&mut self, labels: TransportLabels) -> &mut transport::OpenMetrics {
        self.transports.scopes.entry(labels)
            .or_insert_with(|| transport::OpenMetrics::default().into())
            .stamped()
    }

    fn transport_close(&mut self, labels: TransportCloseLabels) -> &mut transport::CloseMetrics {
        self.transport_closes.scopes.entry(labels)
            .or_insert_with(|| transport::CloseMetrics::default().into())
            .stamped()
    }

    fn retain_since(&mut self, epoch: Instant) {
        self.requests.retain_since(epoch);
        self.responses.retain_since(epoch);
        self.transports.retain_since(epoch);
        self.transport_closes.retain_since(epoch);
    }
}

impl fmt::Display for Root {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        self.requests.fmt(f)?;
        self.responses.fmt(f)?;
        self.transports.fmt(f)?;
        self.transport_closes.fmt(f)?;

        Self::process_start_time_seconds.fmt_help(f)?;
        Self::process_start_time_seconds.fmt_metric(f, self.start_time)?;

        Ok(())
    }
}

// ===== impl Stamped =====

impl<T> Stamped<T> {
    fn stamped(&mut self) -> &mut T {
        self.stamp = Instant::now();
        &mut self.inner
    }
}

impl<T> From<T> for Stamped<T> {
    fn from(inner: T) -> Self {
        Self {
            inner,
            stamp: Instant::now(),
        }
    }
}

impl<T> ::std::ops::Deref for Stamped<T> {
    type Target = T;
    fn deref(&self) -> &Self::Target {
        &self.inner
    }
}

// ===== impl Scopes =====

impl<L: Display + Hash + Eq, S> Default for Scopes<L, S> {
    fn default() -> Self {
        Scopes { scopes: IndexMap::default(), }
    }
}

impl<L: Display + Hash + Eq, S> Scopes<L, Stamped<S>> {
    fn retain_since(&mut self, epoch: Instant) {
        self.scopes.retain(|_, v| v.stamp >= epoch);
    }
}

#[cfg(test)]
mod tests {
    use ctx::test_util::*;
    use telemetry::event;
    use super::*;

    fn mock_route(
        root: &mut Root,
        proxy: &Arc<ctx::Proxy>,
        server: &Arc<ctx::transport::Server>,
        team: &str
    ) {
        let client = client(&proxy, vec![("team", team)]);
        let (req, rsp) = request("http://nba.com", &server, &client, 1);

        let client_transport = Arc::new(ctx::transport::Ctx::Client(client));
        let transport = TransportLabels::new(&client_transport);
        root.transport(transport.clone()).open();

        root.request(RequestLabels::new(&req)).end();
        root.response(ResponseLabels::new(&rsp, None)).end(Duration::from_millis(10));
        root.transport(transport).close(100, 200);

        let end = TransportCloseLabels::new(&client_transport, &event::TransportClose {
            clean: true,
            duration: Duration::from_millis(15),
            rx_bytes: 40,
            tx_bytes: 0,
        });
        root.transport_close(end).close(Duration::from_millis(15));
    }

    #[test]
    fn expiry() {
        let process = process();
        let proxy = ctx::Proxy::outbound(&process);

        let server = server(&proxy);
        let server_transport = Arc::new(ctx::transport::Ctx::Server(server.clone()));

        let mut root = Root::default();

        let t0 = Instant::now();
        root.transport(TransportLabels::new(&server_transport)).open();

        mock_route(&mut root, &proxy, &server, "warriors");
        let t1 = Instant::now();

        mock_route(&mut root, &proxy, &server, "sixers");
        let t2 = Instant::now();

        assert_eq!(root.requests.scopes.len(), 2);
        assert_eq!(root.responses.scopes.len(), 2);
        assert_eq!(root.transports.scopes.len(), 2);
        assert_eq!(root.transport_closes.scopes.len(), 1);

        root.retain_since(t0);
        assert_eq!(root.requests.scopes.len(), 2);
        assert_eq!(root.responses.scopes.len(), 2);
        assert_eq!(root.transports.scopes.len(), 2);
        assert_eq!(root.transport_closes.scopes.len(), 1);

        root.retain_since(t1);
        assert_eq!(root.requests.scopes.len(), 1);
        assert_eq!(root.responses.scopes.len(), 1);
        assert_eq!(root.transports.scopes.len(), 1);
        assert_eq!(root.transport_closes.scopes.len(), 1);

        root.retain_since(t2);
        assert_eq!(root.requests.scopes.len(), 0);
        assert_eq!(root.responses.scopes.len(), 0);
        assert_eq!(root.transports.scopes.len(), 0);
        assert_eq!(root.transport_closes.scopes.len(), 0);
    }
}
