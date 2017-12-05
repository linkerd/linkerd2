use std::fmt::Debug;
use std::marker::PhantomData;

use futures::{Future, Poll};
use h2;
use http;
use tower::Service;

/// Map an HTTP service's error to an appropriate 500 response.
pub struct MapErr<T, E> {
    inner: T,
    _p: PhantomData<E>,
}

/// Catches errors from the inner future and maps them to 500 responses.
pub struct ResponseFuture<T, E> {
    inner: T,
    _p: PhantomData<E>,
}

// ===== impl MapErr =====

impl<T, E> MapErr<T, E>
where
    T: Service<Error = E>,
    E: Debug,
{
    /// Crete a new `MapErr`
    pub fn new(inner: T) -> Self {
        MapErr {
            inner,
            _p: PhantomData,
        }
    }
}

impl<T, B, E> Service for MapErr<T, E>
where
    T: Service<Response = http::Response<B>, Error = E>,
    B: Default,
    E: Debug,
{
    type Request = T::Request;
    type Response = T::Response;
    type Error = h2::Error;
    type Future = ResponseFuture<T::Future, E>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready()
            // TODO: Do something with the original error
            .map_err(|_| h2::Reason::INTERNAL_ERROR.into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        let inner = self.inner.call(request);
        ResponseFuture {
            inner,
            _p: PhantomData,
        }
    }
}

// ===== impl ResponseFuture =====

impl<T, B, E> Future for ResponseFuture<T, E>
where
    T: Future<Item = http::Response<B>, Error = E>,
    B: Default,
    E: Debug,
{
    type Item = T::Item;
    type Error = h2::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        self.inner.poll().or_else(|e| {
            error!("turning h2 error into 500: {:?}", e);
            let response = http::Response::builder()
                .status(500)
                .body(Default::default())
                .unwrap();

            Ok(response.into())
        })
    }
}
