#![allow(dead_code)]
#![allow(unused_imports)]
#![allow(unused_variables)]

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

use bytes::BytesMut;
use futures::{Future, Stream};
use tokio_connect::Connect;
use tokio_core::net::TcpStream;
use tokio_core::reactor::{Core, Handle};
use tower::{Service, NewService};

use self::routeguide::{GetFeature, ListFeatures, RouteGuide, Point, Feature, Rectangle, RouteNote};

// eventually generated?
mod routeguide {
    use futures::{Future, Poll};
    use tower::Service;
    use tower_grpc;
    use tower_grpc::client::Codec;

    #[derive(Debug)]
    pub struct Point {
        pub latitude: i32,
        pub longitude: i32,
    }

    #[derive(Debug)]
    pub struct Rectangle {
        pub lo: Point,
        pub hi: Point,
    }

    #[derive(Debug)]
    pub struct Feature {
        pub name: String,
        pub location: Point,
    }

    #[derive(Debug)]
    pub struct RouteSummary {
        pub point_count: i32,
        pub feature_count: i32,
        pub distance: i32,
        pub elapsed_time: i32,
    }

    #[derive(Debug)]
    pub struct RouteNote {
        pub location: Point,
        pub message: String,
    }

    // the full "service"

    pub struct RouteGuide<GetFeatureRpc, ListFeaturesRpc, RecordRouteRpc, RouteChatRpc> {
        get_feature: GetFeatureRpc,
        list_features: ListFeaturesRpc,
        record_route: RecordRouteRpc,
        route_chat: RouteChatRpc,
    }

    impl<GetFeatureRpc, ListFeaturesRpc, RecordRouteRpc, RouteChatRpc, ListFeaturesStream>
        RouteGuide<GetFeatureRpc, ListFeaturesRpc, RecordRouteRpc, RouteChatRpc>
    where
        GetFeatureRpc: Service<
            Request=tower_grpc::Request<Point>,
            Response=tower_grpc::Response<Feature>,
        >,
        ListFeaturesRpc: Service<
            Request=tower_grpc::Request<Rectangle>,
            Response=tower_grpc::Response<ListFeaturesStream>,
        >,
        ListFeaturesStream: ::futures::Stream<Item=Feature>,
    {
        pub fn new(get_feature: GetFeatureRpc, list_features: ListFeaturesRpc, record_route: RecordRouteRpc, route_chat: RouteChatRpc) -> Self {
            RouteGuide {
                get_feature,
                list_features,
                record_route,
                route_chat,
            }
        }

        pub fn get_feature(&mut self, req: Point) -> ::futures::future::Map<GetFeatureRpc::Future, fn(tower_grpc::Response<Feature>) -> Feature>
        {
            let req = tower_grpc::Request::new("/routeguide.RouteGuide/GetFeature", req);
            self.get_feature.call(req)
                .map(|res| {
                    res.into_http().into_parts().1
                } as _)
        }

        //TODO: should this return a Stream wrapping the future?
        pub fn list_features(&mut self, req: Rectangle) -> ::futures::future::Map<ListFeaturesRpc::Future, fn(tower_grpc::Response<ListFeaturesStream>) -> ListFeaturesStream>
        {
            let req = tower_grpc::Request::new("/routeguide.RouteGuide/GetFeature", req);
            self.list_features.call(req)
                .map(|res| {
                    res.into_http().into_parts().1
                } as _)
        }
    }

    // rpc methods

    pub struct GetFeature<S> {
        service: S,
    }


    impl<C, S, E> GetFeature<S>
    where
        C: Codec<Encode=Point, Decode=Feature>,
        S: Service<
            Request=tower_grpc::Request<
                tower_grpc::client::codec::Unary<Point>
            >,
            Response=tower_grpc::Response<
                tower_grpc::client::codec::DecodingBody<C>
            >,
            Error=tower_grpc::Error<E>
        >,
    {
        pub fn new(service: S) -> Self {
            GetFeature {
                service,
            }
        }
    }

    impl<C, S, E> Service for GetFeature<S>
    where
        C: Codec<Encode=Point, Decode=Feature>,
        S: Service<
            Request=tower_grpc::Request<
                tower_grpc::client::codec::Unary<Point>
            >,
            Response=tower_grpc::Response<
                tower_grpc::client::codec::DecodingBody<C>
            >,
            Error=tower_grpc::Error<E>
        >,
    {
        type Request = tower_grpc::Request<Point>;
        type Response = tower_grpc::Response<Feature>;
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

    pub struct ListFeatures<S> {
        service: S,
    }

    impl<C, S, E> ListFeatures<S>
    where
        C: Codec<Encode=Rectangle, Decode=Feature>,
        S: Service<
            Request=tower_grpc::Request<
                tower_grpc::client::codec::Unary<Rectangle>
            >,
            Response=tower_grpc::Response<
                tower_grpc::client::codec::DecodingBody<C>
            >,
            Error=tower_grpc::Error<E>
        >,
    {
        pub fn new(service: S) -> Self {
            ListFeatures {
                service,
            }
        }
    }

    impl<C, S, E> Service for ListFeatures<S>
    where
        C: Codec<Encode=Rectangle, Decode=Feature>,
        S: Service<
            Request=tower_grpc::Request<
                tower_grpc::client::codec::Unary<Rectangle>
            >,
            Response=tower_grpc::Response<
                tower_grpc::client::codec::DecodingBody<C>
            >,
            Error=tower_grpc::Error<E>
        >,
    {
        type Request = tower_grpc::Request<Rectangle>;
        type Response = tower_grpc::Response<tower_grpc::client::codec::DecodingBody<C>>;
        type Error = S::Error;
        type Future = S::Future;

        fn poll_ready(&mut self) -> Poll<(), Self::Error> {
            self.service.poll_ready()
        }

        fn call(&mut self, req: Self::Request) -> Self::Future {
            self.service.call(req.into_unary())
        }
    }

    pub struct RecordRoute<S> {
        service: S,
    }

    pub struct RouteChat<S> {
        service: S,
    }
}

pub struct StupidCodec<T, U>(::std::marker::PhantomData<(T, U)>);

impl<T, U> StupidCodec<T, U> {
    fn new() -> Self {
        StupidCodec(::std::marker::PhantomData)
    }
}

impl<T, U> Clone for StupidCodec<T, U> {
    fn clone(&self) -> Self {
        StupidCodec(::std::marker::PhantomData)
    }
}

impl tower_grpc::client::Codec for StupidCodec<Point, Feature> {
    const CONTENT_TYPE: &'static str = "application/proto+stupid";

    type Encode = Point;
    type Decode = Feature;
    type EncodeError = ();
    type DecodeError = ();

    fn encode(&mut self, msg: Self::Encode, buf: &mut tower_grpc::client::codec::EncodeBuf) -> Result<(), Self::EncodeError> {
        Ok(())
    }

    fn decode(&mut self, buf: &mut tower_grpc::client::codec::DecodeBuf) -> Result<Self::Decode, Self::DecodeError> {
        Ok(Feature {
            name: String::from("faked"),
            location: Point {
                longitude: 5,
                latitude: 5,
            }
        })
    }
}

impl tower_grpc::client::Codec for StupidCodec<Rectangle, Feature> {
    const CONTENT_TYPE: &'static str = "application/proto+stupid";

    type Encode = Rectangle;
    type Decode = Feature;
    type EncodeError = ();
    type DecodeError = ();

    fn encode(&mut self, msg: Self::Encode, buf: &mut tower_grpc::client::codec::EncodeBuf) -> Result<(), Self::EncodeError> {
        Ok(())
    }

    fn decode(&mut self, buf: &mut tower_grpc::client::codec::DecodeBuf) -> Result<Self::Decode, Self::DecodeError> {
        Ok(Feature {
            name: String::from("faked"),
            location: Point {
                longitude: 5,
                latitude: 5,
            }
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
        .map_err(|_e| unimplemented!("h2 new_service error"))
        .and_then(move |orig_service| {

            let service = AddOrigin(orig_service.clone_handle());
            let grpc = tower_grpc::Client::new(StupidCodec::<Point, Feature>::new(), service);
            let get_feature = GetFeature::new(grpc);


            let service = AddOrigin(orig_service);
            let grpc = tower_grpc::Client::new(StupidCodec::<Rectangle, Feature>::new(), service);
            let list_features = ListFeatures::new(grpc);

            let mut client = RouteGuide::new(get_feature, list_features, (), ());

            let valid_feature = client.get_feature(Point {
                latitude: 409146138,
                longitude: -746188906,
            }).map(|feature| {
                println!("GetFeature: {:?}", feature);
            }).map_err(|e| ("GetFeature", e));

            let missing_feature = client.get_feature(Point {
                latitude: 0,
                longitude: 0,
            }).map(|feature| {
                println!("GetFeature: {:?}", feature);
            }).map_err(|e| ("GetFeature", e));

            let features_between = client.list_features(Rectangle {
                lo: Point {
                    latitude: 400000000,
                    longitude: -750000000,
                },
                hi: Point {
                    latitude: 420000000,
                    longitude: -730000000,
                }
            }).and_then(|features| {
                features.for_each(|feature| {
                    println!("ListFeatures: {:?}", feature);
                    Ok(())
                }).map_err(|e| match e {
                    tower_grpc::Error::Inner(h2) => tower_grpc::Error::Inner(h2.into()),
                    tower_grpc::Error::Grpc(status) => tower_grpc::Error::Grpc(status),
                })
            }).map_err(|e| ("ListFeatures", e));

            /*
            let record_route = client.record_route(futures::stream::iter_ok::<_, ()>(vec![
                Point {
                    longitude: 1,
                    latitude: 1,
                },
                Point {
                    longitude: 2,
                    latitude: 2,
                },
            ])).map(|summary| {
                println!("RecordRoute: {:?}", summary);
            }).map_err(|e| ("RecordRoute", e));

            let route_chat = client.route_chat(futures::stream::iter_ok::<_, ()>(vec![
                RouteNote {
                    location: Point {
                        longitude: 55,
                        latitude: 55,
                    },
                    message: "First note".to_string(),
                },
            ])).for_each(|_| {
                Ok(())
            }).map_err(|e| ("RouteChat", e));
            */

            valid_feature
                .join(missing_feature)
                //.join(features_between)
                //.join(record_route)
                //.join(route_chat)
        })
        .map_err(|e| println!("error: {:?}", e));

    let _ = core.run(done);
}
