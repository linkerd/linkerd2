extern crate bytes;
extern crate env_logger;
extern crate futures;
extern crate http;
extern crate h2;
extern crate tokio_connect;
extern crate tokio_core;
extern crate tower;
extern crate tower_grpc;
extern crate tower_h2;

use std::net::SocketAddr;

use bytes::{Buf, BufMut};
use futures::Future;
use tokio_connect::Connect;
use tokio_core::net::TcpStream;
use tokio_core::reactor::{Core, Handle};
use tower::{Service, NewService};

use self::helloworld::{Greeter, HelloRequest, HelloReply, SayHello};

// eventually generated?
mod helloworld {
    use futures::{Future, Poll};
    use tower::Service;
    use tower_grpc;
    use tower_grpc::client::Codec;

    pub struct HelloRequest {
        pub name: String,
    }

    pub struct HelloReply {
        pub message: String,
    }

    pub struct Greeter<SayHelloRpc> {
        say_hello: SayHelloRpc,
    }

    impl<SayHelloRpc> Greeter<SayHelloRpc>
    where
        SayHelloRpc: Service<
            Request=tower_grpc::Request<HelloRequest>,
            Response=tower_grpc::Response<HelloReply>,
        >,
    {
        pub fn new(say_hello: SayHelloRpc) -> Self {
            Greeter {
                say_hello,
            }
        }

        pub fn say_hello(&mut self, req: HelloRequest) -> ::futures::future::Map<SayHelloRpc::Future, fn(tower_grpc::Response<HelloReply>) -> HelloReply> {
            let req = tower_grpc::Request::new("/helloworld.Greeter/SayHello", req);
            self.say_hello.call(req)
                .map(|res| {
                    res.into_http().into_parts().1
                } as _)
        }
    }

    pub struct SayHello<S> {
        service: S,
    }

    impl<C, S> SayHello<S>
    where
        C: Codec<Encode=HelloRequest, Decode=HelloReply>,
        S: Service<
            Request=tower_grpc::Request<
                tower_grpc::client::codec::Unary<HelloRequest>
            >,
            Response=tower_grpc::Response<
                tower_grpc::client::codec::DecodingBody<C>
            >,
        >,
    {
        pub fn new(service: S) -> Self {
            SayHello {
                service,
            }
        }
    }

    impl<C, S, E> Service for SayHello<S>
    where
        C: Codec<Encode=HelloRequest, Decode=HelloReply>,
        S: Service<
            Request=tower_grpc::Request<
                tower_grpc::client::codec::Unary<HelloRequest>
            >,
            Response=tower_grpc::Response<
                tower_grpc::client::codec::DecodingBody<C>
            >,
            Error=tower_grpc::Error<E>
        >,
    {
        type Request = tower_grpc::Request<HelloRequest>;
        type Response = tower_grpc::Response<HelloReply>;
        type Error = S::Error;
        type Future = tower_grpc::client::Unary<S::Future, C>;

        fn poll_ready(&mut self) -> Poll<(), Self::Error> {
            self.service.poll_ready()
        }

        fn call(&mut self, req: Self::Request) -> Self::Future {
            let fut = self.service.call(req.into_unary());
            tower_grpc::client::Unary::map_future(fut)
        }
    }
}

#[derive(Clone, Copy)]
pub struct StupidCodec;

impl tower_grpc::client::codec::Codec for StupidCodec {
    const CONTENT_TYPE: &'static str = "application/proto+stupid";
    type Encode = HelloRequest;
    type Decode = HelloReply;
    type EncodeError = ();
    type DecodeError = ();


    fn encode(&mut self, msg: Self::Encode, buf: &mut tower_grpc::client::codec::EncodeBuf) -> Result<(), Self::EncodeError> {
        buf.reserve(msg.name.len());
        buf.put(msg.name.as_bytes());
        Ok(())
    }

    fn decode(&mut self, buf: &mut tower_grpc::client::codec::DecodeBuf) -> Result<Self::Decode, Self::DecodeError> {
        let s = ::std::str::from_utf8(buf.bytes()).unwrap().to_string();
        buf.advance(s.len());
        Ok(HelloReply {
            message: s,
        })
    }
}

struct Conn(SocketAddr, Handle);

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

struct AddOrigin<S>(S);

impl<S, B> Service for AddOrigin<S>
where
    S: Service<Request=http::Request<B>>,
{
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> ::futures::Poll<(), Self::Error> {
        self.0.poll_ready()
    }

    fn call(&mut self, mut req: Self::Request) -> Self::Future {
        use std::str::FromStr;
        //TODO: use Uri.into_parts() to be more efficient
        let full_uri = format!("http://127.0.0.1:8888{}", req.uri().path());
        let new_uri = http::Uri::from_str(&full_uri).expect("example uri should work");
        *req.uri_mut() = new_uri;
        self.0.call(req)
    }
}


fn main() {
    drop(env_logger::init());

    let mut core = Core::new().unwrap();
    let reactor = core.handle();

    let addr = "[::1]:8888".parse().unwrap();


    let conn = Conn(addr, reactor.clone());
    let h2 = tower_h2::Client::new(conn, Default::default(), reactor);

    let done = h2.new_service()
        .map_err(|e| unimplemented!("h2 new_service error: {:?}", e))
        .and_then(move |service| {
            let service = AddOrigin(service);
            let grpc = tower_grpc::Client::new(StupidCodec, service);
            let say_hello = SayHello::new(grpc);
            let mut greeter = Greeter::new(say_hello);
            greeter.say_hello(HelloRequest {
                name: String::from("world"),
            })
        })
        .map(|reply| println!("Greeter.SayHello: {}", reply.message))
        .map_err(|e| println!("error: {:?}", e));

    let _ = core.run(done);
}
