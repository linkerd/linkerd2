#![deny(warnings)]
mod support;
use self::support::*;

#[test]
fn outbound_http1() {
    let _ = env_logger::try_init();

    let srv = server::http1().route("/", "hello h1").run();
    let ctrl = controller::new()
        .destination_and_close("transparency.test.svc.cluster.local", srv.addr)
        .run();
    let proxy = proxy::new().controller(ctrl).outbound(srv).run();
    let client = client::http1(proxy.outbound, "transparency.test.svc.cluster.local");

    assert_eq!(client.get("/"), "hello h1");
}

#[test]
fn inbound_http1() {
    let _ = env_logger::try_init();

    let srv = server::http1().route("/", "hello h1").run();
    let proxy = proxy::new()
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
    let proxy = proxy::new()
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
    let proxy = proxy::new()
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
    let proxy = proxy::new()
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
            assert!(!req.headers().contains_key("host"));
            assert_eq!(req.uri().to_string(), "/");
            Response::builder()
                .version(http::Version::HTTP_10)
                .body("".into())
                .unwrap()
        })
        .run();
    let proxy = proxy::new()
        .inbound(srv)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();

    tcp_client.write("\
        GET / HTTP/1.0\r\n\
        \r\n\
    ");

    let expected = "HTTP/1.0 200 OK\r\n";
    assert_eq!(s(&tcp_client.read()[..expected.len()]), expected);
}

#[test]
fn http11_absolute_uri_differs_from_host() {
    let _ = env_logger::try_init();

    // We shouldn't touch the URI or the Host, just pass directly as we got.
    let auth = "transparency.test.svc.cluster.local";
    let host = "foo.bar";
    let srv = server::http1()
        .route_fn("/", move |req| {
            assert_eq!(req.headers()["host"], host);
            assert_eq!(req.uri().to_string(), format!("http://{}/", auth));
            Response::new("".into())
        })
        .run();
    let proxy = proxy::new()
        .inbound(srv)
        .run();
    let client = client::http1_absolute_uris(proxy.inbound, auth);

    let res = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", host));

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
    let proxy = proxy::new()
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
    let proxy = proxy::new()
        .inbound(srv)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();

    tcp_client.write(msg1);
    assert_eq!(tcp_client.read(), msg2.as_bytes());
}

#[test]
fn tcp_server_first() {
    use std::sync::mpsc;

    let _ = env_logger::try_init();

    let msg1 = "custom tcp server starts";
    let msg2 = "custom tcp client second";

    let (tx, rx) = mpsc::channel();

    let srv = server::tcp()
        .accept_fut(move |sock| {
            tokio_io::io::write_all(sock, msg1.as_bytes())
                .and_then(move |(sock, _)| {
                    tokio_io::io::read(sock, vec![0; 512])
                })
                .map(move |(_sock, vec, n)| {
                    assert_eq!(&vec[..n], msg2.as_bytes());
                    tx.send(()).unwrap();
                })
                .map_err(|e| panic!("tcp server error: {}", e))
        })
        .run();
    let proxy = proxy::new()
        .disable_inbound_ports_protocol_detection(vec![srv.addr.port()])
        .inbound(srv)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();

    assert_eq!(tcp_client.read(), msg1.as_bytes());
    tcp_client.write(msg2);
    rx.recv_timeout(Duration::from_secs(5)).unwrap();
}

#[test]
fn tcp_with_no_orig_dst() {
    let _ = env_logger::try_init();

    let srv = server::tcp()
        .accept(move |_| "don't read me")
        .run();
    let proxy = proxy::new()
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
fn tcp_connections_close_if_client_closes() {
    use std::sync::mpsc;

    let _ = env_logger::try_init();

    let msg1 = "custom tcp hello";
    let msg2 = "custom tcp bye";

    let (tx, rx) = mpsc::channel();

    let srv = server::tcp()
        .accept_fut(move |sock| {
            tokio_io::io::read(sock, vec![0; 1024])
                .and_then(move |(sock, vec, n)| {
                    assert_eq!(&vec[..n], msg1.as_bytes());

                    tokio_io::io::write_all(sock, msg2.as_bytes())
                }).and_then(|(sock, _)| {
                    // lets read again, but we should get eof
                    tokio_io::io::read(sock, [0; 16])
                })
                .map(move |(_sock, _vec, n)| {
                    assert_eq!(n, 0);
                    tx.send(()).unwrap();
                })
                .map_err(|e| panic!("tcp server error: {}", e))
        })
        .run();
    let proxy = proxy::new()
        .inbound(srv)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();
    tcp_client.write(msg1);
    assert_eq!(tcp_client.read(), msg2.as_bytes());

    drop(tcp_client);

    // rx will be fulfilled when our tcp accept_fut sees
    // a socket disconnect, which is what we are testing for.
    // the timeout here is just to prevent this test from hanging
    rx.recv_timeout(Duration::from_secs(5)).unwrap();
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
    let proxy = proxy::new()
        .inbound(srv)
        .run();

    let client = client::tcp(proxy.inbound);

    let tcp_client = client.connect();

    tcp_client.write(msg1);

    let expected = "HTTP/1.1 500 ";
    assert_eq!(s(&tcp_client.read()[..expected.len()]), expected);
}

#[test]
fn http1_requests_without_body_doesnt_add_transfer_encoding() {
    let _ = env_logger::try_init();

    let srv = server::http1()
        .route_fn("/", |req| {
            let has_body_header = req.headers().contains_key("transfer-encoding")
                || req.headers().contains_key("content-length");
            let status = if  has_body_header {
                StatusCode::BAD_REQUEST
            } else {
                StatusCode::OK
            };
            let mut res = Response::new("".into());
            *res.status_mut() = status;
            res
        })
        .run();
    let proxy = proxy::new()
        .inbound(srv)
        .run();
    let client = client::http1(proxy.inbound, "transparency.test.svc.cluster.local");

    let methods = &[
        "GET",
        "POST",
        "PUT",
        "DELETE",
        "HEAD",
        "PATCH",
    ];

    for &method in methods {
        let resp = client.request(
            client
                .request_builder("/")
                .method(method)
        );

        assert_eq!(resp.status(), StatusCode::OK, "method={:?}", method);
    }
}

#[test]
fn http1_content_length_zero_is_preserved() {
    let _ = env_logger::try_init();

    let srv = server::http1()
        .route_fn("/", |req| {
            let status = if req.headers()["content-length"] == "0" {
                StatusCode::OK
            } else {
                StatusCode::BAD_REQUEST
            };
            Response::builder()
                .status(status)
                .header("content-length", "0")
                .body("".into())
                .unwrap()
        })
        .run();
    let proxy = proxy::new()
        .inbound(srv)
        .run();
    let client = client::http1(proxy.inbound, "transparency.test.svc.cluster.local");


    let methods = &[
        "GET",
        "POST",
        "PUT",
        "DELETE",
        "HEAD",
        "PATCH",
    ];

    for &method in methods {
        let resp = client.request(
            client
                .request_builder("/")
                .method(method)
                .header("content-length", "0")
        );

        assert_eq!(resp.status(), StatusCode::OK, "method={:?}", method);
        assert_eq!(resp.headers()["content-length"], "0", "method={:?}", method);
    }
}

#[test]
fn http1_bodyless_responses() {
    let _ = env_logger::try_init();

    let req_status_header = "x-test-status-requested";

    let srv = server::http1()
        .route_fn("/", move |req| {
            let status = req.headers()
                .get(req_status_header)
                .map(|val| {
                    val.to_str()
                        .expect("req_status_header should be ascii")
                        .parse::<u16>()
                        .expect("req_status_header should be numbers")
                })
                .unwrap_or(200);

            Response::builder()
                .status(status)
                .body("".into())
                .unwrap()
        })
        .run();
    let proxy = proxy::new()
        .inbound(srv)
        .run();
    let client = client::http1(proxy.inbound, "transparency.test.svc.cluster.local");

    // https://tools.ietf.org/html/rfc7230#section-3.3.3
    // > response to a HEAD request, any 1xx, 204, or 304 cannot contain a body

    //TODO: the proxy doesn't support CONNECT requests yet, but when we do,
    //they should be tested here as well. As RFC7230 says, a 2xx response to
    //a CONNECT request is not allowed to contain a body (but 4xx, 5xx can!).

    let resp = client.request(
        client
            .request_builder("/")
            .method("HEAD")
    );

    assert_eq!(resp.status(), StatusCode::OK);
    assert!(!resp.headers().contains_key("content-length"));
    assert!(!resp.headers().contains_key("transfer-encoding"));

    let statuses = &[
        //TODO: test some 1xx status codes.
        //The current test server doesn't support sending 1xx responses
        //easily. We could test this by making a new unit test with the
        //server being a TCP server, and write the response manually.
        StatusCode::NO_CONTENT, // 204
        StatusCode::NOT_MODIFIED, // 304
    ];

    for &status in statuses {
        let resp = client.request(
            client
                .request_builder("/")
                .header(req_status_header, status.as_str())
        );

        assert_eq!(resp.status(), status);
        assert!(!resp.headers().contains_key("content-length"), "content-length with status={:?}", status);
        assert!(!resp.headers().contains_key("transfer-encoding"), "transfer-encoding with status={:?}", status);
    }
}

#[test]
fn http1_head_responses() {
    let _ = env_logger::try_init();

    let srv = server::http1()
        .route_fn("/", move |req| {
            assert_eq!(req.method(), "HEAD");
            Response::builder()
                .header("content-length", "55")
                .body("".into())
                .unwrap()
        })
        .run();
    let proxy = proxy::new()
        .inbound(srv)
        .run();
    let client = client::http1(proxy.inbound, "transparency.test.svc.cluster.local");

    let resp = client.request(
        client
            .request_builder("/")
            .method("HEAD")
    );

    assert_eq!(resp.status(), StatusCode::OK);
    assert_eq!(resp.headers()["content-length"], "55");

    let body = resp.into_body()
        .concat2()
        .wait()
        .expect("response body concat");

    assert_eq!(body, "");
}

#[test]
fn http1_response_end_of_file() {
    let _ = env_logger::try_init();

    // test both http/1.0 and 1.1
    let srv = server::tcp()
        .accept(move |_read| {
            "\
            HTTP/1.0 200 OK\r\n\
            \r\n\
            body till eof\
            "
        })
        .accept(move |_read| {
            "\
            HTTP/1.1 200 OK\r\n\
            \r\n\
            body till eof\
            "
        })
        .run();
    let proxy = proxy::new()
        .inbound(srv)
        .run();

    let client = client::http1(proxy.inbound, "transparency.test.svc.cluster.local");

    let versions = &[
        "1.0",
        // TODO: We may wish to enforce not translating eof bodies to chunked,
        // even if the client is 1.1. However, there also benefits of translating:
        // the client can reuse the connection, and delimited messages are much
        // safer than those that end with the connection (as it's difficult to
        // notice if a full response was received).
        //
        // Either way, hyper's server does not provide the ability to do this,
        // so we cannot test for it at the moment.
        //"1.1",
    ];

    for v in versions {
        let resp = client.request(
            client
                .request_builder("/")
                .method("GET")
        );

        assert_eq!(resp.status(), StatusCode::OK, "HTTP/{}", v);
        assert!(!resp.headers().contains_key("transfer-encoding"), "HTTP/{} transfer-encoding", v);
        assert!(!resp.headers().contains_key("content-length"), "HTTP/{} content-length", v);

        let body = resp.into_body()
            .concat2()
            .wait()
            .expect("response body concat");

        assert_eq!(body, "body till eof", "HTTP/{} body", v);
    }
}

#[test]
fn http1_one_connection_per_host() {
    let _ = env_logger::try_init();

    let srv = server::http1()
        .route_empty_ok("/")
        .run();
    let proxy = proxy::new().inbound(srv).run();

    let client = client::http1(proxy.inbound, "foo.bar");

    let inbound = &proxy.inbound_server.as_ref()
        .expect("no inbound server!");

    // Make a request with the header "Host: foo.bar". After the request, the
    // server should have seen one connection.
    let res1 = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "foo.bar")
    );
    assert_eq!(res1.status(), http::StatusCode::OK);
    assert_eq!(res1.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 1);

    // Another request with the same host. The proxy may reuse the connection.
    let res1 = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "foo.bar")
    );
    assert_eq!(res1.status(), http::StatusCode::OK);
    assert_eq!(res1.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 1);

    // Make a request with a different Host header. This request must use a new
    // connection.
    let res2 = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "bar.baz"));
    assert_eq!(res2.status(), http::StatusCode::OK);
    assert_eq!(res2.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 2);

    let res2 = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "bar.baz"));
    assert_eq!(res2.status(), http::StatusCode::OK);
    assert_eq!(res2.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 2);

    // Make a request with a different Host header. This request must use a new
    // connection.
    let res3 = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "quuuux.com"));
    assert_eq!(res3.status(), http::StatusCode::OK);
    assert_eq!(res3.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 3);
}

#[test]
fn http1_requests_without_host_have_unique_connections() {
    let _ = env_logger::try_init();

    let srv = server::http1()
        .route_empty_ok("/")
        .run();
    let proxy = proxy::new().inbound(srv).run();

    let client = client::http1(proxy.inbound, "foo.bar");

    let inbound = &proxy.inbound_server.as_ref()
        .expect("no inbound server!");

    // Make a request with no Host header and no authority in the request path.
    let res = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "")
    );
    assert_eq!(res.status(), http::StatusCode::OK);
    assert_eq!(res.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 1);

    // Another request with no Host. The proxy must open a new connection
    // for that request.
    let res = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "")
    );
    assert_eq!(res.status(), http::StatusCode::OK);
    assert_eq!(res.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 2);

    // Make a request with a host header. It must also receive its
    // own connection.
    let res = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "foo.bar")
    );
    assert_eq!(res.status(), http::StatusCode::OK);
    assert_eq!(res.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 3);

    // Another request with no Host. The proxy must open a new connection
    // for that request.
    let res = client.request(client.request_builder("/")
        .version(http::Version::HTTP_11)
        .header("host", "")
    );
    assert_eq!(res.status(), http::StatusCode::OK);
    assert_eq!(res.version(), http::Version::HTTP_11);
    assert_eq!(inbound.connections(), 4);
}
