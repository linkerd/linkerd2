pub mod codec;

use futures::{Async, Future, Poll, Stream};
use http;
use http::header::{HeaderMap, HeaderValue};
use tower::Service;
use tower_h2::RecvBody;

pub use self::codec::Codec;

use self::codec::{DecodingBody, EncodingBody};
use ::Status;

/// A gRPC client wrapping a `Service` over `h2`.
#[derive(Debug)]
pub struct Client<C, S> {
    codec: C,
    service: S,
}

#[must_use = "futures do nothing unless polled"]
#[derive(Debug)]
pub struct ResponseFuture<D, F> {
    decoder: Option<D>,
    future: F,
}

/// Future mapping Response<B> into B.
#[must_use = "futures do nothing unless polled"]
#[derive(Debug)]
pub struct BodyFuture<F> {
    future: F,
}

#[must_use = "futures do nothing unless polled"]
#[derive(Debug)]
pub struct Unary<F, D> where D: Codec {
    body: Option<DecodingBody<D>>,
    future: F,
    head: Option<http::response::Parts>,
    message: Option<D::Decode>,
}

/// A stream of a future Response's body items.
#[must_use = "streams do nothing unless polled"]
#[derive(Debug)]
pub struct Streaming<F, B> {
    body: Option<B>,
    future: F,
}

// ====== impl Client =====

impl<C, S> Client<C, S> {
    /// Create a new `Client` over an h2 service.
    pub fn new(codec: C, service: S) -> Self {
        Client {
            codec,
            service,
        }
    }
}

impl<C, S, R> Service for Client<C, S>
where
    C: Codec,
    S: Service<Request=http::Request<EncodingBody<C, R>>, Response=http::Response<RecvBody>>,
    R: Stream<Item=C::Encode>,
{
    type Request = ::Request<R>;
    type Response = ::Response<DecodingBody<C>>;
    type Error = ::Error<S::Error>;
    type Future = ResponseFuture<C, S::Future>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.service.poll_ready()
            .map_err(::Error::Inner)
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        let http  = req.into_http();
        let (mut head, body) = http.into_parts();

        // gRPC headers
        head.headers.insert(http::header::TE, HeaderValue::from_static("trailers"));

        let content_type = HeaderValue::from_static(C::CONTENT_TYPE);
        head.headers.insert(http::header::CONTENT_TYPE, content_type);

        let encoded = EncodingBody::new(self.codec.clone(), body);
        let req = http::Request::from_parts(head, encoded);
        let fut = self.service.call(req);

        ResponseFuture {
            decoder: Some(self.codec.clone()),
            future: fut,
        }
    }
}

// ====== impl ResponseFuture =====

impl<D, F> Future for ResponseFuture<D, F>
where
    D: Codec,
    F: Future<Item=http::Response<RecvBody>>,
{
    type Item = ::Response<DecodingBody<D>>;
    type Error = ::Error<F::Error>;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let res = try_ready!(self.future.poll().map_err(::Error::Inner));
        let (head, body) = res.into_parts();

        if let Some(status) = check_grpc_status(&head.headers) {
            return Err(::Error::Grpc(status));
        }

        let decoded = DecodingBody::new(self.decoder.take().unwrap(), body);
        let res = http::Response::from_parts(head, decoded);
        let grpc = ::Response::from_http(res);
        Ok(Async::Ready(grpc))
    }
}

// ====== impl Unary =====

impl<F, D> Unary<F, D>
where
    D: Codec,
{
    pub fn map_future(future: F) -> Self {
        Unary {
            body: None,
            future,
            head: None,
            message: None,
        }
    }
}

impl<F, D, E> Future for Unary<F, D>
where
    F: Future<Item=::Response<DecodingBody<D>>, Error=::Error<E>>,
    D: Codec,
{
    type Item = ::Response<D::Decode>;
    type Error = ::Error<E>;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let ref mut body = match self.body {
            Some(ref mut body) => body,
            None => {
                let resp = try_ready!(self.future.poll()).into_http();
                let (head, body) = resp.into_parts();
                self.head = Some(head);
                self.body = Some(body);
                self.body.as_mut().unwrap()
            }
        };

        loop {
            let message = try_ready!(body.poll()
                .map_err(|e| match e {
                    ::Error::Inner(h2) => ::Error::Grpc(::Status::from(h2)),
                    ::Error::Grpc(err) => ::Error::Grpc(err),
                }));

            match (self.message.is_some(), message) {
                (false, Some(msg)) => {
                    self.message = Some(msg);
                    continue;
                },
                (true, None) => {
                    let head = self.head.take().expect("polled more than once");
                    let body = self.message.take().expect("polled more than once");
                    let http = http::Response::from_parts(head, body);
                    let resp = ::Response::from_http(http);
                    return Ok(Async::Ready(resp));
                }
                (true, Some(_)) => {
                    debug!("Unary decoder found 2 messages");
                    return Err(::Error::Grpc(Status::UNKNOWN));
                }
                (false, None) => {
                    debug!("Unary decoder ended before any messages");
                    return Err(::Error::Grpc(Status::UNKNOWN));
                }
            }
        }
    }
}

// ====== impl Stream =====

impl<F, B> Streaming<F, B>
where
    F: Future<Item=::Response<B>>,
    B: Stream<Error=::Error<::h2::Error>>,
{
    pub fn map_future(future: F) -> Self {
        Streaming {
            body: None,
            future,
        }
    }
}

impl<F, B, E> Stream for Streaming<F, B>
where
    F: Future<Item=::Response<B>, Error=::Error<E>>,
    B: Stream<Error=::Error<::h2::Error>>,
{
    type Item = B::Item;
    type Error = ::Error<E>;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        loop {
            if let Some(ref mut body) = self.body {
                return body.poll().map_err(|e| match e {
                        ::Error::Inner(h2) => ::Error::Grpc(::Status::from(h2)),
                        ::Error::Grpc(err) => ::Error::Grpc(err),
                    });
            } else {
                let res = try_ready!(self.future.poll());
                self.body = Some(res.into_http().into_parts().1);
            }
        }
    }
}

// ====== impl BodyFuture =====

impl<F> BodyFuture<F> {
    /// Wrap the future.
    pub fn new(fut: F) -> Self {
        BodyFuture {
            future: fut,
        }
    }
}

impl<F, B> Future for BodyFuture<F>
where
    F: Future<Item=::Response<B>>,
{
    type Item = B;
    type Error = F::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let res = try_ready!(self.future.poll());
        Ok(Async::Ready(res.into_http().into_parts().1))
    }
}

fn check_grpc_status(trailers: &HeaderMap) -> Option<Status> {
    trailers.get("grpc-status").map(|s| {
        Status::from_bytes(s.as_ref())
    })
}
