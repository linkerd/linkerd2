#![deny(warnings)]
mod support;
use self::support::*;

macro_rules! generate_tests {
    (server: $make_server:path, client: $make_client:path) => {
        use conduit_proxy_controller_grpc as pb;

        #[test]
        fn outbound_asks_controller_api() {
            let _ = env_logger::try_init();
            let srv = $make_server().route("/", "hello").route("/bye", "bye").run();

            let ctrl = controller::new()
                .destination_and_close("disco.test.svc.cluster.local", srv.addr);

            let proxy = proxy::new().controller(ctrl.run()).outbound(srv).run();
            let client = $make_client(proxy.outbound, "disco.test.svc.cluster.local");

            assert_eq!(client.get("/"), "hello");
            assert_eq!(client.get("/bye"), "bye");
        }

        #[test]
        fn outbound_reconnects_if_controller_stream_ends() {
            let _ = env_logger::try_init();

            let srv = $make_server().route("/recon", "nect").run();

            let ctrl = controller::new()
                .destination_close("disco.test.svc.cluster.local")
                .destination_and_close("disco.test.svc.cluster.local", srv.addr);

            let proxy = proxy::new().controller(ctrl.run()).outbound(srv).run();
            let client = $make_client(proxy.outbound, "disco.test.svc.cluster.local");

            assert_eq!(client.get("/recon"), "nect");
        }

        #[test]
        #[cfg_attr(not(feature = "flaky_tests"), ignore)]
        fn outbound_destinations_reset_on_reconnect_followed_by_no_endpoints_exists() {
            outbound_destinations_reset_on_reconnect(move || {
                Some(controller::destination_exists_with_no_endpoints())
            })
        }

        #[test]
        #[cfg_attr(not(feature = "flaky_tests"), ignore)]
        fn outbound_destinations_reset_on_reconnect_followed_by_add_none() {
            outbound_destinations_reset_on_reconnect(move || {
                Some(controller::destination_add_none())
            })
        }

        #[test]
        #[cfg_attr(not(feature = "flaky_tests"), ignore)]
        fn outbound_destinations_reset_on_reconnect_followed_by_remove_none() {
            outbound_destinations_reset_on_reconnect(move || {
                Some(controller::destination_remove_none())
            })
        }

        fn outbound_destinations_reset_on_reconnect<F>(f: F)
            where F: Fn() -> Option<pb::destination::Update> + Send + 'static
        {
            use std::thread;
            let _ = env_logger::try_init();
            let mut env = config::TestEnv::new();

            // set the bind timeout to 100 ms.
            env.put(config::ENV_BIND_TIMEOUT, "100ms".to_owned());

            let srv = $make_server().route("/", "hello").run();
            let ctrl = controller::new();

            let dst_tx0 = ctrl.destination_tx("initially-exists.ns.svc.cluster.local");
            dst_tx0.send_addr(srv.addr);

            let dst_tx1 = ctrl.destination_tx("initially-exists.ns.svc.cluster.local");

            let proxy = proxy::new()
                .controller(ctrl.run())
                .outbound(srv)
                .run_with_test_env(env);

            let initially_exists =
                $make_client(proxy.outbound, "initially-exists.ns.svc.cluster.local");
            assert_eq!(initially_exists.get("/"), "hello");

            drop(dst_tx0); // trigger reconnect
            match f() {
                None => drop(dst_tx1),
                Some(up) => dst_tx1.send(up),
            }

            // Wait for the reconnect to happen. TODO: Replace this flaky logic.
            thread::sleep(Duration::from_millis(1000));

            // This will time out since there are no endpoints.
            let mut req = initially_exists.request_builder("/");
            let rsp = initially_exists.request(req.method("GET"));
            // the request should time out
            assert_eq!(rsp.status(), http::StatusCode::INTERNAL_SERVER_ERROR);
        }

        #[test]
        #[cfg_attr(not(feature = "flaky_tests"), ignore)]
        fn outbound_times_out() {
            let _ = env_logger::try_init();
            let mut env = config::TestEnv::new();

            // set the bind timeout to 100 ms.
            env.put(config::ENV_BIND_TIMEOUT, "100ms".to_owned());

            let srv = $make_server().route("/hi", "hello").run();
            let ctrl = controller::new();

            // when the proxy requests the destination, don't respond.
            let _dst_tx = ctrl.destination_tx("disco.test.svc.cluster.local");

            let proxy = proxy::new()
                .controller(ctrl.run())
                .outbound(srv)
                .run_with_test_env(env);

            let client = $make_client(proxy.outbound, "disco.test.svc.cluster.local");
            let mut req = client.request_builder("/");
            let rsp = client.request(req.method("GET"));
            // the request should time out
            assert_eq!(rsp.status(), http::StatusCode::INTERNAL_SERVER_ERROR);
        }

        #[test]
        fn outbound_asks_controller_without_orig_dst() {
            let _ = env_logger::try_init();

            let srv = $make_server()
                .route("/", "hello")
                .route("/bye", "bye")
                .run();

            let ctrl = controller::new()
                .destination_and_close("disco.test.svc.cluster.local", srv.addr);

            let proxy = proxy::new()
                .controller(ctrl.run())
                // don't set srv as outbound(), so that SO_ORIGINAL_DST isn't
                // used as a backup
                .run();
            let client = $make_client(proxy.outbound, "disco.test.svc.cluster.local");

            assert_eq!(client.get("/"), "hello");
            assert_eq!(client.get("/bye"), "bye");
        }
    }
}

mod http2 {
    use super::support::*;

    generate_tests! { server: server::new, client: client::new }

}

mod http1 {
    use super::support::*;

    generate_tests! { server: server::http1, client: client::http1 }

    mod absolute_uris {
        use super::super::support::*;

        generate_tests! {
            server: server::http1,
            client: client::http1_absolute_uris
        }

    }

}


#[test]
fn outbound_updates_newer_services() {
    let _ = env_logger::try_init();

    //TODO: when the support server can listen on both http1 and http2
    //at the same time, do that here
    let srv = server::http1().route("/h1", "hello h1").run();

    let ctrl = controller::new()
        .destination_and_close("disco.test.svc.cluster.local", srv.addr);

    let proxy = proxy::new()
        .controller(ctrl.run())
        .outbound(srv)
        .run();

    // the HTTP2 service starts watching first, receiving an addr
    // from the controller
    let client1 = client::http2(proxy.outbound, "disco.test.svc.cluster.local");

    // This HTTP2 client tries to talk to our HTTP1 server, and the server
    // will return an error (see above TODO).
    //
    // The reason to do this request at all is because the proxy will create
    // an H2 service mapping when it sees an H2 request, and we want to test
    // that when it sees H1 and tries to create a new mapping, the existing
    // known Discovery information is shared with it.
    let res = client1.request(&mut client1.request_builder("/h1"));
    assert_eq!(res.status(), 500);


    // a new HTTP1 service needs to be build now, while the HTTP2
    // service already exists, so make sure previously sent addrs
    // get into the newer service
    let client2 = client::http1(proxy.outbound, "disco.test.svc.cluster.local");
    assert_eq!(client2.get("/h1"), "hello h1");
}
