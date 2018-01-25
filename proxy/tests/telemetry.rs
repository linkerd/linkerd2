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
    assert_eq!(req.ctx.as_ref().unwrap().path, "/hey");
    //assert_eq!(req.ctx.as_ref().unwrap().method, GET);
    assert_eq!(req.count, 1);
    assert_eq!(req.responses.len(), 1);
    // responses
    let res = &req.responses[0];
    assert_eq!(res.ctx.as_ref().unwrap().http_status_code, 200);
    assert_eq!(res.response_latencies.len(), 1);
    assert_eq!(res.ends.len(), 1);
    // ends
    let ends = &res.ends[0];
    assert_eq!(ends.streams.len(), 1);
    // streams
    let stream = &ends.streams[0];
    assert_eq!(stream.bytes_sent, 5);
    assert_eq!(stream.frames_sent, 1);
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
    assert_eq!(req.ctx.as_ref().unwrap().path, "/hey");
    //assert_eq!(req.ctx.as_ref().unwrap().method, GET);
    assert_eq!(req.count, 1);
    assert_eq!(req.responses.len(), 1);
    // responses
    let res = &req.responses[0];
    assert_eq!(res.ctx.as_ref().unwrap().http_status_code, 200);
    assert_eq!(res.response_latencies.len(), 1);
    assert_eq!(res.ends.len(), 1);
    // ends
    let ends = &res.ends[0];
    assert_eq!(ends.streams.len(), 1);
    // streams
    let stream = &ends.streams[0];
    assert_eq!(stream.bytes_sent, 5);
    assert_eq!(stream.frames_sent, 1);
}

#[test]
fn telemetry_report_errors_are_ignored() {}

