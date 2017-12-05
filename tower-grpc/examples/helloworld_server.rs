#![allow(unused_variables)]

extern crate env_logger;
#[macro_use]
extern crate futures;
#[macro_use]
extern crate log;
#[macro_use]
extern crate prost_derive;
extern crate tokio_core;
extern crate tower;
extern crate tower_h2;
extern crate tower_grpc;

use futures::{future, Future, Stream, Poll};
use tokio_core::net::TcpListener;
use tokio_core::reactor::Core;
use tower::Service;
use tower_grpc::{Request, Response};
use tower_h2::Server;

#[derive(Clone, Debug)]
struct Greet;

impl Service for Greet {
    type Request = Request<HelloRequest>;
    type Response = Response<HelloReply>;
    type Error = tower_grpc::Error;
    type Future = future::FutureResult<Self::Response, Self::Error>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        let response = Response::new(HelloReply {
            message: "Zomg, it works!".to_string(),
        });

        future::ok(response)
    }
}

pub fn main() {
    let _ = ::env_logger::init();

    let mut core = Core::new().unwrap();
    let reactor = core.handle();

    let new_service = server::Greeter::new_service()
        .say_hello(Greet)
        ;

    let h2 = Server::new(new_service, Default::default(), reactor.clone());

    let addr = "[::1]:50051".parse().unwrap();
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

    core.run(serve).unwrap();
}

/// The request message containing the user's name.
#[derive(Clone, Debug, PartialEq, Message)]
pub struct HelloRequest {
    #[prost(string, tag="1")]
    pub name: String,
}

/// The response message containing the greetings
#[derive(Clone, Debug, PartialEq, Message)]
pub struct HelloReply {
    #[prost(string, tag="1")]
    pub message: String,
}

pub mod server {
    use super::{HelloRequest, HelloReply};
    use ::tower_grpc::codegen::server::*;

    #[derive(Debug)]
    pub struct Greeter<SayHello>
    where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
    {
        say_hello: grpc::Grpc<grpc::Unary<SayHello, grpc::Decode<HelloRequest>>>,
    }

    impl Greeter<grpc::NotImplemented<HelloRequest, HelloReply>>
    {
        pub fn new_service() -> greeter::NewService<grpc::NotImplemented<HelloRequest, HelloReply>> {
            greeter::NewService::new()
        }
    }

    impl<SayHello> Clone for Greeter<SayHello>
    where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
    {
        fn clone(&self) -> Self {
            Greeter {
                say_hello: self.say_hello.clone(),
            }
        }
    }

    // ===== impl Greeter service =====

    impl<SayHello> tower::Service for Greeter<SayHello>
    where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
    {
        type Request = http::Request<::tower_h2::RecvBody>;
        type Response = http::Response<greeter::ResponseBody<SayHello>>;
        type Error = h2::Error;
        type Future = greeter::ResponseFuture<SayHello>;

        fn poll_ready(&mut self) -> futures::Poll<(), Self::Error> {
            // Always ready
            Ok(().into())
        }

        fn call(&mut self, request: Self::Request) -> Self::Future {
            use self::greeter::Kind::*;

            println!("PATH={:?}", request.uri().path());

            match request.uri().path() {
                "/helloworld.Greeter/SayHello" => {
                    let response = self.say_hello.call(request);
                    greeter::ResponseFuture { kind: Ok(SayHello(response)) }
                }
                _ => {
                    greeter::ResponseFuture { kind: Err(grpc::Status::UNIMPLEMENTED) }
                }
            }
        }
    }

    pub mod greeter {
        use ::tower_grpc::codegen::server::*;
        use super::{Greeter, HelloRequest, HelloReply};
        use std::fmt;

        #[derive(Debug)]
        pub struct NewService<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            inner: Greeter<SayHello>,
        }

        pub struct ResponseFuture<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            pub(super) kind: Result<Kind<<grpc::Grpc<grpc::Unary<SayHello, grpc::Decode<HelloRequest>>> as tower::Service>::Future>, grpc::Status>,
        }

        pub struct ResponseBody<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            kind: Result<Kind<grpc::Encode<<grpc::Unary<SayHello, grpc::Decode<HelloRequest>> as grpc::GrpcService>::ResponseStream>>, grpc::Status>,
        }

        /// Enumeration of all the service methods
        #[derive(Debug)]
        pub(super) enum Kind<SayHello> {
            SayHello(SayHello),
        }

        impl NewService<grpc::NotImplemented<HelloRequest, HelloReply>>
        {
            pub fn new() -> Self {
                NewService {
                    inner: Greeter {
                        say_hello: grpc::Grpc::new(grpc::Unary::new(grpc::NotImplemented::new())),
                    },
                }
            }
        }

        impl<SayHello> NewService<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            /// Set the `say_hello` method
            pub fn say_hello<T>(self, say_hello: T) -> NewService<T>
            where T: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
            {
                let say_hello = grpc::Grpc::new(grpc::Unary::new(say_hello));

                NewService {
                    inner: Greeter {
                        say_hello,
                    }
                }
            }
        }

        impl<SayHello> tower::NewService for NewService<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            type Request = http::Request<::tower_h2::RecvBody>;
            type Response = http::Response<ResponseBody<SayHello>>;
            type Error = h2::Error;
            type Service = Greeter<SayHello>;
            type InitError = h2::Error;
            type Future = futures::FutureResult<Self::Service, Self::Error>;

            fn new_service(&self) -> Self::Future {
                futures::ok(self.inner.clone())
            }
        }

        impl<SayHello> futures::Future for ResponseFuture<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            type Item = http::Response<ResponseBody<SayHello>>;
            type Error = h2::Error;

            fn poll(&mut self) -> futures::Poll<Self::Item, Self::Error> {
                use self::Kind::*;

                match self.kind {
                    Ok(SayHello(ref mut fut)) => {
                        let response = try_ready!(fut.poll());
                        let (head, body) = response.into_parts();
                        let body = ResponseBody { kind: Ok(SayHello(body)) };
                        let response = http::Response::from_parts(head, body);
                        Ok(response.into())
                    }
                    Err(ref status) => {
                        let body = ResponseBody { kind: Err(status.clone()) };
                        Ok(grpc::Response::new(body).into_http().into())
                    }
                }
            }
        }

        impl<SayHello> fmt::Debug for ResponseFuture<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            fn fmt(&self, fmt: &mut fmt::Formatter) -> fmt::Result {
                write!(fmt, "ResponseFuture")
            }
        }

        impl<SayHello> ::tower_h2::Body for ResponseBody<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            type Data = bytes::Bytes;

            fn is_end_stream(&self) -> bool {
                use self::Kind::*;

                match self.kind {
                    Ok(SayHello(ref v)) => v.is_end_stream(),
                    Err(_) => true,
                }
            }

            fn poll_data(&mut self) -> futures::Poll<Option<Self::Data>, h2::Error> {
                use self::Kind::*;

                match self.kind {
                    Ok(SayHello(ref mut v)) => v.poll_data(),
                    Err(_) => Ok(None.into()),
                }
            }

            fn poll_trailers(&mut self) -> futures::Poll<Option<http::HeaderMap>, h2::Error> {
                use self::Kind::*;

                match self.kind {
                    Ok(SayHello(ref mut v)) => v.poll_trailers(),
                    Err(ref status) => {
                        let mut map = http::HeaderMap::new();
                        map.insert("grpc-status", status.to_header_value());
                        Ok(Some(map).into())
                    }
                }
            }
        }

        impl<SayHello> fmt::Debug for ResponseBody<SayHello>
        where SayHello: grpc::UnaryService<Request = HelloRequest, Response = HelloReply>,
        {
            fn fmt(&self, fmt: &mut fmt::Formatter) -> fmt::Result {
                write!(fmt, "ResponseBody")
            }
        }
    }
}
