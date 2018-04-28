use std::sync::{Arc, Mutex};

use telemetry::event::Event;
use super::Metrics;
use super::labels::{
    RequestLabels,
    ResponseLabels,
    TransportLabels,
    TransportCloseLabels
};

/// Tracks Prometheus metrics
#[derive(Debug)]
pub struct Record {
    metrics: Arc<Mutex<Metrics>>,
}

// ===== impl Record =====

impl Record {
    pub(super) fn new(metrics: &Arc<Mutex<Metrics>>) -> Self {
        Self { metrics: metrics.clone() }
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
                self.update(|metrics| {
                    metrics.request_total(RequestLabels::new(req)).incr();
                })
            },

            Event::StreamRequestEnd(ref req, _) => {
                self.update(|metrics| {
                    metrics.request_total(RequestLabels::new(req)).incr();
                })
            },

            Event::StreamResponseEnd(ref res, ref end) => {
                self.update(|metrics| {
                    let labels = ResponseLabels::new(res, end.grpc_status);
                    metrics.response_total(labels.clone()).incr();
                    metrics.response_latency(labels).add(end.since_request_open);
                });
            },

            Event::StreamResponseFail(ref res, ref fail) => {
                self.update(|metrics| {
                    // TODO: do we care about the failure's error code here?
                    let labels = ResponseLabels::fail(res);
                    metrics.response_total(labels.clone()).incr();
                    metrics.response_latency(labels).add(fail.since_request_open);
                });
            },

            Event::TransportOpen(ref ctx) => {
                let labels = TransportLabels::new(ctx);
                self.update(|metrics| {
                    metrics.tcp().open_total(labels).incr();
                    metrics.tcp().open_connections(labels).incr();
                })
            },

            Event::TransportClose(ref ctx, ref close) => {
                let labels = TransportLabels::new(ctx);
                let close_labels = TransportCloseLabels::new(ctx, close);
                self.update(|metrics| {
                    *metrics.tcp().write_bytes_total(labels) += close.tx_bytes as u64;
                    *metrics.tcp().read_bytes_total(labels) += close.rx_bytes as u64;

                    metrics.tcp().connection_duration(close_labels).add(close.duration);
                    metrics.tcp().close_total(close_labels).incr();

                    let metrics = metrics.tcp().open_connections.values.get_mut(&labels);
                    debug_assert!(metrics.is_some());
                    match metrics {
                        Some(m) => {
                            m.decr();
                        }
                        None => {
                            error!("Closed transport missing from metrics registry: {{{}}}", labels);
                        }
                    }
                })
            },
        };
    }
}
