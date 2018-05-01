use std::sync::{Arc, Mutex};

use telemetry::event::Event;
use super::Root;
use super::labels::{
    RequestLabels,
    ResponseLabels,
    TransportLabels,
    TransportCloseLabels
};

/// Tracks Prometheus metrics
#[derive(Debug)]
pub struct Record {
    metrics: Arc<Mutex<Root>>,
}

// ===== impl Record =====

impl Record {
    pub(super) fn new(metrics: &Arc<Mutex<Root>>) -> Self {
        Self { metrics: metrics.clone() }
    }

    #[inline]
    fn update<F: Fn(&mut Root)>(&mut self, f: F) {
        let mut lock = self.metrics.lock()
            .expect("metrics lock poisoned");
        f(&mut *lock);
    }

    /// Observe the given event.
    pub fn record_event(&mut self, event: &Event) {
        trace!("Root::record({:?})", event);
        match *event {

            Event::StreamRequestOpen(_) => {},

            Event::StreamRequestFail(ref req, _) => {
                self.update(|metrics| {
                    metrics.request(RequestLabels::new(req)).end();
                })
            },

            Event::StreamRequestEnd(ref req, _) => {
                self.update(|metrics| {
                    metrics.request(RequestLabels::new(req)).end();
                })
            },

            Event::StreamResponseOpen(_, _) => {},

            Event::StreamResponseEnd(ref res, ref end) => {
                self.update(|metrics| {
                    metrics.response(ResponseLabels::new(res, end.grpc_status))
                        .end(end.since_request_open);
                });
            },

            Event::StreamResponseFail(ref res, ref fail) => {
                // TODO: do we care about the failure's error code here?
                self.update(|metrics| {
                    metrics.response(ResponseLabels::fail(res)).end(fail.since_request_open)
                });
            },

            Event::TransportOpen(ref ctx) => {
                self.update(|metrics| {
                    metrics.transport(TransportLabels::new(ctx)).open();
                })
            },

            Event::TransportClose(ref ctx, ref close) => {
                self.update(|metrics| {
                    metrics.transport(TransportLabels::new(ctx))
                        .close(close.rx_bytes, close.tx_bytes);

                    metrics.transport_close(TransportCloseLabels::new(ctx, close))
                        .close(close.duration);
                })
            },
        };
    }
}
