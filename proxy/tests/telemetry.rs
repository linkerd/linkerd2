#![deny(warnings)]
#[macro_use]
extern crate log;
extern crate regex;
extern crate flate2;

#[macro_use]
mod support;
use self::support::*;
use support::bytes::IntoBuf;
use std::io::Read;

macro_rules! assert_contains {
    ($scrape:expr, $contains:expr) => {
        assert_eventually!($scrape.contains($contains), "metrics scrape:\n{:8}\ndid not contain:\n{:8}", $scrape, $contains)
    }
}

struct Fixture {
    client: client::Client,
    metrics: client::Client,
    proxy: proxy::Listening,
}

struct TcpFixture {
    client: tcp::TcpClient,
    metrics: client::Client,
    proxy: proxy::Listening,
}

impl Fixture {
    fn inbound() -> Self {
        info!("running test server");
        Fixture::inbound_with_server(server::new()
            .route("/", "hello")
            .run())
    }

    fn outbound() -> Self {
        info!("running test server");
        Fixture::outbound_with_server(server::new()
            .route("/", "hello")
            .run())
    }

    fn inbound_with_server(srv: server::Listening) -> Self {
        let proxy = proxy::new()
            .inbound(srv)
            .run();
        let metrics = client::http1(proxy.metrics, "localhost");

        let client = client::new(
            proxy.inbound,
            "tele.test.svc.cluster.local"
        );
        Fixture { client, metrics, proxy }
    }

    fn outbound_with_server(srv: server::Listening) -> Self {
        let ctrl = controller::new()
            .destination_and_close("tele.test.svc.cluster.local", srv.addr)
            .run();
        let proxy = proxy::new()
            .controller(ctrl)
            .outbound(srv)
            .run();
        let metrics = client::http1(proxy.metrics, "localhost");

        let client = client::new(
            proxy.outbound,
            "tele.test.svc.cluster.local"
        );
        Fixture { client, metrics, proxy }
    }
}

impl TcpFixture {
    const HELLO_MSG: &'static str = "custom tcp hello";
    const BYE_MSG: &'static str = "custom tcp bye";

    fn server() -> server::Listening {
        server::tcp()
            .accept(move |read| {
                assert_eq!(read, Self::HELLO_MSG.as_bytes());
                TcpFixture::BYE_MSG
            })
            .accept(move |read| {
                assert_eq!(read, Self::HELLO_MSG.as_bytes());
                TcpFixture::BYE_MSG
            })
            .run()
    }

    fn inbound() -> Self {
        let proxy = proxy::new()
            .inbound(TcpFixture::server())
            .run();

        let client = client::tcp(proxy.inbound);
        let metrics = client::http1(proxy.metrics, "localhost");
        TcpFixture { client, metrics, proxy }
    }

    fn outbound() -> Self {
        let proxy = proxy::new()
            .outbound(TcpFixture::server())
            .run();

        let client = client::tcp(proxy.outbound);
        let metrics = client::http1(proxy.metrics, "localhost");
        TcpFixture { client, metrics, proxy }
    }
}

#[test]
fn metrics_endpoint_inbound_request_count() {
    let _ = env_logger::try_init();
    let Fixture { client, metrics, proxy: _proxy } = Fixture::inbound();

    // prior to seeing any requests, request count should be empty.
    assert!(!metrics.get("/metrics")
        .contains("request_total{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\"}"));

    info!("client.get(/)");
    assert_eq!(client.get("/"), "hello");

    // after seeing a request, the request count should be 1.
    assert_contains!(metrics.get("/metrics"), "request_total{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\"} 1");

}

#[test]
fn metrics_endpoint_outbound_request_count() {
    let _ = env_logger::try_init();
    let Fixture { client, metrics, proxy: _proxy } = Fixture::outbound();

    // prior to seeing any requests, request count should be empty.
    assert!(!metrics.get("/metrics")
        .contains("request_total{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\"}"));

    info!("client.get(/)");
    assert_eq!(client.get("/"), "hello");

    // after seeing a request, the request count should be 1.
    assert_contains!(metrics.get("/metrics"), "request_total{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\"} 1");

}

mod response_classification {
    use super::support::*;
    use super::Fixture;

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


    fn expected_metric(status: &http::StatusCode, direction: &str, tls: &str) -> String {
        format!(
            "response_total{{authority=\"tele.test.svc.cluster.local\",direction=\"{}\",tls=\"{}\",classification=\"{}\",status_code=\"{}\"}} 1",
            direction,
            tls,
            if status.is_server_error() { "failure" } else { "success" },
            status.as_u16(),
        )
    }

    fn make_test_server() -> server::Listening {
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
            .run()
    }

    #[test]
    fn inbound_http() {
        let _ = env_logger::try_init();
        let Fixture { client, metrics, proxy: _proxy } =
            Fixture::inbound_with_server(make_test_server());

        for (i, status) in STATUSES.iter().enumerate() {
            let request = client.request(
                client.request_builder("/")
                    .header(REQ_STATUS_HEADER, status.as_str())
                    .method("GET")
            );
            assert_eq!(&request.status(), status);

            for status in &STATUSES[0..i] {
                // assert that the current status code is incremented, *and* that
                // all previous requests are *not* incremented.
                assert_contains!(metrics.get("/metrics"), &expected_metric(status, "inbound", "disabled"))
            }
        }
    }

    #[test]
    fn outbound_http() {
        let _ = env_logger::try_init();
        let Fixture { client, metrics, proxy: _proxy } =
            Fixture::outbound_with_server(make_test_server());

        for (i, status) in STATUSES.iter().enumerate() {
            let request = client.request(
                client.request_builder("/")
                    .header(REQ_STATUS_HEADER, status.as_str())
                    .method("GET")
            );
            assert_eq!(&request.status(), status);

            for status in &STATUSES[0..i] {
                // assert that the current status code is incremented, *and* that
                // all previous requests are *not* incremented.
                assert_contains!(metrics.get("/metrics"), &expected_metric(status, "outbound", "no_identity"))
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

    let Fixture { client, metrics, proxy: _proxy } =
        Fixture::inbound_with_server(srv);

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    // assert the >=1000ms bucket is incremented by our request with 500ms
    // extra latency.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\",le=\"1000\"} 1");
    // the histogram's count should be 1.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\"} 1");
    // TODO: we're not going to make any assertions about the
    // response_latency_ms_sum stat, since its granularity depends on the actual
    // observed latencies, which may vary a bit. we could make more reliable
    // assertions about that stat if we were using a mock timer, though, as the
    // observed latency values would be predictable.

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");

    // request with 40ms extra latency should fall into the 50ms bucket.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\",le=\"50\"} 1");
    // 1000ms bucket should be incremented as well, since it counts *all*
    // bservations less than or equal to 1000ms, even if they also increment
    // other buckets.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\",le=\"1000\"} 2");
    // the histogram's total count should be 2.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\"} 2");

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");

    // request with 40ms extra latency should fall into the 50ms bucket.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\",le=\"50\"} 2");
    // 1000ms bucket should be incremented as well.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\",le=\"1000\"} 3");
    // the histogram's total count should be 3.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\"} 3");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    // 50ms bucket should be un-changed by the request with 500ms latency.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\",le=\"50\"} 2");
    // 1000ms bucket should be incremented.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\",le=\"1000\"} 4");
    // the histogram's total count should be 4.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\"} 4");
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

    let Fixture { client, metrics, proxy: _proxy } =
        Fixture::outbound_with_server(srv);

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    // assert the >=1000ms bucket is incremented by our request with 500ms
    // extra latency.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\",le=\"1000\"} 1");
    // the histogram's count should be 1.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
    // TODO: we're not going to make any assertions about the
    // response_latency_ms_sum stat, since its granularity depends on the actual
    // observed latencies, which may vary a bit. we could make more reliable
    // assertions about that stat if we were using a mock timer, though, as the
    // observed latency values would be predictable.

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");

    // request with 40ms extra latency should fall into the 50ms bucket.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\",le=\"50\"} 1");
    // 1000ms bucket should be incremented as well, since it counts *all*
    // bservations less than or equal to 1000ms, even if they also increment
    // other buckets.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\",le=\"1000\"} 2");
    // the histogram's total count should be 2.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 2");

    info!("client.get(/hi)");
    assert_eq!(client.get("/hi"), "good morning");

    // request with 40ms extra latency should fall into the 50ms bucket.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\",le=\"50\"} 2");
    // 1000ms bucket should be incremented as well.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\",le=\"1000\"} 3");
    // the histogram's total count should be 3.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 3");

    info!("client.get(/hey)");
    assert_eq!(client.get("/hey"), "hello");

    // 50ms bucket should be un-changed by the request with 500ms latency.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\",le=\"50\"} 2");
    // 1000ms bucket should be incremented.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_bucket{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\",le=\"1000\"} 4");
    // the histogram's total count should be 4.
    assert_contains!(metrics.get("/metrics"),
        "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"outbound\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 4");
}

// Tests for destination labels provided by control plane service discovery.
mod outbound_dst_labels {
    use super::support::*;
    use super::Fixture;
    use controller::DstSender;

    fn fixture(dest: &str) -> (Fixture, SocketAddr, DstSender) {
        info!("running test server");
        let srv = server::new()
            .route("/", "hello")
            .run();

        let addr = srv.addr;

        let ctrl = controller::new();
        let dst_tx = ctrl.destination_tx(dest);

        let proxy = proxy::new()
            .controller(ctrl.run())
            .outbound(srv)
            .run();
        let metrics = client::http1(proxy.metrics, "localhost");

        let client = client::new(
            proxy.outbound,
            dest,
        );

        let f = Fixture { client, metrics, proxy };

        (f, addr, dst_tx)
    }

    #[test]
    fn multiple_addr_labels() {
        let _ = env_logger::try_init();
        let (Fixture { client, metrics, proxy: _proxy }, addr, dst_tx) =
            fixture("labeled.test.svc.cluster.local");

        {
            let mut labels = HashMap::new();
            labels.insert("addr_label1".to_owned(), "foo".to_owned());
            labels.insert("addr_label2".to_owned(), "bar".to_owned());
            dst_tx.send_labeled(addr, labels, HashMap::new());
        }

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        // We can't make more specific assertions about the metrics
        // besides asserting that both labels are present somewhere in the
        // scrape, because testing for whole metric lines would depend on
        // the order in which the labels occur, and we can't depend on hash
        // map ordering.
        assert_contains!(metrics.get("/metrics"), "dst_addr_label1=\"foo\"");
        assert_contains!(metrics.get("/metrics"), "dst_addr_label2=\"bar\"");
    }

    #[test]
    fn multiple_addrset_labels() {
        let _ = env_logger::try_init();
        let (Fixture { client, metrics, proxy: _proxy }, addr, dst_tx) =
            fixture("labeled.test.svc.cluster.local");

        {
            let mut labels = HashMap::new();
            labels.insert("set_label1".to_owned(), "foo".to_owned());
            labels.insert("set_label2".to_owned(), "bar".to_owned());
            dst_tx.send_labeled(addr, HashMap::new(), labels);
        }


        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        // We can't make more specific assertions about the metrics
        // besides asserting that both labels are present somewhere in the
        // scrape, because testing for whole metric lines would depend on
        // the order in which the labels occur, and we can't depend on hash
        // map ordering.
        assert_contains!(metrics.get("/metrics"), "dst_set_label1=\"foo\"");
        assert_contains!(metrics.get("/metrics"), "dst_set_label2=\"bar\"");
    }

    #[test]
    fn labeled_addr_and_addrset() {
        let _ = env_logger::try_init();
        let (Fixture { client, metrics, proxy: _proxy }, addr, dst_tx) =
            fixture("labeled.test.svc.cluster.local");

        {
            let mut alabels = HashMap::new();
            alabels.insert("addr_label".to_owned(), "foo".to_owned());
            let mut slabels = HashMap::new();
            slabels.insert("set_label".to_owned(), "bar".to_owned());
            dst_tx.send_labeled(addr, alabels, slabels);
        }

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        assert_contains!(metrics.get("/metrics"),
            "response_latency_ms_count{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"bar\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "request_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"bar\",tls=\"no_identity\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "response_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"bar\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
    }

    // Ignore this test on CI, as it may fail due to the reduced concurrency
    // on CI containers causing the proxy to see both label updates from
    // the mock controller before the first request has finished.
    // See https://github.com/runconduit/conduit/issues/751
    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn controller_updates_addr_labels() {
        let _ = env_logger::try_init();
                info!("running test server");

        let (Fixture { client, metrics, proxy: _proxy }, addr, dst_tx) =
            fixture("labeled.test.svc.cluster.local");

        {
            let mut alabels = HashMap::new();
            alabels.insert("addr_label".to_owned(), "foo".to_owned());
            let mut slabels = HashMap::new();
            slabels.insert("set_label".to_owned(), "unchanged".to_owned());
            dst_tx.send_labeled(addr, alabels, slabels);
        }

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        // the first request should be labeled with `dst_addr_label="foo"`
        assert_contains!(metrics.get("/metrics"),
            "response_latency_ms_count{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"unchanged\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "request_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"unchanged\",tls=\"no_identity\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "response_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"unchanged\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");

        {
            let mut alabels = HashMap::new();
            alabels.insert("addr_label".to_owned(), "bar".to_owned());
            let mut slabels = HashMap::new();
            slabels.insert("set_label".to_owned(), "unchanged".to_owned());
            dst_tx.send_labeled(addr, alabels, slabels);
        }

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        // the second request should increment stats labeled with `dst_addr_label="bar"`
        assert_contains!(metrics.get("/metrics"),
            "response_latency_ms_count{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"bar\",dst_set_label=\"unchanged\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "request_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"bar\",dst_set_label=\"unchanged\",tls=\"no_identity\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "response_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"bar\",dst_set_label=\"unchanged\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        // stats recorded from the first request should still be present.
        assert_contains!(metrics.get("/metrics"),
            "response_latency_ms_count{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"unchanged\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "request_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"unchanged\",tls=\"no_identity\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "response_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_addr_label=\"foo\",dst_set_label=\"unchanged\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
    }

    // Ignore this test on CI, as it may fail due to the reduced concurrency
    // on CI containers causing the proxy to see both label updates from
    // the mock controller before the first request has finished.
    // See https://github.com/runconduit/conduit/issues/751
    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn controller_updates_set_labels() {
        let _ = env_logger::try_init();
                info!("running test server");
        let (Fixture { client, metrics, proxy: _proxy }, addr, dst_tx) =
            fixture("labeled.test.svc.cluster.local");

        {
            let alabels = HashMap::new();
            let mut slabels = HashMap::new();
            slabels.insert("set_label".to_owned(), "foo".to_owned());
            dst_tx.send_labeled(addr, alabels, slabels);
        }

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        // the first request should be labeled with `dst_addr_label="foo"`
        assert_contains!(metrics.get("/metrics"),
            "response_latency_ms_count{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"foo\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "request_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"foo\",tls=\"no_identity\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "response_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"foo\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");

        {
            let alabels = HashMap::new();
            let mut slabels = HashMap::new();
            slabels.insert("set_label".to_owned(), "bar".to_owned());
            dst_tx.send_labeled(addr, alabels, slabels);
        }

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        // the second request should increment stats labeled with `dst_addr_label="bar"`
        assert_contains!(metrics.get("/metrics"),
            "response_latency_ms_count{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"bar\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "request_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"bar\",tls=\"no_identity\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "response_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"bar\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        // stats recorded from the first request should still be present.
        assert_contains!(metrics.get("/metrics"),
            "response_latency_ms_count{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"foo\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "request_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"foo\",tls=\"no_identity\"} 1");
        assert_contains!(metrics.get("/metrics"),
            "response_total{authority=\"labeled.test.svc.cluster.local\",direction=\"outbound\",dst_set_label=\"foo\",tls=\"no_identity\",classification=\"success\",status_code=\"200\"} 1");
    }
}

#[test]
fn metrics_have_no_double_commas() {
    // Test for regressions to runconduit/conduit#600.
    let _ = env_logger::try_init();

    info!("running test server");
    let inbound_srv = server::new().route("/hey", "hello").run();
    let outbound_srv = server::new().route("/hey", "hello").run();

    let ctrl = controller::new()
        .destination_and_close("tele.test.svc.cluster.local", outbound_srv.addr)
        .run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(inbound_srv)
        .outbound(outbound_srv)
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


#[test]
fn metrics_has_start_time() {
    let Fixture { metrics, proxy: _proxy, .. } = Fixture::inbound();
    let uptime_regex = regex::Regex::new(r"process_start_time_seconds \d+")
        .expect("compiling regex shouldn't fail");
    assert_eventually!(
        uptime_regex.find(&metrics.get("/metrics")).is_some()
    )
}

mod transport {
    use super::support::*;
    use super::*;

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn inbound_http_accept() {
        let _ = env_logger::try_init();
        let Fixture { client, metrics, proxy } = Fixture::inbound();

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"inbound\",peer=\"src\",tls=\"disabled\"} 1"
        );
        // drop the client to force the connection to close.
        drop(client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_close_total{direction=\"inbound\",peer=\"src\",tls=\"disabled\",classification=\"success\"} 1"
        );

        // create a new client to force a new connection
        let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"inbound\",peer=\"src\",tls=\"disabled\"} 2"
        );
        // drop the client to force the connection to close.
        drop(client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_close_total{direction=\"inbound\",peer=\"src\",tls=\"disabled\",classification=\"success\"} 2"
        );
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn inbound_http_connect() {
        let _ = env_logger::try_init();
        let Fixture { client, metrics, proxy } = Fixture::inbound();

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"inbound\",peer=\"dst\",tls=\"no_identity\"} 1");

        // create a new client to force a new connection
        let client = client::new(proxy.inbound, "tele.test.svc.cluster.local");

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        // server connection should be pooled
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"inbound\",peer=\"dst\",tls=\"no_identity\"} 1");
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_http_accept() {
        let _ = env_logger::try_init();
        let Fixture { client, metrics, proxy } = Fixture::outbound();

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 1"
        );
        // drop the client to force the connection to close.
        drop(client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_close_total{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\",classification=\"success\"} 1"
        );

        // create a new client to force a new connection
        let client = client::new(proxy.outbound, "tele.test.svc.cluster.local");

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 2"
        );
        // drop the client to force the connection to close.
        drop(client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_close_total{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\",classification=\"success\"} 2"
        );
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_http_connect() {
        let _ = env_logger::try_init();
        let Fixture { client, metrics, proxy } = Fixture::outbound();

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"outbound\",peer=\"dst\",tls=\"no_identity\"} 1");

        // create a new client to force a new connection
        let client2 = client::new(proxy.outbound, "tele.test.svc.cluster.local");

        info!("client.get(/)");
        assert_eq!(client2.get("/"), "hello");
        // server connection should be pooled
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"outbound\",peer=\"dst\",tls=\"no_identity\"} 1");
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn inbound_tcp_connect() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::inbound();

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"inbound\",peer=\"dst\",tls=\"no_identity\"} 1");
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn inbound_tcp_accept() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::inbound();

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());

        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"inbound\",peer=\"src\",tls=\"disabled\"} 1");

        drop(tcp_client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_close_total{direction=\"inbound\",peer=\"src\",tls=\"disabled\",classification=\"success\"} 1");

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());

        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"inbound\",peer=\"src\",tls=\"disabled\"} 2");
        drop(tcp_client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_close_total{direction=\"inbound\",peer=\"src\",tls=\"disabled\",classification=\"success\"} 2");
    }

    // https://github.com/runconduit/conduit/issues/831
    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn inbound_tcp_duration() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::inbound();

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        drop(tcp_client);
        // TODO: make assertions about buckets
        let out = metrics.get("/metrics");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"inbound\",peer=\"src\",tls=\"disabled\",classification=\"success\"} 1");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"inbound\",peer=\"dst\",tls=\"no_identity\",classification=\"success\"} 1");

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        let out = metrics.get("/metrics");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"inbound\",peer=\"src\",tls=\"disabled\",classification=\"success\"} 1");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"inbound\",peer=\"dst\",tls=\"no_identity\",classification=\"success\"} 1");

        drop(tcp_client);
        let out = metrics.get("/metrics");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"inbound\",peer=\"src\",tls=\"disabled\",classification=\"success\"} 2");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"inbound\",peer=\"dst\",tls=\"no_identity\",classification=\"success\"} 2");
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn inbound_tcp_write_bytes_total() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::inbound();
        let src_expected = format!(
            "tcp_write_bytes_total{{direction=\"inbound\",peer=\"src\",tls=\"disabled\"}} {}",
            TcpFixture::BYE_MSG.len()
        );
        let dst_expected = format!(
            "tcp_write_bytes_total{{direction=\"inbound\",peer=\"dst\",tls=\"no_identity\"}} {}",
            TcpFixture::HELLO_MSG.len()
        );

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        drop(tcp_client);

        let out = metrics.get("/metrics");
        assert_contains!(out, &src_expected);
        assert_contains!(out, &dst_expected);
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn inbound_tcp_read_bytes_total() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::inbound();
        let src_expected = format!(
            "tcp_read_bytes_total{{direction=\"inbound\",peer=\"src\",tls=\"disabled\"}} {}",
            TcpFixture::HELLO_MSG.len()
        );
        let dst_expected = format!(
            "tcp_read_bytes_total{{direction=\"inbound\",peer=\"dst\",tls=\"no_identity\"}} {}",
            TcpFixture::BYE_MSG.len()
        );

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        drop(tcp_client);

        let out = metrics.get("/metrics");
        assert_contains!(out, &src_expected);
        assert_contains!(out, &dst_expected);    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_tcp_connect() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::outbound();

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"outbound\",peer=\"dst\",tls=\"no_identity\"} 1");
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_tcp_accept() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::outbound();

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());

        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 1");

        drop(tcp_client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_close_total{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\",classification=\"success\"} 1");

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());

        assert_contains!(metrics.get("/metrics"),
            "tcp_open_total{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 2");
        drop(tcp_client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_close_total{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\",classification=\"success\"} 2");
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_tcp_duration() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::outbound();

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        drop(tcp_client);
        // TODO: make assertions about buckets
        let out = metrics.get("/metrics");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\",classification=\"success\"} 1");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"outbound\",peer=\"dst\",tls=\"no_identity\",classification=\"success\"} 1");

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        let out = metrics.get("/metrics");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\",classification=\"success\"} 1");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"outbound\",peer=\"dst\",tls=\"no_identity\",classification=\"success\"} 1");

        drop(tcp_client);
        let out = metrics.get("/metrics");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\",classification=\"success\"} 2");
        assert_contains!(out,
            "tcp_connection_duration_ms_count{direction=\"outbound\",peer=\"dst\",tls=\"no_identity\",classification=\"success\"} 2");
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_tcp_write_bytes_total() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::outbound();
        let src_expected = format!(
            "tcp_write_bytes_total{{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"}} {}",
            TcpFixture::BYE_MSG.len()
        );
        let dst_expected = format!(
            "tcp_write_bytes_total{{direction=\"outbound\",peer=\"dst\",tls=\"no_identity\"}} {}",
            TcpFixture::HELLO_MSG.len()
        );

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        drop(tcp_client);

        let out = metrics.get("/metrics");
        assert_contains!(out, &src_expected);
        assert_contains!(out, &dst_expected);
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_tcp_read_bytes_total() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::outbound();
        let src_expected = format!(
            "tcp_read_bytes_total{{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"}} {}",
            TcpFixture::HELLO_MSG.len()
        );
        let dst_expected = format!(
            "tcp_read_bytes_total{{direction=\"outbound\",peer=\"dst\",tls=\"no_identity\"}} {}",
            TcpFixture::BYE_MSG.len()
        );

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        drop(tcp_client);

        let out = metrics.get("/metrics");
        assert_contains!(out, &src_expected);
        assert_contains!(out, &dst_expected);
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_tcp_open_connections() {
        let _ = env_logger::try_init();
        let TcpFixture { client, metrics, proxy: _proxy } =
            TcpFixture::outbound();

        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_connections{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 1");
        drop(tcp_client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_connections{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 0");
        let tcp_client = client.connect();

        tcp_client.write(TcpFixture::HELLO_MSG);
        assert_eq!(tcp_client.read(), TcpFixture::BYE_MSG.as_bytes());
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_connections{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 1");

        drop(tcp_client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_connections{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 0");
    }

    #[test]
    #[cfg_attr(not(feature = "flaky_tests"), ignore)]
    fn outbound_http_tcp_open_connections() {
        let _ = env_logger::try_init();
        let Fixture { client, metrics, proxy } =
            Fixture::outbound();

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");

        assert_contains!(metrics.get("/metrics"),
            "tcp_open_connections{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 1");
        drop(client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_connections{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 0");

        // create a new client to force a new connection
        let client = client::new(proxy.outbound, "tele.test.svc.cluster.local");

        info!("client.get(/)");
        assert_eq!(client.get("/"), "hello");
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_connections{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 1");

        drop(client);
        assert_contains!(metrics.get("/metrics"),
            "tcp_open_connections{direction=\"outbound\",peer=\"src\",tls=\"internal_traffic\"} 0");
    }
}

// https://github.com/runconduit/conduit/issues/613
#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn metrics_compression() {
    let _ = env_logger::try_init();

    let Fixture { client, metrics, proxy: _proxy } = Fixture::inbound();

    let do_scrape = |encoding: &str| {
        let resp = metrics.request(
            metrics.request_builder("/metrics")
                .method("GET")
                .header("Accept-Encoding", encoding)
        );

        {
            // create a new scope so we can release our borrow on `resp` before
            // getting the body
            let content_encoding = resp.headers()
                .get("content-encoding")
                .as_ref()
                .map(|val| val
                    .to_str()
                    .expect("content-encoding value should be ascii")
                );
            assert_eq!(content_encoding, Some("gzip"),
                "unexpected Content-Encoding {:?} (requested Accept-Encoding: {})", content_encoding, encoding);
        }

        let body = resp.into_body()
            .concat2()
            .wait()
            .expect("response body concat");
        let mut decoder = flate2::read::GzDecoder::new(body.into_buf());
        let mut scrape = String::new();
        decoder.read_to_string(&mut scrape)
            .expect(&format!(
                "decode gzip (requested Accept-Encoding: {})",
                encoding
            ));
        scrape
    };

    let encodings = &[
        "gzip",
        "deflate, gzip",
        "gzip,deflate",
        "brotli,gzip,deflate"
    ];

    info!("client.get(/)");
    assert_eq!(client.get("/"), "hello");

    for &encoding in encodings {
        assert_contains!(do_scrape(encoding),
            "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\"} 1");
    }

    info!("client.get(/)");
    assert_eq!(client.get("/"), "hello");

    for &encoding in encodings {
        assert_contains!(do_scrape(encoding),
            "response_latency_ms_count{authority=\"tele.test.svc.cluster.local\",direction=\"inbound\",tls=\"disabled\",classification=\"success\",status_code=\"200\"} 2");
    }
}
