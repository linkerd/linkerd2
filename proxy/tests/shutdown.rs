#![deny(warnings)]
mod support;
use self::support::*;

#[test]
fn h2_goaways_connections() {
    let _ = env_logger::try_init();

    let (shdn, rx) = shutdown_signal();

    let srv = server::http2().route("/", "hello").run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .shutdown_signal(rx)
        .run();
    let client = client::http2(proxy.inbound, "shutdown.test.svc.cluster.local");

    assert_eq!(client.get("/"), "hello");

    shdn.signal();

    client.wait_for_closed();
}

#[test]
fn h2_exercise_goaways_connections() {
    let _ = env_logger::try_init();

    const RESPONSE_SIZE: usize = 1024 * 16;
    const NUM_REQUESTS: usize = 50;

    let (shdn, rx) = shutdown_signal();

    let body = Bytes::from(vec![b'1'; RESPONSE_SIZE]);
    let srv = server::http2()
        .route_fn("/", move |_req| {
            Response::builder()
                .body(body.clone())
                .unwrap()
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .shutdown_signal(rx)
        .run();
    let client = client::http2(proxy.inbound, "shutdown.test.svc.cluster.local");

    let reqs = (0..NUM_REQUESTS)
        .into_iter()
        .map(|_| {
            client.request_async(
                client.request_builder("/").method("GET")
            )
        })
        .collect::<Vec<_>>();

    // Wait to get all responses (but not bodies)
    let resps = future::join_all(reqs)
        .wait()
        .expect("reqs");

    // Trigger a shutdown while bodies are still in progress.
    shdn.signal();

    let bodies = resps
        .into_iter()
        .map(|resp| {
            resp
                .into_body()
                .concat2()
                // Make sure the bodies weren't cut off
                .map(|buf| assert_eq!(buf.len(), RESPONSE_SIZE))
        })
        .collect::<Vec<_>>();

    // See that the proxy gives us all the bodies.
    future::join_all(bodies)
        .wait()
        .expect("bodies");

    client.wait_for_closed();
}

#[test]
fn http1_closes_idle_connections() {
    use std::cell::RefCell;
    let _ = env_logger::try_init();

    let (shdn, rx) = shutdown_signal();

    const RESPONSE_SIZE: usize = 1024 * 16;
    let body = Bytes::from(vec![b'1'; RESPONSE_SIZE]);

    let shdn = RefCell::new(Some(shdn));
    let srv = server::http1()
        .route_fn("/", move |_req| {
            // Trigger a shutdown signal while the request is made
            // but a response isn't returned yet.
            shdn.borrow_mut()
                .take()
                .expect("only 1 request")
                .signal();
            Response::builder()
                .body(body.clone())
                .unwrap()
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .shutdown_signal(rx)
        .run();
    let client = client::http1(proxy.inbound, "shutdown.test.svc.cluster.local");

    let res_body = client.get("/");
    assert_eq!(res_body.len(), RESPONSE_SIZE);

    client.wait_for_closed();
}

#[test]
fn tcp_waits_for_proxies_to_close() {
    let _ = env_logger::try_init();

    let (shdn, rx) = shutdown_signal();
    let msg1 = "custom tcp hello";
    let msg2 = "custom tcp bye";

    let srv = server::tcp()
        // Trigger a shutdown while TCP stream is busy
        .accept_fut(move |sock| {
            shdn.signal();
            tokio_io::io::read(sock, vec![0; 256])
                .and_then(move |(sock, vec, n)| {
                    assert_eq!(&vec[..n], msg1.as_bytes());

                    tokio_io::io::write_all(sock, msg2.as_bytes())
                })
                .map(|_| ())
                .map_err(|e| panic!("tcp server error: {}", e))
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .shutdown_signal(rx)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();

    tcp_client.write(msg1);
    assert_eq!(tcp_client.read(), msg2.as_bytes());
}

