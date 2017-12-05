use {Request, Response};
use protobuf::server::ServerStreamingService;

use futures::{Future, Stream, Poll};
use tower::Service;

/// Maps to a server streaming gRPC service.
#[derive(Debug)]
pub struct ServerStreaming<T, S> {
    /// Inner service blueprint. This is used as the source to clone from.
    inner: T,

    /// The clone that will be used to handle the next request.
    clone: T,

    /// Making the rustc compiler happy since 2014.
    _p: ::std::marker::PhantomData<S>,
}

pub struct ResponseFuture<T, S>
where T: ServerStreamingService,
      S: Stream,
{
    inner: T,
    state: Option<State<T::Future, S>>,
}

enum State<T, S> {
    /// Waiting for the request to be received
    Requesting(Request<S>),

    /// Waiting for the response future to resolve
    Responding(T),
}

// ===== impl ServerStreaming =====

impl<T, S, U> ServerStreaming<T, S>
where T: ServerStreamingService<Request = S::Item, Response = U>,
      S: Stream<Error = ::Error>,
{
    /// Return a new `ServerStreaming` gRPC service handler
    pub fn new(inner: T) -> Self {
        let clone = inner.clone();

        ServerStreaming {
            inner,
            clone,
            _p: ::std::marker::PhantomData,
        }
    }
}

impl<T, S> Service for ServerStreaming<T, S>
where T: ServerStreamingService<Request = S::Item>,
      S: Stream<Error = ::Error>,
{
    type Request = Request<S>;
    type Response = Response<T::ResponseStream>;
    type Error = ::Error;
    type Future = ResponseFuture<T, S>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        // Ensure that the clone is ready to process the request.
        self.clone.poll_ready()
            .map_err(|_| unimplemented!())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        use std::mem;

        // Create a new clone to replace the old one.
        let inner = mem::replace(&mut self.clone, self.inner.clone());

        ResponseFuture {
            inner,
            state: Some(State::Requesting(request)),
        }
    }
}

impl<T, S> Clone for ServerStreaming<T, S>
where T: Clone,
{
    fn clone(&self) -> Self {
        let inner = self.inner.clone();
        let clone = inner.clone();

        ServerStreaming {
            inner,
            clone,
            _p: ::std::marker::PhantomData,
        }
    }
}

// ===== impl ResponseFuture ======

impl<T, S> Future for ResponseFuture<T, S>
where T: ServerStreamingService<Request = S::Item>,
      S: Stream<Error = ::Error>,
{
    type Item = Response<T::ResponseStream>;
    type Error = ::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        use self::State::*;

        loop {
            let msg = match *self.state.as_mut().unwrap() {
                Requesting(ref mut request) => {
                    try_ready!(request.get_mut().poll())
                }
                Responding(ref mut fut) => {
                    return fut.poll();
                }
            };

            match msg {
                Some(msg) => {
                    match self.state.take().unwrap() {
                        Requesting(request) => {
                            // A bunch of junk to map the body type
                            let http = request.into_http();
                            let (head, _) = http.into_parts();

                            let http = ::http::Request::from_parts(head, msg);
                            let request = Request::from_http(http);

                            let response = self.inner.call(request);

                            self.state = Some(Responding(response));
                        }
                        _ => unreachable!(),
                    }
                }
                None => {
                    // TODO: Do something
                    return Err(::Error::Inner(()));
                }
            }
        }
    }
}
