use {Request, Response};
use super::unary::Once;
use protobuf::server::ClientStreamingService;

use futures::{Future, Poll};
use tower::Service;

/// Maps to a client streaming gRPC service.
#[derive(Debug, Clone)]
pub struct ClientStreaming<T> {
    inner: T,
}

#[derive(Debug)]
pub struct ResponseFuture<T> {
    inner: T,
}

// ===== impl ClientStreaming =====

impl<T> ClientStreaming<T>
where T: ClientStreamingService,
{
    /// Return a new `ClientStreaming` gRPC service handler
    pub fn new(inner: T) -> Self {
        ClientStreaming { inner }
    }
}

impl<T> Service for ClientStreaming<T>
where T: ClientStreamingService,
{
    type Request = Request<T::RequestStream>;
    type Response = Response<Once<T::Response>>;
    type Error = ::Error;
    type Future = ResponseFuture<T::Future>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready()
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        let inner = self.inner.call(request);
        ResponseFuture { inner }
    }
}

// ===== impl ResponseFuture ======

impl<T, U> Future for ResponseFuture<T>
where T: Future<Item = Response<U>, Error = ::Error> {
    type Item = Response<Once<U>>;
    type Error = ::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let response = try_ready!(self.inner.poll());
        Ok(Once::map(response).into())
    }
}
