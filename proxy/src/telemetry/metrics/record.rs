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

#[cfg(test)]
mod test {
    use telemetry::{
        event,
        metrics::{self, labels},
        Event,
    };
    use ctx::{self, test_util::* };
    use std::time::Duration;

    #[test]
    fn record_response_end() {
        let process = process();
        let proxy = ctx::Proxy::outbound(&process);
        let server = server(&proxy);

        let client = client(&proxy, vec![
            ("service", "draymond"),
            ("deployment", "durant"),
            ("pod", "klay"),
        ]);

        let (_, rsp) = request("http://buoyant.io", &server, &client, 1);

        let end = event::StreamResponseEnd {
            grpc_status: None,
            since_request_open: Duration::from_millis(300),
            since_response_open: Duration::from_millis(0),
            bytes_sent: 0,
            frames_sent: 0,
        };

        let (mut r, _) = metrics::new(&process, Duration::from_secs(100));
        let ev = Event::StreamResponseEnd(rsp.clone(), end.clone());
        let labels = labels::ResponseLabels::new(&rsp, None);

        assert!(r.metrics.lock()
            .expect("lock")
            .responses.scopes
            .get(&labels)
            .is_none()
        );

        r.record_event(&ev);
        {
            let lock = r.metrics.lock()
                .expect("lock");
            let scope = lock.responses.scopes
                .get(&labels)
                .expect("scope should be some after event");

            let total: u64 = scope.total.into();
            assert_eq!(total, 1);

            scope.latency.assert_bucket_exactly(300, 1);
            scope.latency.assert_lt_exactly(300, 0);
            scope.latency.assert_gt_exactly(300, 0);
        }

    }


}
