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
    time::{Duration, Instant, SystemTime},
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

    let request_open_at = Instant::now();
    let response_open_at = request_open_at + Duration::from_millis(300);
    let end = event::StreamResponseEnd {
        grpc_status: None,
        request_open_at,
        response_open_at,
        response_end_at: response_open_at,
        bytes_sent: 0,
        frames_sent: 0,
    };

    let (mut r, _) = metrics::new(&process, Duration::from_secs(1000));
    b.iter(|| r.record_event(&Event::StreamResponseEnd(rsp.clone(), end.clone())));
}

#[bench]
fn record_one_conn_request(b: &mut Bencher) {
    let process = process();
    let proxy = ctx::Proxy::outbound(&process);
    let server = server(&proxy);

    let client = client(&proxy, vec![
        ("service", "draymond"),
        ("deployment", "durant"),
        ("pod", "klay"),
    ]);

    let (req, rsp) = request("http://buoyant.io", &server, &client, 1);

    let server_transport = Arc::new(ctx::transport::Ctx::Server(server));
    let client_transport = Arc::new(ctx::transport::Ctx::Client(client));

    let request_open_at = Instant::now();
    let request_end_at = request_open_at + Duration::from_millis(10);
    let response_open_at = request_open_at + Duration::from_millis(300);
    let response_end_at = response_open_at;

    use Event::*;
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
            response_end_at,
            bytes_sent: 0,
            frames_sent: 0,
        }),

        TransportClose(server_transport.clone(), event::TransportClose {
            clean: true,
            duration: Duration::from_secs(30_000),
            rx_bytes: 4321,
            tx_bytes: 4321,
        }),
        TransportClose(client_transport.clone(), event::TransportClose {
            clean: true,
            duration: Duration::from_secs(30_000),
            rx_bytes: 4321,
            tx_bytes: 4321,
        }),
    ];

    let (mut r, _) = metrics::new(&process, Duration::from_secs(1000));
    b.iter(|| for e in &events { r.record_event(e); });
}

#[bench]
fn record_many_dsts(b: &mut Bencher) {
    let process = process();
    let proxy = ctx::Proxy::outbound(&process);
    let server = server(&proxy);
    let server_transport = Arc::new(ctx::transport::Ctx::Server(server.clone()));

    use Event::*;
    let mut events = Vec::new();
    events.push(TransportOpen(server_transport.clone()));

    let request_open_at = Instant::now();
    let request_end_at = request_open_at + Duration::from_millis(10);
    let response_open_at = request_open_at + Duration::from_millis(300);
    let response_end_at = response_open_at;

    for n in 0..REQUESTS {
        let client = client(&proxy, vec![
            ("service".into(), format!("svc{}", n)),
            ("deployment".into(), format!("dep{}", n)),
            ("pod".into(), format!("pod{}", n)),
        ]);
        let uri = format!("http://test{}.local", n);
        let (req, rsp) = request(&uri, &server, &client, 1);
        let client_transport = Arc::new(ctx::transport::Ctx::Client(client));

        events.push(TransportOpen(client_transport.clone()));

        events.push(StreamRequestOpen(req.clone()));
        events.push(StreamRequestEnd(req.clone(), event::StreamRequestEnd {
            request_open_at,
            request_end_at,
        }));

        events.push(StreamResponseOpen(rsp.clone(), event::StreamResponseOpen {
            request_open_at,
            response_open_at,
        }));
        events.push(StreamResponseEnd(rsp.clone(), event::StreamResponseEnd {
            grpc_status: None,
            request_open_at,
            response_open_at,
            response_end_at,
            bytes_sent: 0,
            frames_sent: 0,
        }));

        events.push(TransportClose(client_transport.clone(), event::TransportClose {
            clean: true,
            duration: Duration::from_secs(30_000),
            rx_bytes: 4321,
            tx_bytes: 4321,
        }));
    }

    events.push(TransportClose(server_transport.clone(), event::TransportClose {
        clean: true,
        duration: Duration::from_secs(30_000),
        rx_bytes: 4321,
        tx_bytes: 4321,
    }));

    let (mut r, _) = metrics::new(&process, Duration::from_secs(1000));
    b.iter(|| for e in &events { r.record_event(e); });
}
