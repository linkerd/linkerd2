mod support;
use self::support::*;

use std::thread;

#[test]
fn outbound_asks_controller_api() {
    let _ = env_logger::init();

    let srv = server::new().route("/", "hello").route("/bye", "bye").run();
    let ctrl = controller::new()
        .destination("disco.test.svc.cluster.local", srv.addr)
        .run();
    let proxy = proxy::new().controller(ctrl).outbound(srv).run();
    let client = client::new(proxy.outbound, "disco.test.svc.cluster.local");

    assert_eq!(client.get("/"), "hello");
    assert_eq!(client.get("/bye"), "bye");
}

#[test]
fn outbound_reconnects_if_controller_stream_ends() {
    let _ = env_logger::init();

    let srv = server::new().route("/recon", "nect").run();
    let ctrl = controller::new()
        .destination_close("disco.test.svc.cluster.local")
        .destination("disco.test.svc.cluster.local", srv.addr)
        .run();
    let proxy = proxy::new().controller(ctrl).outbound(srv).run();
    let client = client::new(proxy.outbound, "disco.test.svc.cluster.local");

    assert_eq!(client.get("/recon"), "nect");
}

#[test]
fn outbound_updates_newer_services() {
    let _ = env_logger::init();

    //TODO: when the support server can listen on both http1 and http2
    //at the same time, do that here
    let srv = server::http1().route("/h1", "hello h1").run();
    let ctrl = controller::new()
        .destination("disco.test.svc.cluster.local", srv.addr)
        .run();
    let proxy = proxy::new().controller(ctrl).outbound(srv).run();
    // the HTTP2 service starts watching first, receiving an addr
    // from the controller
    let client1 = client::http2(proxy.outbound, "disco.test.svc.cluster.local");
    client1.get("/h2"); // 500, ignore

    // a new HTTP1 service needs to be build now, while the HTTP2
    // service already exists, so make sure previously sent addrs
    // get into the newer service
    let client2 = client::http1(proxy.outbound, "disco.test.svc.cluster.local");
    assert_eq!(client2.get("/h1"), "hello h1");
}

#[test]
#[cfg_attr(not(feature = "flaky_tests"), ignore)]
fn outbound_times_out() {
    let _ = env_logger::init();
    let mut env = config::TestEnv::new();

    // set the bind timeout to 100 ms.
    env.put(config::ENV_BIND_TIMEOUT, "100".to_owned());

    let srv = server::http1().route("/h1", "hello h1").run();
    let addr = srv.addr.clone();
    let ctrl = controller::new()
        // when the proxy requests the destination, sleep for 500 ms, and then
        // return the correct destination
        .destination_fn("disco.test.svc.cluster.local", move || {
            thread::sleep(Duration::from_millis(500));
            Some(controller::destination_update(addr))
        })
        .run();

    let proxy = proxy::new()
        .controller(ctrl)
        .outbound(srv)
        .run_with_test_env(env);

    let client = client::http2(proxy.outbound, "disco.test.svc.cluster.local");
    let mut req = client.request_builder("/");
    let rsp = client.request(req.method("GET"));

    // the request should time out
    assert_eq!(rsp.status(), http::StatusCode::INTERNAL_SERVER_ERROR);
}

#[test]
fn outbound_uses_orig_dst_if_not_local_svc() {
    let _ = env_logger::init();

    let srv = server::new()
        .route("/", "hello")
        .route("/bye", "bye")
        .run();
    let ctrl = controller::new()
        // no controller rule for srv
        .run();
    let proxy = proxy::new()
        .controller(ctrl)
        // set outbound orig_dst to srv
        .outbound(srv)
        .run();
    let client = client::new(proxy.outbound, "versioncheck.conduit.io");

    assert_eq!(client.get("/"), "hello");
    assert_eq!(client.get("/bye"), "bye");
}
