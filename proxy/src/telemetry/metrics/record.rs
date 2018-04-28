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
                let labels = Arc::new(RequestLabels::new(req));
                self.update(|metrics| {
                    metrics.request_total(&labels).incr();
                })
            },

            Event::StreamRequestEnd(ref req, _) => {
                let labels = Arc::new(RequestLabels::new(req));
                self.update(|metrics| {
                    metrics.request_total(&labels).incr();
                })
            },

            Event::StreamResponseEnd(ref res, ref end) => {
                let labels = Arc::new(ResponseLabels::new(
                    res,
                    end.grpc_status,
                ));
                self.update(|metrics| {
                    metrics.response_total(&labels).incr();
                    metrics.response_latency(&labels).add(end.since_request_open);
                });
            },

            Event::StreamResponseFail(ref res, ref fail) => {
                // TODO: do we care about the failure's error code here?
                let labels = Arc::new(ResponseLabels::fail(res));
                self.update(|metrics| {
                    metrics.response_total(&labels).incr();
                    metrics.response_latency(&labels).add(fail.since_request_open);
                });
            },

            Event::TransportOpen(ref ctx) => {
                let labels = Arc::new(TransportLabels::new(ctx));
                self.update(|metrics| {
                    metrics.tcp().open_total(&labels).incr();
                    metrics.tcp().open_connections(&labels).incr();
                })
            },

            Event::TransportClose(ref ctx, ref close) => {
                let labels = Arc::new(TransportLabels::new(ctx));
                let close_labels = Arc::new(TransportCloseLabels::new(ctx, close));
                self.update(|metrics| {
                    *metrics.tcp().write_bytes_total(&labels) += close.tx_bytes as u64;
                    *metrics.tcp().read_bytes_total(&labels) += close.rx_bytes as u64;

                    metrics.tcp().connection_duration(&close_labels).add(close.duration);
                    metrics.tcp().close_total(&close_labels).incr();

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
