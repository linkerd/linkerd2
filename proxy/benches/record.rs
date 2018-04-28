#![feature(test)]
#![deny(warnings)]

extern crate conduit_proxy;
extern crate futures_watch;
extern crate http;
extern crate test;

use conduit_proxy::{
    ctx,
    telemetry::{
        event,
        metrics,
        Event,
    },
};
use std::{
    fmt,
    net::SocketAddr,
    sync::Arc,
    time::{Duration, SystemTime},
};
use test::Bencher;

const REQUESTS: usize = 100;

fn addr() -> SocketAddr {
    ([1, 2, 3, 4], 5678).into()
}

fn process() -> Arc<ctx::Process> {
    Arc::new(ctx::Process {
        scheduled_namespace: "test".into(),
        start_time: SystemTime::now(),
    })
}

fn server(proxy: &Arc<ctx::Proxy>) -> Arc<ctx::transport::Server> {
    ctx::transport::Server::new(&proxy, &addr(), &addr(), &Some(addr()))
}

fn client<L, S>(proxy: &Arc<ctx::Proxy>, labels: L) -> Arc<ctx::transport::Client>
where
    L: IntoIterator<Item=(S, S)>,
    S: fmt::Display,
{
    let (labels_watch, _store) = futures_watch::Watch::new(metrics::DstLabels::new(labels));
    ctx::transport::Client::new(&proxy, &addr(), Some(labels_watch))
}

fn request(
    uri: &str,
    server: &Arc<ctx::transport::Server>,
    client: &Arc<ctx::transport::Client>,
    id: usize
) -> (Arc<ctx::http::Request>, Arc<ctx::http::Response>) {
    let req = ctx::http::Request::new(
        &http::Request::get(uri).body(()).unwrap(),
        &server,
        &client,
        id,
    );
    let rsp = ctx::http::Response::new(
        &http::Response::builder().status(http::StatusCode::OK).body(()).unwrap(),
        &req,
    );
    (req, rsp)
}

#[bench]
fn record_response_end(b: &mut Bencher) {
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

    let (mut r, _) = metrics::new(&process);
    b.iter(|| r.record_event(&Event::StreamResponseEnd(rsp.clone(), end.clone())));
}

#[bench]
fn record_one_conn_many_reqs(b: &mut Bencher) {
    let process = process();
    let proxy = ctx::Proxy::outbound(&process);
    let server = server(&proxy);
    let server_transport = Arc::new(ctx::transport::Ctx::Server(server.clone()));

    let client = client(&proxy, vec![
        ("service", "draymond"),
        ("deployment", "durant"),
        ("pod", "klay"),
    ]);
    let client_transport = Arc::new(ctx::transport::Ctx::Client(client.clone()));

    let requests = (0..REQUESTS).map(|n| request("http://buoyant.io", &server, &client, n));

    let (mut r, _) = metrics::new(&process);
    b.iter(|| {
        use Event::*;

        r.record_event(&TransportOpen(server_transport.clone()));
        r.record_event(&TransportOpen(client_transport.clone()));

        for (req, rsp) in requests.clone() {
            r.record_event(&StreamRequestOpen(req.clone()));

            r.record_event(&StreamRequestEnd(req.clone(), event::StreamRequestEnd {
                since_request_open: Duration::from_millis(10),
            }));

            r.record_event(&StreamResponseOpen(rsp.clone(), event::StreamResponseOpen {
                since_request_open: Duration::from_millis(300),
            }));

            r.record_event(&StreamResponseEnd(rsp.clone(), event::StreamResponseEnd {
                grpc_status: None,
                since_request_open: Duration::from_millis(300),
                since_response_open: Duration::from_millis(0),
                bytes_sent: 0,
                frames_sent: 0,
            }));
        }

        r.record_event(&TransportClose(server_transport.clone(), event::TransportClose {
            clean: true,
            duration: Duration::from_secs(30_000),
            rx_bytes: 4321,
            tx_bytes: 4321,
        }));
        r.record_event(&TransportClose(client_transport.clone(), event::TransportClose {
            clean: true,
            duration: Duration::from_secs(30_000),
            rx_bytes: 4321,
            tx_bytes: 4321,
        }));
    });
}

#[bench]
fn record_many_dsts(b: &mut Bencher) {
    let process = process();
    let proxy = ctx::Proxy::outbound(&process);
    let server = server(&proxy);
    let server_transport = Arc::new(ctx::transport::Ctx::Server(server.clone()));

    let requests = (0..REQUESTS).map(|n| {
        let client = client(&proxy, vec![
            ("service".into(), format!("svc{}", n)),
            ("deployment".into(), format!("dep{}", n)),
            ("pod".into(), format!("pod{}", n)),
        ]);
        let client_transport = Arc::new(ctx::transport::Ctx::Client(client.clone()));
        let uri = format!("http://test{}.local", n);
        let (req, rsp) = request(&uri, &server, &client, 1);
        (client_transport, req, rsp)
    });

    let (mut r, _) = metrics::new(&process);
    b.iter(|| {
        use Event::*;

        r.record_event(&TransportOpen(server_transport.clone()));

        for (client_transport, req, rsp) in requests.clone() {
            r.record_event(&TransportOpen(client_transport.clone()));

            r.record_event(&StreamRequestOpen(req.clone()));

            r.record_event(&StreamRequestEnd(req.clone(), event::StreamRequestEnd {
                since_request_open: Duration::from_millis(10),
            }));

            r.record_event(&StreamResponseOpen(rsp.clone(), event::StreamResponseOpen {
                since_request_open: Duration::from_millis(300),
            }));

            r.record_event(&StreamResponseEnd(rsp.clone(), event::StreamResponseEnd {
                grpc_status: None,
                since_request_open: Duration::from_millis(300),
                since_response_open: Duration::from_millis(0),
                bytes_sent: 0,
                frames_sent: 0,
            }));

            r.record_event(&TransportClose(client_transport.clone(), event::TransportClose {
                clean: true,
                duration: Duration::from_secs(30_000),
                rx_bytes: 4321,
                tx_bytes: 4321,
            }));
        }

        r.record_event(&TransportClose(server_transport.clone(), event::TransportClose {
            clean: true,
            duration: Duration::from_secs(30_000),
            rx_bytes: 4321,
            tx_bytes: 4321,
        }));
    });
}
