#[macro_use]
extern crate log;

mod support;
use self::support::*;

#[test]
fn inbound_sends_telemetry() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new().route("/hey", "hello").run();

    let mut ctrl = controller::new();
    let reports = ctrl.reports();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    info!("awaiting report");
    let report = reports.wait().next().unwrap().unwrap();
    // proxy inbound
    assert_eq!(report.proxy, 0);
    // process
    assert_eq!(report.process.as_ref().unwrap().node, "");
    assert_eq!(report.process.as_ref().unwrap().scheduled_instance, "");
    assert_eq!(report.process.as_ref().unwrap().scheduled_namespace, "test");
    // requests
    assert_eq!(report.requests.len(), 1);
    let req = &report.requests[0];
    assert_eq!(req.ctx.as_ref().unwrap().authority, "tele.test.svc.cluster.local");
    //assert_eq!(req.ctx.as_ref().unwrap().method, GET);
    assert_eq!(req.count, 1);
    assert_eq!(req.responses.len(), 1);
    // responses
    let res = &req.responses[0];
    assert_eq!(res.ctx.as_ref().unwrap().http_status_code, 200);
    // response latencies should always have a length equal to the number
    // of latency buckets in the latency histogram.
    assert_eq!(
        res.response_latency_counts.len(),
        report.histogram_bucket_bounds_tenth_ms.len()
    );
    assert_eq!(res.ends.len(), 1);
    // ends
    let ends = &res.ends[0];
    assert_eq!(ends.streams, 1);
}


#[test]
fn http1_inbound_sends_telemetry() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::http1().route("/hey", "hello").run();

    let mut ctrl = controller::new();
    let reports = ctrl.reports();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::http1(proxy.inbound, "tele.test.svc.cluster.local");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    info!("awaiting report");
    let report = reports.wait().next().unwrap().unwrap();
    // proxy inbound
    assert_eq!(report.proxy, 0);
    // requests
    assert_eq!(report.requests.len(), 1);
    let req = &report.requests[0];
    assert_eq!(req.ctx.as_ref().unwrap().authority, "tele.test.svc.cluster.local");
    //assert_eq!(req.ctx.as_ref().unwrap().method, GET);
    assert_eq!(req.count, 1);
    assert_eq!(req.responses.len(), 1);
    // responses
    let res = &req.responses[0];
    assert_eq!(res.ctx.as_ref().unwrap().http_status_code, 200);
    // response latencies should always have a length equal to the number
    // of latency buckets in the latency histogram.
    assert_eq!(
        res.response_latency_counts.len(),
        report.histogram_bucket_bounds_tenth_ms.len()
    );
    assert_eq!(res.ends.len(), 1);
    // ends
    let ends = &res.ends[0];
    assert_eq!(ends.streams, 1);
}


#[test]
fn inbound_aggregates_telemetry_over_several_requests() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new()
        .route("/hey", "hello")
        .route("/hi", "good morning")
        .run();

    let mut ctrl = controller::new();
    let reports = ctrl.reports();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");
    assert_eq!(client.get("/hi"), "good morning");

    info!("awaiting report");
    let report = reports.wait().next().unwrap().unwrap();
    // proxy inbound
    assert_eq!(report.proxy, 0);

    // requests -----------------------
    assert_eq!(report.requests.len(), 2);

    // -- first request -----------------
    let req = &report.requests[0];
    assert_eq!(req.ctx.as_ref().unwrap().authority, "tele.test.svc.cluster.local");
    assert_eq!(req.count, 1);
    assert_eq!(req.responses.len(), 1);
    // ---- response --------------------
    let res = &req.responses[0];
    assert_eq!(res.ctx.as_ref().unwrap().http_status_code, 200);
    // response latencies should always have a length equal to the number
    // of latency buckets in the latency histogram.
    assert_eq!(
        res.response_latency_counts.len(),
        report.histogram_bucket_bounds_tenth_ms.len()
    );
    assert_eq!(res.ends.len(), 1);

    // ------ ends ----------------------
    let ends = &res.ends[0];
    assert_eq!(ends.streams, 1);

    // -- second request ----------------
    let req = &report.requests[1];
    assert_eq!(req.ctx.as_ref().unwrap().authority, "tele.test.svc.cluster.local");
    // repeated twice
    assert_eq!(req.count, 2);
    assert_eq!(req.responses.len(), 1);
    // ---- response  -------------------
    let res = &req.responses[0];
    assert_eq!(res.ctx.as_ref().unwrap().http_status_code, 200);
    // response latencies should always have a length equal to the number
    // of latency buckets in the latency histogram.
    assert_eq!(
        res.response_latency_counts.len(),
        report.histogram_bucket_bounds_tenth_ms.len()
    );
    assert_eq!(res.ends.len(), 1);

    // ------ ends ----------------------
    let ends = &res.ends[0];
    assert_eq!(ends.streams, 2);

}

// Ignore this test on CI, because our method of adding latency to requests
// (calling `thread::sleep`) is likely to be flakey on Travis.
// Eventually, we can add some kind of mock timer system for simulating latency
// more reliably, and re-enable this test.
#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn records_latency_statistics() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new()
        .route_with_latency("/hey", "hello", Duration::from_millis(500))
        .route_with_latency("/hi", "good morning", Duration::from_millis(40))
        .run();

    let mut ctrl = controller::new();
    let reports = ctrl.reports();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_secs(5))
        .run();
    let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");
    assert_eq!(client.get("/hi"), "good morning");

    info!("awaiting report");
    let report = reports.wait().next().unwrap().unwrap();

    // requests -----------------------
    assert_eq!(report.requests.len(), 2);
    // first request
    let req = &report.requests[0];
    assert_eq!(req.ctx.as_ref().unwrap().authority, "tele.test.svc.cluster.local");
    let res = &req.responses[0];
    // response latencies should always have a length equal to the number
    // of latency buckets in the latency histogram.
    assert_eq!(
        res.response_latency_counts.len(),
        report.histogram_bucket_bounds_tenth_ms.len()
    );
    for (idx, bucket) in res.response_latency_counts.iter().enumerate() {
        // 500 ms of extra latency should put us in the 500-1000
        // millisecond bucket (the 15th bucket)
        if idx == 15 {
            assert_eq!(*bucket, 1, "poorly bucketed latencies: {:?}", res.response_latency_counts);
        } else {
            assert_eq!(*bucket, 0, "poorly bucketed latencies: {:?}", res.response_latency_counts);
        }
    }

    // second request
    let req = &report.requests.get(1).expect("second report");
    assert_eq!(req.ctx.as_ref().unwrap().authority, "tele.test.svc.cluster.local");
    assert_eq!(req.count, 2);
    assert_eq!(req.responses.len(), 1);
    let res = req.responses.get(0).expect("responses[0]");
    // response latencies should always have a length equal to the number
    // of latency buckets in the latency histogram.
    assert_eq!(
        res.response_latency_counts.len(),
        report.histogram_bucket_bounds_tenth_ms.len()
    );
    for (idx, bucket) in res.response_latency_counts.iter().enumerate() {
        // 40 ms of extra latency should put us in the 40-50
        // millisecond bucket (the 10th bucket)
        if idx == 9 {
            assert_eq!(*bucket, 2, "poorly bucketed latencies: {:?}", res.response_latency_counts);
        } else {
            assert_eq!(*bucket, 0, "poorly bucketed latencies: {:?}", res.response_latency_counts);
        }
    }

}

#[test]
fn telemetry_report_errors_are_ignored() {}

macro_rules! assert_contains {
    ($scrape:expr, $contains:expr) => {
        assert!($scrape.contains($contains), "metrics scrape:\n{:8}\ndid not contain:\n{:8}", $scrape, $contains)
    }
}

// https://github.com/runconduit/conduit/issues/613
#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn metrics_endpoint_inbound_request_count() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new().route("/hey", "hello").run();

    let ctrl = controller::new();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");
    let metrics = client::http1(proxy.metrics, "localhost");

    // prior to seeing any requests, request count should be empty.
    assert!(!metrics.get("/metrics")
        .contains("request_total{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\"}"));

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    // after seeing a request, the request count should be 1.
    assert_contains!(scrape, "request_total{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\"} 1");

}

// https://github.com/runconduit/conduit/issues/613
#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn metrics_endpoint_outbound_request_count() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new().route("/hey", "hello").run();

    let ctrl = controller::new()
        .destination("tele.test.svc.cluster.local", srv.addr)
        .run();
    let proxy = proxy::new()
        .controller(ctrl)
        .outbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.outbound, "tele.test.svc.cluster.local");
    let metrics = client::http1(proxy.metrics, "localhost");

    // prior to seeing any requests, request count should be empty.
    assert!(!metrics.get("/metrics")
        .contains("request_total{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\"}"));

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    // after seeing a request, the request count should be 1.
    assert_contains!(scrape, "request_total{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\"} 1");

}

mod response_classification {
    use super::support::*;

    const REQ_STATUS_HEADER: &'static str = "x-test-status-requested";
    const REQ_GRPC_STATUS_HEADER: &'static str = "x-test-grpc-status-requested";

    const STATUSES: [http::StatusCode; 6] = [
        http::StatusCode::OK,
        http::StatusCode::NOT_MODIFIED,
        http::StatusCode::BAD_REQUEST,
        http::StatusCode::IM_A_TEAPOT,
        http::StatusCode::GATEWAY_TIMEOUT,
        http::StatusCode::INTERNAL_SERVER_ERROR,
    ];


    fn expected_metric(status: &http::StatusCode, direction: &str) -> String {
        format!(
            "response_total{{authority=\"tele.test.svc.cluster.local\",direction=\"{}\",classification=\"{}\",status_code=\"{}\"}} 1",
            direction,
            if status.is_server_error() { "failure" } else { "success" },
            status.as_u16(),
        )
    }

    fn make_test_server() -> server::Server {
        fn parse_header(headers: &http::HeaderMap, which: &str)
            -> Option<http::StatusCode>
        {
            headers.get(which)
                .map(|val| {
                    val.to_str()
                        .expect("requested status should be ascii")
                        .parse::<http::StatusCode>()
                        .expect("requested status should be numbers")
                })
        }
        info!("running test server");
        server::new()
            .route_fn("/", move |req| {
                let headers = req.headers();
                let status = parse_header(headers, REQ_STATUS_HEADER)
                    .unwrap_or(http::StatusCode::OK);
                let grpc_status = parse_header(headers, REQ_GRPC_STATUS_HEADER);
                let mut rsp = if let Some(_grpc_status) = grpc_status {
                    // TODO: tests for grpc statuses
                    unimplemented!()
                } else {
                    Response::new("".into())
                };
                *rsp.status_mut() = status;
                rsp
            })
    }

    // https://github.com/runconduit/conduit/issues/613
    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn inbound_http() {
        let _ = env_logger::try_init();
        let srv = make_test_server().run();
        let ctrl = controller::new();
        let proxy = proxy::new()
            .controller(ctrl.run())
            .inbound(srv)
            .metrics_flush_interval(Duration::from_millis(500))
            .run();
        let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");
        let metrics = client::http1(proxy.metrics, "localhost");

        for (i, status) in STATUSES.iter().enumerate() {
            let request = client.request(
                client.request_builder("/")
                    .header(REQ_STATUS_HEADER, status.as_str())
                    .method("GET")
            );
            assert_eq!(&request.status(), status);

            let scrape = metrics.get("/metrics");
            for status in &STATUSES[0..i] {
                // assert that the current status code is incremented, *and* that
                // all previous requests are *not* incremented.
                assert_contains!(scrape, &expected_metric(status, "inbound"))
            }
        }
    }

    // https://github.com/runconduit/conduit/issues/613
    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_http() {
        let _ = env_logger::try_init();
        let srv = make_test_server().run();
        let ctrl = controller::new()
            .destination("tele.test.svc.cluster.local", srv.addr)
            .run();
        let proxy = proxy::new()
            .controller(ctrl)
            .outbound(srv)
            .metrics_flush_interval(Duration::from_millis(500))
            .run();
        let client = client::new(proxy.outbound, "tele.test.svc.cluster.local");
        let metrics = client::http1(proxy.metrics, "localhost");

        for (i, status) in STATUSES.iter().enumerate() {
            let request = client.request(
                client.request_builder("/")
                    .header(REQ_STATUS_HEADER, status.as_str())
                    .method("GET")
            );
            assert_eq!(&request.status(), status);

            let scrape = metrics.get("/metrics");
            for status in &STATUSES[0..i] {
                // assert that the current status code is incremented, *and* that
                // all previous requests are *not* incremented.
                assert_contains!(scrape, &expected_metric(status, "outbound"))
            }
        }
    }
}

// Ignore this test on CI, because our method of adding latency to requests
// (calling `thread::sleep`) is likely to be flakey on Travis.
// Eventually, we can add some kind of mock timer system for simulating latency
// more reliably, and re-enable this test.
#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn metrics_endpoint_inbound_response_latency() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new()
        .route_with_latency("/hey", "hello", Duration::from_millis(500))
        .route_with_latency("/hi", "good morning", Duration::from_millis(40))
        .run();

    let ctrl = controller::new();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");
    let metrics = client::http1(proxy.metrics, "localhost");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    // assert the >=1000ms bucket is incremented by our request with 500ms
    // extra latency.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\",le=\"1000\"} 1");
    // the histogram's count should be 1.
    assert_contains!(scrape,
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\"} 1");
    // TODO: we're not going to make any assertions about the
    // response_latency_ms_sum stat, since its granularity depends on the actual
    // observed latencies, which may vary a bit. we could make more reliable
    // assertions about that stat if we were using a mock timer, though, as the
    // observed latency values would be predictable.

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");

    let scrape = metrics.get("/metrics");

    // request with 40ms extra latency should fall into the 50ms bucket.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\",le=\"50\"} 1");
    // 1000ms bucket should be incremented as well, since it counts *all*
    // bservations less than or equal to 1000ms, even if they also increment
    // other buckets.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\",le=\"1000\"} 2");
    // the histogram's total count should be 2.
    assert_contains!(scrape,
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\"} 2");

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");

    let scrape = metrics.get("/metrics");
    // request with 40ms extra latency should fall into the 50ms bucket.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\",le=\"50\"} 2");
    // 1000ms bucket should be incremented as well.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\",le=\"1000\"} 3");
    // the histogram's total count should be 3.
    assert_contains!(scrape,
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\"} 3");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    // 50ms bucket should be un-changed by the request with 500ms latency.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\",le=\"50\"} 2");
    // 1000ms bucket should be incremented.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\",le=\"1000\"} 4");
    // the histogram's total count should be 4.
    assert_contains!(scrape,
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",classification=\"success\",status_code=\"200\"} 4");
}

// Ignore this test on CI, because our method of adding latency to requests
// (calling `thread::sleep`) is likely to be flakey on Travis.
// Eventually, we can add some kind of mock timer system for simulating latency
// more reliably, and re-enable this test.
#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn metrics_endpoint_outbound_response_latency() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new()
        .route_with_latency("/hey", "hello", Duration::from_millis(500))
        .route_with_latency("/hi", "good morning", Duration::from_millis(40))
        .run();

    let ctrl = controller::new()
        .destination("tele.test.svc.cluster.local", srv.addr)
        .run();
    let proxy = proxy::new()
        .controller(ctrl)
        .outbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.outbound, "tele.test.svc.cluster.local");
    let metrics = client::http1(proxy.metrics, "localhost");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    // assert the >=1000ms bucket is incremented by our request with 500ms
    // extra latency.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\",le=\"1000\"} 1");
    // the histogram's count should be 1.
    assert_contains!(scrape,
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\"} 1");
    // TODO: we're not going to make any assertions about the
    // response_latency_ms_sum stat, since its granularity depends on the actual
    // observed latencies, which may vary a bit. we could make more reliable
    // assertions about that stat if we were using a mock timer, though, as the
    // observed latency values would be predictable.

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");

    let scrape = metrics.get("/metrics");

    // request with 40ms extra latency should fall into the 50ms bucket.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\",le=\"50\"} 1");
    // 1000ms bucket should be incremented as well, since it counts *all*
    // bservations less than or equal to 1000ms, even if they also increment
    // other buckets.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\",le=\"1000\"} 2");
    // the histogram's total count should be 2.
    assert_contains!(scrape,
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\"} 2");

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");

    let scrape = metrics.get("/metrics");
    // request with 40ms extra latency should fall into the 50ms bucket.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\",le=\"50\"} 2");
    // 1000ms bucket should be incremented as well.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\",le=\"1000\"} 3");
    // the histogram's total count should be 3.
    assert_contains!(scrape,
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\"} 3");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    // 50ms bucket should be un-changed by the request with 500ms latency.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\",le=\"50\"} 2");
    // 1000ms bucket should be incremented.
    assert_contains!(scrape,
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\",le=\"1000\"} 4");
    // the histogram's total count should be 4.
    assert_contains!(scrape,
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",classification=\"success\",status_code=\"200\"} 4");
}

// https://github.com/runconduit/conduit/issues/613
#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn metrics_endpoint_inbound_request_duration() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new()
        .route("/hey", "hello")
        .run();

    let ctrl = controller::new();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");
    let metrics = client::http1(proxy.metrics, "localhost");

    // request with body should increment request_duration
    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    assert_contains!(scrape,
        "request_duration_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\"} 1");

    // request without body should also increment request_duration
    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    assert_contains!(scrape,
        "request_duration_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\"} 2");
}

// https://github.com/runconduit/conduit/issues/613
#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn metrics_endpoint_outbound_request_duration() {
    let _ = env_logger::try_init();

    info!("running test server");
    let srv = server::new()
        .route("/hey", "hello")
        .run();
    let ctrl = controller::new()
        .destination("tele.test.svc.cluster.local", srv.addr)
        .run();
    let proxy = proxy::new()
        .controller(ctrl)
        .outbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.outbound, "tele.test.svc.cluster.local");
    let metrics = client::http1(proxy.metrics, "localhost");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    assert_contains!(scrape,
        "request_duration_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\"} 1");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    assert_contains!(scrape,
        "request_duration_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\"} 2");
}

#[test]
fn metrics_have_no_double_commas() {
    // Test for regressions to runconduit/conduit#600.
    let _ = env_logger::try_init();

    info!("running test server");
    let inbound_srv = server::new().route("/hey", "hello").run();
    let outbound_srv = server::new().route("/hey", "hello").run();

    let ctrl = controller::new()
        .destination("tele.test.svc.cluster.local", outbound_srv.addr)
        .run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(inbound_srv)
        .outbound(outbound_srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");
    let metrics = client::http1(proxy.metrics, "localhost");

    let scrape = metrics.get("/metrics");
    assert!(!scrape.contains(",,"));

    info!("inbound.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    assert!(!scrape.contains(",,"), "inbound metrics had double comma");

    let client = client::new(proxy.outbound, "tele.test.svc.cluster.local");

    info!("outbound.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    let scrape = metrics.get("/metrics");
    assert!(!scrape.contains(",,"), "outbound metrics had double comma");

}
