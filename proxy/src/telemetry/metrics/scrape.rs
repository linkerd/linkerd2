use std::io;

use tower::Service;
use http;
use transparency::HttpBody;

/// Serve scrapable metrics.
#[derive(Debug, Clone)]
pub struct Server {
}

impl Service for Server {
    type Request = http::Request<HttpBody>;
    type Response = http::Response<HttpBody>;
    type Error = io::Error;
    type Future = Box<Future<Item=Self::Response, Error=Self::Error>>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        unimplemented!()
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        unimplemented!()
    }
}
