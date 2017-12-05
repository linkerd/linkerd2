extern crate bytes;
extern crate env_logger;
extern crate futures;
extern crate h2;
extern crate http;
#[macro_use]
extern crate log;
extern crate tokio_core;
extern crate tower;
extern crate tower_h2;

use bytes::{Buf, BufMut, Bytes, BytesMut, IntoBuf, BigEndian};
use futures::{future, Async, Future, Poll, Stream};
use http::{Request, HeaderMap};
use http::header::HeaderValue;
use tokio_core::net::TcpListener;
use tokio_core::reactor::Core;
use tower::{NewService, Service};
use tower_h2::{Body, Data, Server, RecvBody};

type Response = http::Response<HelloBody>;

struct HelloBody {
    message: Bytes,
    status: &'static str,
}

impl HelloBody {
    fn new(body: Bytes) -> Self {
        HelloBody {
            message: body,
            status: "0",
        }
    }

    fn unimplemented() -> Self {
        HelloBody {
            message: Bytes::new(),
            status: "12",
        }
    }

    fn internal() -> Self {
        HelloBody {
            message: Bytes::new(),
            status: "13",
        }
    }
}


impl Body for HelloBody {
    type Data = Bytes;

    fn poll_data(&mut self) -> Poll<Option<Bytes>, h2::Error> {
        let data = self.message.split_off(0);
        let data = if data.is_empty() {
            None
        } else {
            Some(data)
        };
        Ok(Async::Ready(data))
    }

    fn poll_trailers(&mut self) -> Poll<Option<HeaderMap>, h2::Error> {
        let mut map = HeaderMap::new();
        map.insert("grpc-status", HeaderValue::from_static(self.status));
        Ok(Async::Ready(Some(map)))
    }
}

struct RecvBodyStream(tower_h2::RecvBody);

impl Stream for RecvBodyStream {
    type Item = Data;
    type Error = h2::Error;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        self.0.poll_data()
    }
}

const SAY_HELLO: &'static str = "/helloworld.Greeter/SayHello";

#[derive(Debug)]
struct Svc;
impl Service for Svc {
    type Request = Request<RecvBody>;
    type Response = Response;
    type Error = h2::Error;
    type Future = Box<Future<Item=Response, Error=h2::Error>>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(Async::Ready(()))
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        let mut rsp = http::Response::builder();
        rsp.version(http::Version::HTTP_2);

        if req.uri().path() != SAY_HELLO {
            println!("unknown route");
            let body = HelloBody::unimplemented();
            let rsp = rsp.body(body).unwrap();
            return Box::new(future::ok(rsp));
        }

        let hello = RecvBodyStream(req.into_parts().1);

        // TODO: This is not great flow control management
        Box::new(hello.map(Bytes::from).concat2().and_then(move |bytes| {
            let mut buf = bytes.into_buf();
            let compressed_byte = buf.get_u8();
            if compressed_byte == 1 {
                println!("compression not supported");
                let body = HelloBody::unimplemented();
                let rsp = rsp.body(body).unwrap();
                return Ok(rsp);
            } else if compressed_byte != 0 {
                println!("grpc header looked busted");
                let body = HelloBody::internal();
                let rsp = rsp.body(body).unwrap();
                return Ok(rsp);
            }

            let len = buf.get_u32::<BigEndian>() as usize;

            if buf.remaining() != len {
                println!("delimited message claims len={}, but body={}", len, buf.remaining());
                let body = HelloBody::internal();
                let rsp = rsp.body(body).unwrap();
                return Ok(rsp);
            }

            let s = ::std::str::from_utf8(buf.bytes()).unwrap();
            println!("HelloRequest = {}:{:?}", len, s);
            let reply = format!("Hello, {}", s);
            let mut bytes = BytesMut::with_capacity(reply.len() + 5);
            bytes.put_u8(0);
            bytes.put_u32::<BigEndian>(reply.len() as u32);
            bytes.put_slice(reply.as_bytes());

            let body = HelloBody::new(bytes.freeze());
            let rsp = rsp.body(body).unwrap();
            Ok(rsp)
        }))
    }
}

#[derive(Debug)]
struct NewSvc;
impl NewService for NewSvc {
    type Request = Request<RecvBody>;
    type Response = Response;
    type Error = h2::Error;
    type InitError = ::std::io::Error;
    type Service = Svc;
    type Future = future::FutureResult<Svc, Self::InitError>;

    fn new_service(&self) -> Self::Future {
        future::ok(Svc)
    }
}

fn main() {
    drop(env_logger::init());

    let mut core = Core::new().unwrap();
    let reactor = core.handle();

    let h2 = Server::new(NewSvc, Default::default(), reactor.clone());

    let addr = "127.0.0.1:9888".parse().unwrap();
    let bind = TcpListener::bind(&addr, &reactor).expect("bind");

    let serve = bind.incoming()
        .fold((h2, reactor), |(h2, reactor), (sock, _)| {
            if let Err(e) = sock.set_nodelay(true) {
                return Err(e);
            }

            let serve = h2.serve(sock);
            reactor.spawn(serve.map_err(|e| error!("h2 error: {:?}", e)));

            Ok((h2, reactor))
        });

    println!("Greeter listening on {}", addr);
    core.run(serve).unwrap();
}
