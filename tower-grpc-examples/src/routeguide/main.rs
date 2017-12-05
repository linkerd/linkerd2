#![allow(dead_code)]
#![allow(unused_variables)]

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

mod data;
mod routeguide {
    include!(concat!(env!("OUT_DIR"), "/routeguide.rs"));
}
use routeguide::{server, Point, Rectangle, Feature, RouteSummary, RouteNote};

use futures::{future, Future, Stream, Sink, Poll};
use futures::sync::mpsc;
use tokio_core::net::TcpListener;
use tokio_core::reactor::Core;
use tower::Service;
use tower_h2::Server;
use tower_grpc::{Request, Response};

use std::sync::Arc;

pub type Features = Arc<Vec<routeguide::Feature>>;

/// Handles GetFeature requests
#[derive(Clone, Debug)]
struct GetFeature(Features);

/// Handles ListFeatures requests
#[derive(Clone, Debug)]
struct ListFeatures(Features);

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

        for feature in &self.0[..] {
            if feature.location.as_ref() == Some(request.get_ref()) {
                return future::ok(Response::new(feature.clone()));
            }
        }

        // Otherwise, return some other feature?
        let response = Response::new(Feature {
            name: "".to_string(),
            location: None,
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
        use std::thread;

        println!("ListFeatures = {:?}", request);

        let (tx, rx) = mpsc::channel(4);

        let features = self.0.clone();

        thread::spawn(move || {
            let mut tx = tx.wait();

            for feature in &features[..] {
                if in_range(feature.location.as_ref().unwrap(), request.get_ref()) {
                    tx.send(feature.clone()).unwrap();
                }
            }
        });

        let rx = rx.map_err(|_| unimplemented!());
        future::ok(Response::new(Box::new(rx)))
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

fn in_range(point: &Point, rect: &Rectangle) -> bool {
    use std::cmp;

    let lo = rect.lo.as_ref().unwrap();
    let hi = rect.hi.as_ref().unwrap();

    let left = cmp::min(lo.longitude, hi.longitude);
    let right = cmp::max(lo.longitude, hi.longitude);
    let top = cmp::max(lo.latitude, hi.latitude);
    let bottom = cmp::min(lo.latitude, hi.latitude);

    point.longitude >= left &&
        point.longitude <= right &&
        point.latitude >= bottom &&
        point.latitude <= top
}

pub fn main() {
    let _ = ::env_logger::init();

    // Load data file
    let data = Arc::new(data::load());

    let mut core = Core::new().unwrap();
    let reactor = core.handle();

    let new_service = server::RouteGuide::new_service()
        .get_feature(GetFeature(data.clone()))
        .list_features(ListFeatures(data.clone()))
        ;

    let h2 = Server::new(new_service, Default::default(), reactor.clone());

    let addr = "127.0.0.1:10000".parse().unwrap();
    let bind = TcpListener::bind(&addr, &reactor).expect("bind");

    println!("listining on {:?}", addr);

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
