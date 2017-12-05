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

use bytes::Bytes;
use futures::*;
use http::{Request, HeaderMap};
use http::header::HeaderValue;
use tokio_core::net::TcpListener;
use tokio_core::reactor::Core;
use tower::{NewService, Service};
use tower_h2::{Body, Data, Server, RecvBody};

type Response = http::Response<GrpcBody>;

struct GrpcBody {
    message: Bytes,
    status: &'static str,
}

impl GrpcBody {
    fn new(body: Bytes) -> Self {
        GrpcBody {
            message: body,
            status: "0",
        }
    }

    fn unimplemented() -> Self {
        GrpcBody {
            message: Bytes::new(),
            status: "12",
        }
    }
}


impl Body for GrpcBody {
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

const GET_FEATURE: &'static str = "/routeguide.RouteGuide/GetFeature";

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

    fn call(&mut self, req: Request<RecvBody>) -> Self::Future {
        let mut rsp = http::Response::builder();
        rsp.version(http::Version::HTTP_2);

        let (head, body) = req.into_parts();
        match head.uri.path() {
            GET_FEATURE => {
                let body = RecvBodyStream(body);

                // TODO: This is not great flow control management
                Box::new(body.map(Bytes::from).concat2().and_then(move |bytes| {
                    let s = ::std::str::from_utf8(&bytes).unwrap();
                    println!("GetFeature = {:?}", s);
                    let body = GrpcBody::new("blah".into());
                    let rsp = rsp.body(body).unwrap();
                    Ok(rsp)
                }))
            },
            _ => {
                println!("unknown route");
                let body = GrpcBody::unimplemented();
                let rsp = rsp.body(body).unwrap();
                Box::new(future::ok(rsp))
            }
        }
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

    let addr = "[::1]:8888".parse().unwrap();
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
