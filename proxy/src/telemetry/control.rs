use std::io;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use futures::{future, Async, Future, Poll, Stream};
use futures_mpsc_lossy::Receiver;
use tokio::executor::current_thread::TaskExecutor;

use super::event::Event;
use super::metrics;
use super::tap::Taps;
use connection;
use ctx;
use task;

/// A `Control` which has been configured but not initialized.
#[derive(Debug)]
pub struct MakeControl {
    /// Receives events.
    rx: Receiver<Event>,

    process_ctx: Arc<ctx::Process>,

    metrics_retain_idle: Duration,
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
#[derive(Debug)]
pub struct Control {
    /// Records telemetry events.
    metrics_record: metrics::Record,

    /// Serves scrapable metrics.
    metrics_service: metrics::Serve,

    /// Receives telemetry events.
    rx: Option<Receiver<Event>>,

    /// Holds the current state of tap observations, as configured by an external source.
    taps: Option<Arc<Mutex<Taps>>>,

}

// ===== impl Control =====

impl Control {

    /// Returns a new `Control`.
    ///
    /// # Arguments
    /// - `rx`: the `Receiver` side of the channel on which events are sent.
    /// - `process_ctx`: runtime process metadata.
    /// - `taps`: shares a `Taps` instance.
    pub(super) fn new(
        rx: Receiver<Event>,
        process_ctx: &Arc<ctx::Process>,
        metrics_retain_idle: Duration,
        taps: &Arc<Mutex<Taps>>
    ) -> Self {
        let (metrics_record, metrics_service) =
            metrics::new(&process_ctx, metrics_retain_idle);
        Self {
            metrics_record,
            metrics_service,
            rx: Some(rx),
            taps: Some(taps.clone()),
        }
    }

    fn recv(&mut self) -> Poll<Option<Event>, ()> {
        match self.rx.take() {
            None => Ok(Async::Ready(None)),
            Some(mut rx) => {
                match rx.poll() {
                    Ok(Async::Ready(None)) => Ok(Async::Ready(None)),
                    ev => {
                        self.rx = Some(rx);
                        ev
                    }
                }
            }
        }
    }

    pub fn serve_metrics(&self, bound_port: connection::BoundPort)
        -> impl Future<Item = (), Error = io::Error>
    {
        use hyper;

        let log = ::logging::admin().server("metrics", bound_port.local_addr());
        let service = self.metrics_service.clone();
        let fut = {
            let log = log.clone();
            bound_port.listen_and_fold(
                None, // TODO: Serve over TLS.
                hyper::server::conn::Http::new(),
                move |hyper, (conn, remote)| {
                    let service = service.clone();
                    let serve = hyper.serve_connection(conn, service)
                        .map(|_| {})
                        .map_err(|e| {
                            error!("error serving prometheus metrics: {:?}", e);
                        });
                    let serve = log.clone().with_remote(remote).future(serve);

                    let r = TaskExecutor::current()
                        .spawn_local(Box::new(serve))
                        .map(move |()| hyper)
                        .map_err(task::Error::into_io);

                    future::result(r)
                })
        };

        log.future(fut)
    }

}

impl Future for Control {
    type Item = ();
    type Error = ();

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        loop {
            match try_ready!(self.recv()) {
                Some(ev) => {
                    if let Some(taps) = self.taps.as_mut() {
                        if let Ok(mut t) = taps.lock() {
                            t.inspect(&ev);
                        }
                    }

                    self.metrics_record.record_event(&ev);
                }
                None => {
                    debug!("events finished");
                    return Ok(Async::Ready(()));
                }
            };
        }
    }
}
