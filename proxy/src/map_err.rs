use std::marker::PhantomData;
use std::sync::Arc;

use futures::{Future, Poll};
use h2;
use http;
use http::header::CONTENT_LENGTH;
use tower_service::Service;

/// Map an HTTP service's error to an appropriate 500 response.
pub struct MapErr<T, E, F> {
    inner: T,
    f: Arc<F>,
    _p: PhantomData<E>,
}

/// Catches errors from the inner future and maps them to 500 responses.
pub struct ResponseFuture<T, E, F> {
    inner: T,
    f: Arc<F>,
    _p: PhantomData<E>,
}


// ===== impl MapErr =====

impl<T, E, F> MapErr<T, E, F>
where
    T: Service<Error = E>,
    F: Fn(E) -> http::StatusCode,
{
    /// Crete a new `MapErr`
    pub fn new(inner: T, f: F) -> Self {
        MapErr {
            inner,
            f: Arc::new(f),
            _p: PhantomData,
        }
    }
}

impl<T, B, E, F> Service for MapErr<T, E, F>
where
    T: Service<Response = http::Response<B>, Error = E>,
    B: Default,
    F: Fn(E) -> http::StatusCode,
{
    type Request = T::Request;
    type Response = T::Response;
    type Error = h2::Error;
    type Future = ResponseFuture<T::Future, E, F>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready()
            // TODO: Do something with the original error
            .map_err(|_| h2::Reason::INTERNAL_ERROR.into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        let inner = self.inner.call(request);
        ResponseFuture {
            inner,
            f: self.f.clone(),
            _p: PhantomData,
        }
    }
}

// ===== impl ResponseFuture =====

impl<T, B, E, F> Future for ResponseFuture<T, E, F>
where
    T: Future<Item = http::Response<B>, Error = E>,
    B: Default,
    F: Fn(E) -> http::StatusCode,
{
    type Item = T::Item;
    type Error = h2::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        self.inner.poll().or_else(|e| {
            let status = (self.f)(e);
            let response = http::Response::builder()
                .status(status)
                .header(CONTENT_LENGTH, "0")
                .body(Default::default())
                .unwrap();

            Ok(response.into())
        })
    }
}
