// The support mod is compiled for all the integration tests, which are each
// compiled as separate crates. Each only uses a subset of this module, which
// means some of it is unused.
//
// Note, lints like `unused_variable` should not be ignored.
#![allow(dead_code)]

pub extern crate bytes;
pub extern crate conduit_proxy_controller_grpc;
extern crate conduit_proxy;
pub extern crate convert;
extern crate futures;
extern crate h2;
pub extern crate http;
extern crate hyper;
extern crate prost;
extern crate tokio_connect;
extern crate tokio_core;
pub extern crate tokio_io;
extern crate tower_h2;
extern crate tower_grpc;
extern crate tower_service;
extern crate log;
pub extern crate env_logger;

pub use std::collections::HashMap;
pub use std::net::SocketAddr;
pub use std::time::Duration;

pub use self::bytes::Bytes;
pub use self::conduit_proxy::*;
pub use self::futures::*;
use self::futures::sync::oneshot;
pub use self::http::{HeaderMap, Request, Response, StatusCode};
use self::tokio_connect::Connect;
use self::tokio_core::net::{TcpListener, TcpStream};
use self::tokio_core::reactor::{Core, Handle};
use self::tower_h2::{Body, RecvBody};
use self::tower_grpc as grpc;
use self::tower_service::{NewService, Service};

/// Environment variable for overriding the test patience.
pub const ENV_TEST_PATIENCE_MS: &'static str = "RUST_TEST_PATIENCE_MS";
pub const DEFAULT_TEST_PATIENCE: Duration = Duration::from_millis(15);

/// Retry an assertion up to a specified number of times, waiting
/// `RUST_TEST_PATIENCE_MS` between retries.
///
/// If the assertion is successful after a retry, execution will continue
/// normally. If all retries are exhausted and the assertion still fails,
/// `assert_eventually!` will panic as though a regular `assert!` had failed.
/// Note that other panics elsewhere in the code under test will not be
/// prevented.
///
/// This should be used sparingly, but is often useful in end-to-end testing
/// where a desired state may not be reached immediately. For example, when
/// some state updates asynchronously and there's no obvious way for the test
/// to wait for an update to occur before making assertions.
///
/// The `RUST_TEST_PATIENCE_MS` environment variable may be used to customize
/// the backoff duration between retries. This may be useful for purposes such
/// compensating for decreased performance on CI.
#[macro_export]
macro_rules! assert_eventually {
    ($cond:expr, retries: $retries:expr, $($arg:tt)+) => {
        {
            use std::{env, u64};
            use std::time::{Instant, Duration};
            use std::str::FromStr;
            // TODO: don't do this *every* time eventually is called (lazy_static?)
            let patience = env::var($crate::support::ENV_TEST_PATIENCE_MS).ok()
                .map(|s| {
                    let millis = u64::from_str(&s)
                        .expect(
                            "Could not parse RUST_TEST_PATIENCE_MS environment \
                             variable."
                        );
                    Duration::from_millis(millis)
                })
                .unwrap_or($crate::support::DEFAULT_TEST_PATIENCE);
            let start_t = Instant::now();
            for i in 0..($retries + 1) {
                if $cond {
                    break;
                } else if i == $retries {
                    panic!(
                        "assertion failed after {} (retried {} times): {}",
                        timeout::HumanDuration(start_t.elapsed()),
                        i,
                        format_args!($($arg)+)
                    )
                } else {
                    ::std::thread::sleep(patience);
                }
            }
        }
    };
    ($cond:expr, $($arg:tt)+) => {
        assert_eventually!($cond, retries: 5, $($arg)+)
    };
    ($cond:expr, retries: $retries:expr) => {
        assert_eventually!($cond, retries: $retries, stringify!($cond))
    };
    ($cond:expr) => {
        assert_eventually!($cond, retries: 5, stringify!($cond))
    };
}

pub mod client;
pub mod controller;
pub mod proxy;
pub mod server;
pub mod tcp;

pub fn shutdown_signal() -> (Shutdown, ShutdownRx) {
    let (tx, rx) = oneshot::channel();
    (Shutdown { tx }, Box::new(rx.then(|_| Ok(()))))
}

pub struct Shutdown {
    tx: oneshot::Sender<()>,
}

impl Shutdown {
    pub fn signal(self) {
        // a drop is enough
    }
}

pub type ShutdownRx = Box<Future<Item=(), Error=()> + Send>;

/// A channel used to signal when a Client's related connection is running or closed.
pub fn running() -> (oneshot::Sender<()>, Running) {
    let (tx, rx) = oneshot::channel();
    let rx = Box::new(rx.then(|_| Ok::<(), ()>(())));
    (tx, rx)
}

pub type Running = Box<Future<Item=(), Error=()> + Send>;

struct RecvBodyStream(tower_h2::RecvBody);

impl Stream for RecvBodyStream {
    type Item = Bytes;
    type Error = h2::Error;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        let data = try_ready!(self.0.poll_data());
        Ok(Async::Ready(data.map(From::from)))
    }
}

pub fn s(bytes: &[u8]) -> &str {
    ::std::str::from_utf8(bytes.as_ref()).unwrap()
}

/// The Rust test runner creates a thread per unit test, naming it after
/// the function name. If still in that thread, this can be useful to allow
/// associating test logs with a specific test, since tests *can* be run in
/// parallel.
pub fn thread_name() -> String {
    ::std::thread::current()
        .name()
        .unwrap_or("<no-name>")
        .to_owned()
}

#[test]
#[should_panic]
fn assert_eventually() {
    assert_eventually!(false)
}
