use std::str::FromStr;

use http;

use ::client::codec::Unary;

#[derive(Debug)]
pub struct Request<T> {
    http: http::Request<T>,
}

impl<T> Request<T> {
    /// Create a new gRPC request
    pub fn new(name: &str, message: T) -> Self {
        let mut req = http::Request::new(message);
        *req.version_mut() = http::Version::HTTP_2;
        *req.method_mut() = http::Method::POST;

        //TODO: specifically parse a `http::uri::PathAndQuery`
        *req.uri_mut() = http::Uri::from_str(name)
            .expect("user supplied illegal RPC name");

        Request {
            http: req,
        }
    }

    /// Get a reference to the message
    pub fn get_ref(&self) -> &T {
        self.http.body()
    }

    /// Get a mutable reference to the message
    pub fn get_mut(&mut self) -> &mut T {
        self.http.body_mut()
    }

    /// Convert an HTTP request to a gRPC request
    pub fn from_http(http: http::Request<T>) -> Self {
        // TODO: validate
        Request { http }
    }

    pub fn into_unary(self) -> Request<Unary<T>> {
        let (head, body) = self.http.into_parts();
        let http = http::Request::from_parts(head, Unary::new(body));
        Request {
            http,
        }
    }

    pub fn into_http(self) -> http::Request<T> {
        self.http
    }

    pub fn map<F, U>(self, f: F) -> Request<U>
    where F: FnOnce(T) -> U,
    {
        let (head, body) = self.http.into_parts();
        let body = f(body);
        let http = http::Request::from_parts(head, body);
        Request::from_http(http)
    }

    // pub fn metadata()
    // pub fn metadata_bin()
}
