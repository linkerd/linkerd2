#![allow(unused)]

extern crate bytes;
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
extern crate tower;
extern crate tower_h2;
pub extern crate env_logger;

use self::bytes::{BigEndian, Bytes, BytesMut};
pub use self::conduit_proxy::*;
pub use self::futures::*;
use self::futures::sync::oneshot;
pub use self::http::{HeaderMap, Request, Response, StatusCode};
use self::http::header::HeaderValue;
use self::tokio_connect::Connect;
use self::tokio_core::net::{TcpListener, TcpStream};
use self::tokio_core::reactor::{Core, Handle};
use self::tower::{NewService, Service};
use self::tower_h2::{Body, RecvBody};
use std::net::SocketAddr;
pub use std::time::Duration;

/// Environment variable for overriding the test patience.
pub const ENV_TEST_PATIENCE_MS: &'static str = "RUST_TEST_PATIENCE_MS";
pub const DEFAULT_TEST_PATIENCE: u64 = 15;

#[macro_export]
macro_rules! eventually {
    ($cond:expr, retries: $retries:expr, $($arg:tt)+) => {
        {
            use std::{env, u64};
            use std::time::{Instant, Duration};
            use std::str::FromStr;
            // TODO: don't do this *every* time eventually is called (lazy_static?)
            let patience = env::var($crate::support::ENV_TEST_PATIENCE_MS).ok()
                .and_then(|s| u64::from_str(&s).ok())
                .unwrap_or($crate::support::DEFAULT_TEST_PATIENCE);
            let patience = Duration::from_millis(patience);
            let start_t = Instant::now();
            for i in 0..($retries + 1) {
                if $cond {
                    break;
                } else if i == $retries {
                    panic!(
                        "assertion failed after {:?} (retried {} times): {}",
                        start_t.elapsed(), i, format_args!($($arg)+)
                    )
                } else {
                    ::std::thread::sleep(patience);
                }
            }
        }
    };
    ($cond:expr, $($arg:tt)+) => {
        eventually!($cond, retries: 5, $($arg)+)
    };
    ($cond:expr, retries: $retries:expr) => {
        eventually!($cond, retries: $retries, stringify!($cond))
    };
    ($cond:expr) => {
        eventually!($cond, retries: 5, stringify!($cond))
    };
}

pub mod client;
pub mod controller;
pub mod proxy;
pub mod server;
mod tcp;

pub type Shutdown = oneshot::Sender<()>;
pub type ShutdownRx = future::Then<
    oneshot::Receiver<()>,
    Result<(), ()>,
    fn(Result<(), oneshot::Canceled>) -> Result<(), ()>,
>;

pub fn shutdown_signal() -> (oneshot::Sender<()>, ShutdownRx) {
    let (tx, rx) = oneshot::channel();
    (tx, rx.then(|_| { Ok(()) } as _))
}


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

#[test]
#[should_panic]
fn eventually() {
    eventually!(false)
}
