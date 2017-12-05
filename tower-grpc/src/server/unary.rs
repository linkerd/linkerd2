use super::server_streaming::{self, ServerStreaming};

use {Request, Response};
use protobuf::server::UnaryService;

use futures::{Future, Stream, Poll};
use tower::Service;

/// Maps to a unary gRPC service.
#[derive(Debug)]
pub struct Unary<T, S> {
    inner: ServerStreaming<Inner<T>, S>,
}

pub struct ResponseFuture<T, S>
where T: UnaryService,
      S: Stream,
{
    inner: server_streaming::ResponseFuture<Inner<T>, S>,
}


#[derive(Debug)]
pub struct Once<T> {
    inner: Option<T>,
}

/// Maps inbound requests
#[derive(Debug, Clone)]
struct Inner<T>(pub T);
struct InnerFuture<T>(T);

// ===== impl Unary =====

impl<T, S, U> Unary<T, S>
where T: UnaryService<Request = S::Item, Response = U>,
      S: Stream<Error = ::Error>,
{
    /// Return a new `Unary` gRPC service handler
    pub fn new(inner: T) -> Self {
        let inner = ServerStreaming::new(Inner(inner));
        Unary { inner }
    }
}

impl<T, S, U> Service for Unary<T, S>
where T: UnaryService<Request = S::Item, Response = U>,
      S: Stream<Error = ::Error>,
{
    type Request = Request<S>;
    type Response = ::Response<Once<U>>;
    type Error = ::Error;
    type Future = ResponseFuture<T, S>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Service::poll_ready(&mut self.inner)
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        let inner = Service::call(&mut self.inner, request);
        ResponseFuture { inner }
    }
}

impl<T, S> Clone for Unary<T, S>
where T: Clone,
{
    fn clone(&self) -> Self {
        Unary { inner: self.inner.clone() }
    }
}

// ===== impl Inner =====

impl<T> Service for Inner<T>
where T: UnaryService,
{
    type Request = Request<T::Request>;
    type Response = Response<Once<T::Response>>;
    type Error = ::Error;
    type Future = InnerFuture<T::Future>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.0.poll_ready()
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        let inner = self.0.call(request);
        InnerFuture(inner)
    }
}

// ===== impl ResponseFuture ======

impl<T, S, U> Future for ResponseFuture<T, S>
where T: UnaryService<Request = S::Item, Response = U>,
      S: Stream<Error = ::Error>,
{
    type Item = Response<Once<U>>;
    type Error = ::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        self.inner.poll()
    }
}

// ===== impl InnerFuture ======

impl<T, U> Future for InnerFuture<T>
where T: Future<Item = Response<U>, Error = ::Error> {
    type Item = Response<Once<U>>;
    type Error = ::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let response = try_ready!(self.0.poll());
        Ok(Once::map(response).into())
    }
}

// ===== impl Once =====

impl<T> Once<T> {
    /// Map a response to a response of a `Once` stream
    pub(super) fn map(response: Response<T>) -> Response<Self> {
        // A bunch of junk to map the body type
        let http = response.into_http();
        let (head, body) = http.into_parts();

        // Wrap with `Once`
        let body = Once { inner: Some(body) };

        let http = ::http::Response::from_parts(head, body);
        Response::from_http(http)
    }
}

impl<T> Stream for Once<T> {
    type Item = T;
    type Error = ::Error;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        Ok(self.inner.take().into())
    }
}
