use {Request, Response};
use super::codec::{Codec, Encoder, Decode, Encode};

use {http, h2};
use futures::{Future, Stream, Poll, Async};
use tower::Service;
use tower_h2::RecvBody;

/// A bidirectional streaming gRPC service.
#[derive(Debug, Clone)]
pub struct Grpc<T, C> {
    inner: T,
    codec: C,
}

#[derive(Debug)]
pub struct ResponseFuture<T, E> {
    inner: T,
    encoder: Option<E>,
}

// ===== impl Grpc =====

impl<T, C, S> Grpc<T, C>
where T: Service<Request = Request<Decode<C::Decoder>>,
                Response = Response<S>,
                   Error = ::Error>,
      C: Codec,
      S: Stream<Item = C::Encode>,
{
    pub fn new(inner: T, codec: C) -> Self {
        Grpc {
            inner,
            codec,
        }
    }
}

impl<T, C, S> Service for Grpc<T, C>
where T: Service<Request = Request<Decode<C::Decoder>>,
                Response = Response<S>,
                   Error = ::Error>,
      C: Codec,
      S: Stream<Item = C::Encode>,
{
    type Request = http::Request<RecvBody>;
    type Response = http::Response<Encode<S, C::Encoder>>;
    type Error = h2::Error;
    type Future = ResponseFuture<T::Future, C::Encoder>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready().map_err(|_| unimplemented!())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        // Map the request body
        let (head, body) = request.into_parts();

        // Wrap the body stream with a decoder
        let body = Decode::new(body, self.codec.decoder());

        // Reconstruct the HTTP request
        let request = http::Request::from_parts(head, body);

        // Convert the HTTP request to a gRPC request
        let request = Request::from_http(request);

        // Send the request to the inner service
        let inner = self.inner.call(request);

        // Return the response
        ResponseFuture {
            inner,
            encoder: Some(self.codec.encoder()),
        }
    }
}

// ===== impl ResponseFuture =====

impl<T, E, S> Future for ResponseFuture<T, E>
where T: Future<Item = Response<S>,
               Error = ::Error>,
      E: Encoder,
      S: Stream<Item = E::Item>,
{
    type Item = http::Response<Encode<S, E>>;
    type Error = h2::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        // Get the gRPC response
        let response = match self.inner.poll() {
            Ok(Async::Ready(response)) => response,
            Ok(Async::NotReady) => return Ok(Async::NotReady),
            Err(e) => {
                match e {
                    ::Error::Grpc(status) => {
                        let response = Response::new(Encode::error(status));
                        return Ok(response.into_http().into());
                    }
                    // TODO: Is this correct?
                    _ => return Err(h2::Reason::INTERNAL_ERROR.into()),
                }
            }
        };

        // Convert to an HTTP response
        let response = response.into_http();

        // Map the response body
        let (head, body) = response.into_parts();

        // Get the encoder
        let encoder = self.encoder.take().expect("encoder consumed");

        // Encode the body
        let body = Encode::new(body, encoder);

        // Success
        Ok(http::Response::from_parts(head, body).into())
    }
}
