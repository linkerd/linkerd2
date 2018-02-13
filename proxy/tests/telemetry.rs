#[macro_use]
extern crate log;

mod support;
use self::support::*;

#[test]
fn inbound_sends_telemetry() {
    let _ = env_logger::init();

    info!("running test server");
    let srv = server::new().route("/hey", "hello").run();

    let mut ctrl = controller::new();
    let reports = ctrl.reports();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::new(proxy.inbound, "test.conduit.local");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    info!("awaiting report");
    let report = reports.wait().next().unwrap().unwrap();
    // proxy inbound
    assert_eq!(report.proxy, 0);
    // process
    assert_eq!(report.process.as_ref().unwrap().node, "");
    assert_eq!(report.process.as_ref().unwrap().scheduled_instance, "");
    assert_eq!(report.process.as_ref().unwrap().scheduled_namespace, "");
    // requests
    assert_eq!(report.requests.len(), 1);
    let req = &report.requests[0];
    assert_eq!(req.ctx.as_ref().unwrap().authority, "test.conduit.local");
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
    let _ = env_logger::init();

    info!("running test server");
    let srv = server::http1().route("/hey", "hello").run();

    let mut ctrl = controller::new();
    let reports = ctrl.reports();
    let proxy = proxy::new()
        .controller(ctrl.run())
        .inbound(srv)
        .metrics_flush_interval(Duration::from_millis(500))
        .run();
    let client = client::http1(proxy.inbound, "test.conduit.local");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    info!("awaiting report");
    let report = reports.wait().next().unwrap().unwrap();
    // proxy inbound
    assert_eq!(report.proxy, 0);
    // requests
    assert_eq!(report.requests.len(), 1);
    let req = &report.requests[0];
    assert_eq!(req.ctx.as_ref().unwrap().authority, "test.conduit.local");
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
    let _ = env_logger::init();

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
    let client = client::new(proxy.inbound, "test.conduit.local");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");
    assert_eq!(client.get("/hi"), "good morning");

    info!("awaiting report");
    let report = reports.wait().next().unwrap().unwrap();
    // proxy inbound
    assert_eq!(report.proxy, 0);
    // process
    assert_eq!(report.process.as_ref().unwrap().node, "");
    assert_eq!(report.process.as_ref().unwrap().scheduled_instance, "");
    assert_eq!(report.process.as_ref().unwrap().scheduled_namespace, "");

    // requests -----------------------
    assert_eq!(report.requests.len(), 2);

    // -- first request -----------------
    let req = &report.requests[0];
    assert_eq!(req.ctx.as_ref().unwrap().authority, "test.conduit.local");
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
    assert_eq!(req.ctx.as_ref().unwrap().authority, "test.conduit.local");
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

// Ignore this test for now, because our method of adding latency to requests
// (calling `thread::sleep`) is likely to be flakey, especially on CI.
// Eventually, we can add some kind of mock timer system for simulating latency
// more reliably, and re-enable this test.
#[test]
#[ignore]
fn records_latency_statistics() {
    let _ = env_logger::init();

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
    let client = client::new(proxy.inbound, "test.conduit.local");

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
    assert_eq!(req.ctx.as_ref().unwrap().authority, "test.conduit.local");
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
    assert_eq!(req.ctx.as_ref().unwrap().authority, "test.conduit.local");
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

