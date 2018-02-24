mod support;
use self::support::*;

#[test]
fn outbound_http1() {
    let _ = env_logger::try_init();

    let srv = server::http1().route("/", "hello h1").run();
    let ctrl = controller::new()
        .destination("transparency.test.svc.cluster.local", srv.addr)
        .run();
    let proxy = proxy::new().controller(ctrl).outbound(srv).run();
    let client = client::http1(proxy.outbound, "transparency.test.svc.cluster.local");

    assert_eq!(client.get("/"), "hello h1");
}

#[test]
fn inbound_http1() {
    let _ = env_logger::try_init();

    let srv = server::http1().route("/", "hello h1").run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();
    let client = client::http1(proxy.inbound, "transparency.test.svc.cluster.local");

    assert_eq!(client.get("/"), "hello h1");
}

#[test]
fn http1_connect_not_supported() {
    let _ = env_logger::try_init();

    let srv = server::tcp()
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();
    tcp_client.write("CONNECT foo.bar:443 HTTP/1.1\r\nHost: foo.bar:443\r\n\r\n");

    let expected = "HTTP/1.1 502 Bad Gateway\r\n";
    assert_eq!(s(&tcp_client.read()[..expected.len()]), expected);
}

#[test]
fn http1_removes_connection_headers() {
    let _ = env_logger::try_init();

    let srv = server::http1()
        .route_fn("/", |req| {
            assert!(!req.headers().contains_key("x-foo-bar"));
            Response::builder()
                .header("x-server-quux", "lorem ipsum")
                .header("connection", "close, x-server-quux")
                .body("".into())
                .unwrap()
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();
    let client = client::http1(proxy.inbound, "transparency.test.svc.cluster.local");

    let res = client.request(client.request_builder("/")
        .header("x-foo-bar", "baz")
        .header("connection", "x-foo-bar, close"));

    assert_eq!(res.status(), http::StatusCode::OK);
    assert!(!res.headers().contains_key("x-server-quux"));
}

#[test]
fn http10_with_host() {
    let _ = env_logger::try_init();

    let host = "transparency.test.svc.cluster.local";
    let srv = server::http1()
        .route_fn("/", move |req| {
            assert_eq!(req.version(), http::Version::HTTP_10);
            assert_eq!(req.headers().get("host").unwrap(), host);
            Response::builder()
                .version(http::Version::HTTP_10)
                .body("".into())
                .unwrap()
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();
    let client = client::http1(proxy.inbound, host);

    let res = client.request(client.request_builder("/")
        .version(http::Version::HTTP_10)
        .header("host", host));

    assert_eq!(res.status(), http::StatusCode::OK);
    assert_eq!(res.version(), http::Version::HTTP_10);
}

#[test]
fn http10_without_host() {
    let _ = env_logger::try_init();

    let srv = server::http1()
        .route_fn("/", move |req| {
            assert_eq!(req.version(), http::Version::HTTP_10);
            Response::builder()
                .version(http::Version::HTTP_10)
                .body("".into())
                .unwrap()
        })
        .run();
    let ctrl = controller::new()
        .destination(&srv.addr.to_string(), srv.addr)
        .run();
    let proxy = proxy::new()
        .controller(ctrl)
        .outbound(srv)
        .run();
    let client = client::http1(proxy.outbound, "foo.bar");

    let res = client.request(client.request_builder("/")
        .version(http::Version::HTTP_10)
        .header("host", ""));

    assert_eq!(res.status(), http::StatusCode::OK);
    assert_eq!(res.version(), http::Version::HTTP_10);
}

#[test]
fn http11_absolute_uri_differs_from_host() {
    let _ = env_logger::try_init();

    let host = "transparency.test.svc.cluster.local";
    let srv = server::http1()
        .route_fn("/", move |req| {
            assert_eq!(req.version(), http::Version::HTTP_11);
            assert_eq!(req.headers().get("host").unwrap(), host);
            Response::builder()
                .body("".into())
                .unwrap()
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();
    let client = client::http1_absolute_uris(proxy.inbound, host);

    let res = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "foo.bar"));

    assert_eq!(res.status(), http::StatusCode::OK);
    assert_eq!(res.version(), http::Version::HTTP_11);
}

#[test]
fn outbound_tcp() {
    let _ = env_logger::try_init();

    let msg1 = "custom tcp hello";
    let msg2 = "custom tcp bye";

    let srv = server::tcp()
        .accept(move |read| {
            assert_eq!(read, msg1.as_bytes());
            msg2
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .outbound(srv)
        .run();

    let client = client::tcp(proxy.outbound);

    let tcp_client = client.connect();

    tcp_client.write(msg1);
    assert_eq!(tcp_client.read(), msg2.as_bytes());
}

#[test]
fn inbound_tcp() {
    let _ = env_logger::try_init();

    let msg1 = "custom tcp hello";
    let msg2 = "custom tcp bye";

    let srv = server::tcp()
        .accept(move |read| {
            assert_eq!(read, msg1.as_bytes());
            msg2
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();

    tcp_client.write(msg1);
    assert_eq!(tcp_client.read(), msg2.as_bytes());
}

#[test]
fn tcp_with_no_orig_dst() {
    let _ = env_logger::try_init();

    let srv = server::tcp()
        .accept(move |_| "don't read me")
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();

    // no outbound configured for proxy
    let client = client::tcp(proxy.outbound);

    let tcp_client = client.connect();
    tcp_client.write("custom tcp hello");

    let read = tcp_client
        .try_read()
        // This read might be an error, or an empty vec
        .unwrap_or_else(|_| Vec::new());
    assert_eq!(read, b"");
}

#[test]
fn http11_upgrade_not_supported() {
    let _ = env_logger::try_init();

    // our h1 proxy will strip the Connection header
    // and headers it mentions
    let msg1 = "\
        GET /chat HTTP/1.1\r\n\
        Host: foo.bar\r\n\
        Connection: Upgrade\r\n\
        Upgrade: websocket\r\n\
        \r\n\
        ";

    // but let's pretend the server tries to upgrade
    // anyways
    let msg2 = "\
        HTTP/1.1 101 Switching Protocols\r\n\
        Upgrade: websocket\r\n\
        Connection: Upgrade\r\n\
        \r\n\
        ";

    let srv = server::tcp()
        .accept(move |read| {
            let head = s(&read);
            assert!(!head.contains("Upgrade: websocket"));
            msg2
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();

    tcp_client.write(msg1);

    let expected = "HTTP/1.1 500 ";
    assert_eq!(s(&tcp_client.read()[..expected.len()]), expected);
}

#[test]
fn http1_get_doesnt_add_transfer_encoding() {
    let _ = env_logger::try_init();

    let srv = server::http1()
        .route_fn("/", |req| {
            assert!(!req.headers().contains_key("transfer-encoding"));
            Response::new("hello h1".into())
        })
        .run();
    let ctrl = controller::new().run();
    let proxy = proxy::new()
        .controller(ctrl)
        .inbound(srv)
        .run();
    let client = client::http1(proxy.inbound, "transparency.test.svc.cluster.local");
    assert_eq!(client.get("/"), "hello h1");
}
