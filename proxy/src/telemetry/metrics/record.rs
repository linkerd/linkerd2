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
                let latency = end.response_first_frame_at - end.request_open_at;
                self.update(|metrics| {
                    metrics.response(ResponseLabels::new(res, end.grpc_status))
                        .end(latency);
                });
            },

            Event::StreamResponseFail(ref res, ref fail) => {
                // TODO: do we care about the failure's error code here?
                let first_frame_at = fail.response_first_frame_at.unwrap_or(fail.response_fail_at);
                let latency = first_frame_at - fail.request_open_at;
                self.update(|metrics| {
                    metrics.response(ResponseLabels::fail(res)).end(latency)
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
    use ctx::{self, test_util::*, transport::TlsStatus};
    use std::time::{Duration, Instant};
    use conditional::Conditional;
    use tls;

    const TLS_ENABLED: Conditional<(), tls::ReasonForNoTls> = Conditional::Some(());
    const TLS_DISABLED: Conditional<(), tls::ReasonForNoTls> =
        Conditional::None(tls::ReasonForNoTls::Disabled);

    fn test_record_response_end_outbound(client_tls: TlsStatus, server_tls: TlsStatus) {
        let process = process();
        let proxy = ctx::Proxy::outbound(&process);
        let server = server(&proxy, server_tls);

        let client = client(&proxy, vec![
            ("service", "draymond"),
            ("deployment", "durant"),
            ("pod", "klay"),
        ], client_tls);

        let (_, rsp) = request("http://buoyant.io", &server, &client);

        let request_open_at = Instant::now();
        let response_open_at = request_open_at + Duration::from_millis(100);
        let response_first_frame_at = response_open_at + Duration::from_millis(100);
        let response_end_at = response_first_frame_at + Duration::from_millis(100);
        let end = event::StreamResponseEnd {
            grpc_status: None,
            request_open_at,
            response_open_at,
            response_first_frame_at,
            response_end_at,
            bytes_sent: 0,
            frames_sent: 0,
        };

        let (mut r, _) = metrics::new(&process, Duration::from_secs(100));
        let ev = Event::StreamResponseEnd(rsp.clone(), end.clone());
        let labels = labels::ResponseLabels::new(&rsp, None);

        assert_eq!(labels.tls_status(), client_tls);

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

            assert_eq!(scope.total(), 1);

            scope.latency().assert_bucket_exactly(200, 1);
            scope.latency().assert_lt_exactly(200, 0);
            scope.latency().assert_gt_exactly(200, 0);
        }

    }

    fn test_record_one_conn_request_outbound(client_tls: TlsStatus, server_tls: TlsStatus) {
        use self::Event::*;
        use self::labels::*;
        use std::sync::Arc;

        let process = process();
        let proxy = ctx::Proxy::outbound(&process);
        let server = server(&proxy, server_tls);

        let client = client(&proxy, vec![
            ("service", "draymond"),
            ("deployment", "durant"),
            ("pod", "klay"),
        ], client_tls);

        let (req, rsp) = request("http://buoyant.io", &server, &client);
        let server_transport =
            Arc::new(ctx::transport::Ctx::Server(server.clone()));
        let client_transport =
             Arc::new(ctx::transport::Ctx::Client(client.clone()));
        let transport_close = event::TransportClose {
            clean: true,
            duration: Duration::from_secs(30_000),
            rx_bytes: 4321,
            tx_bytes: 4321,
        };

        let request_open_at = Instant::now();
        let request_end_at = request_open_at + Duration::from_millis(10);
        let response_open_at = request_open_at + Duration::from_millis(100);
        let response_first_frame_at = response_open_at + Duration::from_millis(100);
        let response_end_at = response_first_frame_at + Duration::from_millis(100);
        let events = vec![
            TransportOpen(server_transport.clone()),
            TransportOpen(client_transport.clone()),
            StreamRequestOpen(req.clone()),
            StreamRequestEnd(req.clone(), event::StreamRequestEnd {
                request_open_at,
                request_end_at,
            }),

            StreamResponseOpen(rsp.clone(), event::StreamResponseOpen {
                request_open_at,
                response_open_at,
            }),
            StreamResponseEnd(rsp.clone(), event::StreamResponseEnd {
                grpc_status: None,
                request_open_at,
                response_open_at,
                response_first_frame_at,
                response_end_at,
                bytes_sent: 0,
                frames_sent: 0,
            }),
            TransportClose(
                server_transport.clone(),
                transport_close.clone(),
            ),
            TransportClose(
                client_transport.clone(),
                transport_close.clone(),
            ),
        ];

        let (mut r, _) = metrics::new(&process, Duration::from_secs(1000));

        let req_labels = RequestLabels::new(&req);
        let rsp_labels = ResponseLabels::new(&rsp, None);
        let srv_open_labels = TransportLabels::new(&server_transport);
        let srv_close_labels = TransportCloseLabels::new(
            &ctx::transport::Ctx::Server(server.clone()),
            &transport_close,
        );
        let client_open_labels = TransportLabels::new(&client_transport);
        let client_close_labels = TransportCloseLabels::new(
            &ctx::transport::Ctx::Client(client.clone()),
            &transport_close,
        );

        assert_eq!(client_tls, req_labels.tls_status());
        assert_eq!(client_tls, rsp_labels.tls_status());
        assert_eq!(client_tls, client_open_labels.tls_status());
        assert_eq!(client_tls, client_close_labels.tls_status());
        assert_eq!(server_tls, srv_open_labels.tls_status());
        assert_eq!(server_tls, srv_close_labels.tls_status());

        {
            let lock = r.metrics.lock()
                .expect("lock");
            assert!(lock.requests.scopes.get(&req_labels).is_none());
            assert!(lock.responses.scopes.get(&rsp_labels).is_none());
            assert!(lock.transports.scopes.get(&srv_open_labels).is_none());
            assert!(lock.transports.scopes.get(&client_open_labels).is_none());
            assert!(lock.transport_closes.scopes.get(&srv_close_labels).is_none());
            assert!(lock.transport_closes.scopes.get(&client_close_labels).is_none());
        }

        for e in &events {
            r.record_event(e);
        }

        {
            let lock = r.metrics.lock()
                .expect("lock");

            // === request scope ====================================
            assert_eq!(
                lock.requests.scopes
                    .get(&req_labels)
                    .map(|scope| scope.total()),
                Some(1)
            );

            // === response scope ===================================
            let response_scope = lock
                .responses.scopes
                .get(&rsp_labels)
                .expect("response scope missing");
            assert_eq!(response_scope.total(), 1);

            response_scope.latency()
                .assert_bucket_exactly(200, 1)
                .assert_gt_exactly(200, 0)
                .assert_lt_exactly(200, 0);

            // === server transport open scope ======================
            let srv_transport_scope = lock
                .transports.scopes
                .get(&srv_open_labels)
                .expect("server transport scope missing");
            assert_eq!(srv_transport_scope.open_total(), 1);
            assert_eq!(srv_transport_scope.write_bytes_total(), 4321);
            assert_eq!(srv_transport_scope.read_bytes_total(), 4321);

            // === client transport open scope ======================
            let client_transport_scope = lock
                .transports.scopes
                .get(&client_open_labels)
                .expect("client transport scope missing");
            assert_eq!(client_transport_scope.open_total(), 1);
            assert_eq!(client_transport_scope.write_bytes_total(), 4321);
            assert_eq!(client_transport_scope.read_bytes_total(), 4321);

            let transport_duration: u64 = 30_000 * 1_000;

            // === server transport close scope =====================
            let srv_transport_close_scope = lock
                .transport_closes.scopes
                .get(&srv_close_labels)
                .expect("server transport close scope missing");
            assert_eq!(srv_transport_close_scope.close_total(), 1);
            srv_transport_close_scope.connection_duration()
                .assert_bucket_exactly(transport_duration, 1)
                .assert_gt_exactly(transport_duration, 0)
                .assert_lt_exactly(transport_duration, 0);

            // === client transport close scope =====================
            let client_transport_close_scope = lock
                .transport_closes.scopes
                .get(&client_close_labels)
                .expect("client transport close scope missing");
            assert_eq!(client_transport_close_scope.close_total(), 1);
            client_transport_close_scope.connection_duration()
                .assert_bucket_exactly(transport_duration, 1)
                .assert_gt_exactly(transport_duration, 0)
                .assert_lt_exactly(transport_duration, 0);
        }
    }

    #[test]
    fn record_one_conn_request_outbound_client_tls() {
        test_record_one_conn_request_outbound(TLS_ENABLED, TLS_DISABLED)
    }

    #[test]
    fn record_one_conn_request_outbound_server_tls() {
        test_record_one_conn_request_outbound(TLS_DISABLED, TLS_ENABLED)
    }

    #[test]
    fn record_one_conn_request_outbound_both_tls() {
        test_record_one_conn_request_outbound(TLS_ENABLED, TLS_ENABLED)
    }

    #[test]
    fn record_one_conn_request_outbound_no_tls() {
        test_record_one_conn_request_outbound(TLS_DISABLED, TLS_DISABLED)
    }

    #[test]
    fn record_response_end_outbound_client_tls() {
        test_record_response_end_outbound(TLS_ENABLED, TLS_DISABLED)
    }

    #[test]
    fn record_response_end_outbound_server_tls() {
        test_record_response_end_outbound(TLS_DISABLED, TLS_ENABLED)
    }

    #[test]
    fn record_response_end_outbound_both_tls() {
        test_record_response_end_outbound(TLS_ENABLED, TLS_ENABLED)
    }

    #[test]
    fn record_response_end_outbound_no_tls() {
        test_record_response_end_outbound(TLS_DISABLED, TLS_DISABLED)
    }
}
