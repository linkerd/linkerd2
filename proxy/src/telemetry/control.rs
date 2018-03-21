use std::{fmt, io};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use futures::{future, Async, Future, Poll, Stream};
use futures_mpsc_lossy::Receiver;
use tokio_core::reactor::{Handle, Timeout};

use super::event::Event;
use super::metrics::{prometheus, Metrics as PushMetrics};
use super::tap::Taps;
use conduit_proxy_controller_grpc::telemetry::ReportRequest;
use connection;
use ctx;

/// A `Control` which has been configured but not initialized.
#[derive(Debug)]
pub struct MakeControl {
    /// Receives events.
    rx: Receiver<Event>,

    /// Limits the amount of time metrics may be buffered before being flushed to the
    /// controller.
    flush_interval: Duration,

    process_ctx: Arc<ctx::Process>,
}

/// Handles the receipt of events.
///
/// `Control` exposes a `Stream` that summarizes events accumulated over the past
/// `flush_interval`.
///
/// As `Control` is polled, events are proceesed for the purposes of metrics export _as
/// well as_ for Tap, which supports subscribing to a stream of events that match
/// criteria.
///
/// # TODO
/// Limit the amount of memory that may be consumed for metrics aggregation.
pub struct Control {
    /// Holds the current state of aggregated metrics.
    push_metrics: Option<PushMetrics>,

    /// Aggregates scrapable metrics.
    metrics_work: prometheus::Aggregate,

    /// Serves scrapable metrics.
    metrics_service: prometheus::Serve,

    /// Receives telemetry events.
    rx: Option<Receiver<Event>>,

    /// Holds the current state of tap observations, as configured by an external source.
    taps: Option<Arc<Mutex<Taps>>>,

    /// Limits the amount of time metrics may be buffered before being flushed to the
    /// controller.
    flush_interval: Duration,

    /// Ensures liveliness of telemetry by waking the stream to produce reports when
    /// needed.  This timeout is reset as reports are returned.
    flush_timeout: Timeout,

    handle: Handle,
}

// ===== impl MakeControl =====

impl MakeControl {
    /// Constructs a type that can instantiate a `Control`.
    ///
    /// # Arguments
    /// - `rx`: the `Receiver` side of the channel on which events are sent.
    /// - `flush_interval`: the maximum amount of time between sending reports to the
    ///   controller.
    pub(super) fn new(
        rx: Receiver<Event>,
        flush_interval: Duration,
        process_ctx: &Arc<ctx::Process>,
    ) -> Self {
        Self {
            rx,
            flush_interval,
            process_ctx: Arc::clone(process_ctx),
        }
    }

    /// Bind a `Control` with a reactor core.
    ///
    /// # Arguments
    /// - `handle`: a `Handle` on an event loop that will track the timeout.
    /// - `taps`: shares a `Taps` instance.
    ///
    /// # Returns
    /// - `Ok(())` if the timeout was successfully created.
    /// - `Err(io::Error)` if the timeout could not be created.
    pub fn make_control(self, taps: &Arc<Mutex<Taps>>, handle: &Handle) -> io::Result<Control> {
        trace!("telemetry control flush_interval={:?}", self.flush_interval);

        let flush_timeout = Timeout::new(self.flush_interval, handle)?;
        let (metrics_work, metrics_service) = prometheus::new();
        let push_metrics = Some(PushMetrics::new(self.process_ctx));

        Ok(Control {
            push_metrics,
            metrics_work,
            metrics_service,
            rx: Some(self.rx),
            taps: Some(taps.clone()),
            flush_interval: self.flush_interval,
            flush_timeout,
            handle: handle.clone(),
        })
    }
}

// ===== impl Control =====

impl Control {
    /// Returns true if the flush timeout has expired, false otherwise.
    #[inline]
    fn flush_timeout_expired(&mut self) -> bool {
        self.flush_timeout
            .poll()
            .ok()
            .map(|r| r.is_ready())
            .unwrap_or(false)
    }

    /// Returns true if this `Control` should flush metrics.
    ///
    /// Metrics should be flushed if either of the following conditions are true:
    /// - we have aggregated `flush_bytes` bytes of data,
    /// - we haven't sent a report in `flush_interval` seconds.
    fn flush_report(&mut self) -> Option<ReportRequest> {
        let metrics = if self.flush_timeout_expired() {
            trace!("flush timeout expired");
            self.push_metrics.as_mut()
        } else {
            None
        };

        metrics.map(Self::generate_report)
    }

    fn generate_report(m: &mut PushMetrics) -> ReportRequest {
        let mut r = m.generate_report();
        r.proxy = 0; // 0 = Inbound, 1 = Outbound
        r
    }

    /// Reset the flush timeout.
    fn reset_timeout(&mut self) {
        trace!("flushing in {:?}", self.flush_interval);
        self.flush_timeout
            .reset(Instant::now() + self.flush_interval);
    }

    fn recv(&mut self) -> Async<Option<Event>> {
        match self.rx.take() {
            None => Async::Ready(None),
            Some(mut rx) => {
                trace!("recv.poll({:?})", rx);
                match rx.poll().expect("recv telemetry") {
                    Async::Ready(None) => Async::Ready(None),
                    ev => {
                        self.rx = Some(rx);
                        ev
                    }
                }
            }
        }
    }

    pub fn serve_metrics(&self, bound_port: connection::BoundPort)
        -> Box<Future<Item = (), Error = io::Error> + 'static>
    {
        use hyper;
        let service = self.metrics_service.clone();
        let hyper = hyper::server::Http::<hyper::Chunk>::new();
        bound_port.listen_and_fold(
            &self.handle,
            (hyper, self.handle.clone()),
            move |(hyper, executor), (conn, _)| {
                let service = service.clone();
                let serve = hyper.serve_connection(conn, service)
                    .map(|_| {})
                    .map_err(|e| {
                        error!("error serving prometheus metrics: {:?}", e);
                    });

                executor.spawn(::logging::context_future("serve_metrics", serve));

                future::ok((hyper, executor))
            })
    }

}

impl Stream for Control {
    type Item = ReportRequest;
    type Error = ();

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        trace!("poll");
        loop {
            let report = match self.recv() {
                Async::NotReady => break,
                Async::Ready(Some(ev)) => {
                    if let Some(taps) = self.taps.as_mut() {
                        if let Ok(mut t) = taps.lock() {
                            t.inspect(&ev);
                        }
                    }

                    // XXX Only inbound events are currently aggregated.
                    if ev.proxy().is_inbound() {
                        if let Some(metrics) = self.push_metrics.as_mut() {
                            metrics.record_event(&ev);
                        }
                    }

                    self.metrics_work.record_event(&ev);

                    self.flush_report()
                }
                Async::Ready(None) => {
                    warn!("events finished");
                    let report = self.push_metrics
                        .take()
                        .map(|mut m| Self::generate_report(&mut m));
                    if report.is_none() {
                        return Ok(Async::Ready(None));
                    }
                    report
                }
            };

            if let Some(report) = report {
                self.reset_timeout();
                return Ok(Async::Ready(Some(report)));
            }
        }

        // There may be no new events, but the timeout fired; so check at least once
        // explicitly:
        if self.push_metrics.is_none() {
            Ok(Async::Ready(None))
        } else {
            match self.flush_report() {
                None => {
                    // Either `rx` isn't ready or the timeout isn't ready
                    Ok(Async::NotReady)
                }
                Some(report) => {
                    self.reset_timeout();
                    Ok(Async::Ready(Some(report)))
                }
            }
        }
    }
}

// NOTE: `flush_timeout` does not impl `Debug`.
impl fmt::Debug for Control {
    fn fmt(&self, fmt: &mut fmt::Formatter) -> fmt::Result {
        fmt.debug_struct("Control")
            .field("push_metrics", &self.push_metrics)
            .field("rx", &self.rx)
            .field("taps", &self.taps)
            .field("flush_interval", &self.flush_interval)
            .field(
                "flush_timeout",
                &format!("Timeout({:?})", &self.flush_interval),
            )
            .finish()
    }
}
