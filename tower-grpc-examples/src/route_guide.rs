extern crate bytes;
extern crate env_logger;
extern crate futures;
#[macro_use]
extern crate log;
extern crate prost;
#[macro_use]
extern crate prost_derive;
extern crate tokio_core;
extern crate tower;
extern crate tower_h2;
extern crate tower_grpc;

extern crate serde;
extern crate serde_json;
#[macro_use]
extern crate serde_derive;

mod routeguide {
    include!(concat!(env!("OUT_DIR"), "/routeguide.rs"));
}
use routeguide::{server, Point, Rectangle, Feature, RouteSummary, RouteNote};

use futures::{future, Future, Stream, Poll};
use futures::sync::mpsc;
use tokio_core::net::TcpListener;
use tokio_core::reactor::Core;
use tower::Service;
use tower_h2::Server;
use tower_grpc::{Request, Response};

#[derive(Debug, Deserialize)]
pub struct Route {
}

#[derive(Debug, Deserialize)]
pub struct Location {
    latitude: i32,
    longitude: i32,
}

/// Handles GetFeature requests
#[derive(Clone, Debug)]
struct GetFeature;

/// Handles ListFeatures requests
#[derive(Clone, Debug)]
struct ListFeatures;

/// Handles RecordRoute requests
#[derive(Clone, Debug)]
struct RecordRoute;

/// Handles RouteChat requests
#[derive(Clone, Debug)]
struct RouteChat;

impl Service for GetFeature {
    type Request = Request<Point>;
    type Response = Response<Feature>;
    type Error = tower_grpc::Error;
    type Future = future::FutureResult<Self::Response, Self::Error>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        println!("GetFeature = {:?}", request);

        let response = Response::new(Feature {
            name: "This is my feature".to_string(),
            location: Some(request.get_ref().clone()),
        });

        future::ok(response)
    }
}

impl Service for ListFeatures {
    type Request = Request<Rectangle>;
    type Response = Response<Box<Stream<Item = Feature, Error = tower_grpc::Error>>>;
    type Error = tower_grpc::Error;
    type Future = future::FutureResult<Self::Response, Self::Error>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        unimplemented!();
    }
}

impl Service for RecordRoute {
    type Request = Request<tower_grpc::protobuf::server::Decode<Point>>;
    type Response = Response<RouteSummary>;
    type Error = tower_grpc::Error;
    type Future = future::FutureResult<Self::Response, Self::Error>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        unimplemented!();
    }
}

impl Service for RouteChat {
    type Request = Request<tower_grpc::protobuf::server::Decode<RouteNote>>;
    type Response = Response<Box<Stream<Item = RouteNote, Error = tower_grpc::Error>>>;
    type Error = tower_grpc::Error;
    type Future = future::FutureResult<Self::Response, Self::Error>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        unimplemented!();
    }
}

pub fn main() {
    let _ = ::env_logger::init();

    let mut core = Core::new().unwrap();
    let reactor = core.handle();

    let new_service = server::RouteGuide::new_service()
        .get_feature(GetFeature)
        .list_features(ListFeatures)
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

