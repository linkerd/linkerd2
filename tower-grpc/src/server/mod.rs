mod codec;
pub mod client_streaming;
pub mod server_streaming;
pub mod streaming;
pub mod unary;

pub use self::codec::{Codec, Encoder, Decoder, Decode, Encode};
pub use self::streaming::Grpc;
pub use self::client_streaming::ClientStreaming;
pub use self::server_streaming::ServerStreaming;
pub use self::unary::Unary;

use {Request, Response};

use futures::{Poll};
use futures::future::{self, FutureResult};
use tower::Service;

/// A gRPC service that responds to all requests with not implemented
#[derive(Debug)]
pub struct NotImplemented<T, U> {
    _p: ::std::marker::PhantomData<(T, U)>,
}

// ===== impl NotImplemented =====

impl<T, U> NotImplemented<T, U> {
    pub fn new() -> Self {
        NotImplemented {
            _p: ::std::marker::PhantomData,
        }
    }
}

impl<T, U> Service for NotImplemented<T, U>
{
    type Request = Request<T>;
    type Response = Response<U>;
    type Error = ::Error;
    type Future = FutureResult<Self::Response, Self::Error>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    fn call(&mut self, _: Self::Request) -> Self::Future {
        future::err(::Error::Grpc(::Status::UNIMPLEMENTED))
    }
}

impl<T, U> Clone for NotImplemented<T, U> {
    fn clone(&self) -> Self {
        NotImplemented {
            _p: ::std::marker::PhantomData,
        }
    }
}
