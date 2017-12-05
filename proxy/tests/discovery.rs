#[macro_use]
extern crate log;

mod support;
use self::support::*;

#[test]
fn outbound_asks_controller_api() {
    let _ = env_logger::init();

    let srv = server::new().route("/", "hello").route("/bye", "bye").run();
    let ctrl = controller::new()
        .destination("test.conduit.local", srv.addr)
        .run();
    let proxy = proxy::new().controller(ctrl).outbound(srv).run();
    let client = client::new(proxy.outbound, "test.conduit.local");

    assert_eq!(client.get("/"), "hello");
    assert_eq!(client.get("/bye"), "bye");
}

#[test]
fn outbound_reconnects_if_controller_stream_ends() {
    let _ = env_logger::init();

    let srv = server::new().route("/recon", "nect").run();
    let ctrl = controller::new()
        .destination_close("test.conduit.local")
        .destination("test.conduit.local", srv.addr)
        .run();
    let proxy = proxy::new().controller(ctrl).outbound(srv).run();
    let client = client::new(proxy.outbound, "test.conduit.local");

    assert_eq!(client.get("/recon"), "nect");
}

#[test]
#[ignore]
fn outbound_times_out() {
    // Currently, the outbound router will wait forever until discovery tells
    // it where to send the request. It should probably time out eventually.
}
