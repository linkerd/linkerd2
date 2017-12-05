extern crate env_logger;
extern crate futures;
extern crate bytes;
extern crate h2;
extern crate http;
extern crate string;
extern crate tokio_connect;
extern crate tokio_core;
extern crate tower;
extern crate tower_h2;

use futures::*;
use bytes::Bytes;
use http::{Request, Response};
use std::net::SocketAddr;
use string::{String, TryFrom};
use tokio_connect::Connect;
use tokio_core::net::TcpStream;
use tokio_core::reactor::{Core, Handle};
use tower::{NewService, Service};
use tower_h2::{Body, Client, RecvBody};
use h2::Reason;

pub struct Conn(SocketAddr, Handle);

fn main() {
    drop(env_logger::init());

    let mut core = Core::new().unwrap();
    let reactor = core.handle();

    let addr = "[::1]:8888".parse().unwrap();

    impl Connect for Conn {
        type Connected = TcpStream;
        type Error = ::std::io::Error;
        type Future = Box<Future<Item = TcpStream, Error = ::std::io::Error>>;

        fn connect(&self) -> Self::Future {
            let c = TcpStream::connect(&self.0, &self.1)
                .and_then(|tcp| tcp.set_nodelay(true).map(move |_| tcp));
            Box::new(c)
        }
    }

    let conn = Conn(addr, reactor.clone());
    let h2 = Client::<Conn, Handle, ()>::new(conn, Default::default(), reactor);

    let done = h2.new_service()
        .map_err(|_| Reason::REFUSED_STREAM.into())
        .and_then(move |h2| {
            Serial {
                h2,
                count: 500,
                pending: None,
            }
        })
        .map(|_| println!("done"))
        .map_err(|e| println!("error: {:?}", e));

    core.run(done).unwrap();
}

/// Avoids overflowing max concurrent streams
struct Serial {
    count: usize,
    h2: tower_h2::client::Service<Conn, Handle, ()>,
    pending: Option<Box<Future<Item = (), Error = tower_h2::client::Error>>>,
}

impl Future for Serial {
    type Item = ();
    type Error = tower_h2::client::Error;

    fn poll(&mut self) -> Poll<(), Self::Error> {
        loop {
            if let Some(mut fut) = self.pending.take() {
                if fut.poll()?.is_not_ready() {
                    self.pending = Some(fut);
                    return Ok(Async::NotReady);
                }
            }

            if self.count == 0 {
                return Ok(Async::Ready(()));
            }

            let pfx = format!("{}", self.count);
            self.count -= 1;
            let mut fut = self.h2
                .call(mkreq())
                .and_then(move |rsp| read_response(&pfx, rsp).map_err(Into::into));

            if fut.poll()?.is_not_ready() {
                self.pending = Some(Box::new(fut));
                return Ok(Async::NotReady);
            }
        }
    }
}

fn mkreq() -> Request<()> {
    Request::builder()
        .method("GET")
        .uri("http://[::1]:8888/")
        .version(http::Version::HTTP_2)
        .body(())
        .unwrap()
}

fn read_response(pfx: &str, rsp: Response<RecvBody>) -> ReadResponse {
    let (parts, body) = rsp.into_parts();
    println!("{}: {}", pfx, parts.status);
    let pfx = pfx.to_owned();
    ReadResponse {
        pfx,
        body,
    }
}

struct ReadResponse {
    pfx: ::std::string::String,
    body: RecvBody,
}

impl Future for ReadResponse {
    type Item = ();
    type Error = tower_h2::client::Error;
    fn poll(&mut self) -> Poll<(), Self::Error> {
        loop {
            match try_ready!(self.body.poll_data()) {
                None => return Ok(Async::Ready(())),
                Some(b) => {
                    let b: Bytes = b.into();
                    {
                        let s = String::try_from(b).expect("decode utf8 string");
                        println!("{}: {}", self.pfx, &*s);
                    }
                }
            }
        }
    }
}
