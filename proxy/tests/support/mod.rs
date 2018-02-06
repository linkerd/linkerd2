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
extern crate url;
pub extern crate env_logger;

use self::bytes::{BigEndian, Bytes, BytesMut};
pub use self::futures::*;
use self::futures::sync::oneshot;
pub use self::http::{HeaderMap, Request, Response};
use self::http::header::HeaderValue;
use self::tokio_connect::Connect;
use self::tokio_core::net::{TcpListener, TcpStream};
use self::tokio_core::reactor::{Core, Handle};
use self::tower::{NewService, Service};
use self::tower_h2::{Body, RecvBody};
use std::net::SocketAddr;
pub use std::time::Duration;

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
