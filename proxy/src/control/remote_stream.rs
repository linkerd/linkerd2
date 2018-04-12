use std::fmt;

use futures::{Async, Future, Poll, Stream};
use http;
use prost::Message;
use tower_h2::{Body, Data, RecvBody};
use tower_grpc::{
    self as grpc,
    Streaming,
    client::server_streaming::ResponseFuture
};

pub enum Remote<M: Message + Default, F, B: Body = RecvBody> {
    NeedsReconnect,
    ConnectedOrConnecting {
        rx: Receiver<M, F, B>
    },
}

/// Receiver for destination set updates.
///
/// The destination RPC returns a `ResponseFuture` whose item is a
/// `Response<Stream>`, so this type holds the state of that RPC call ---
/// either we're waiting for the future, or we have a stream --- and allows
/// us to implement `Stream` regardless of whether the RPC has returned yet
/// or not.
///
/// Polling an `Receiver` polls the wrapped future while we are
/// `Waiting`, and the `Stream` if we are `Streaming`. If the future is `Ready`,
/// then we switch states to `Streaming`.
pub enum Receiver<M: Message + Default, F, B: Body = RecvBody> {
    Waiting(ResponseFuture<M, F>),
    Streaming(Streaming<M, B>),
}


/// Wraps the error types returned by `Receiver` polls.
///
/// An `Receiver` error is either the error type of the `Future` in the
/// `Receiver::Waiting` state, or the `Stream` in the `Receiver::Streaming`
/// state.
#[derive(Debug)]
pub enum Error<T> {
    Future(grpc::Error<T>),
    Stream(grpc::Error),
}

// ===== impl Receiver =====

impl<M, F, B> Stream for Receiver<M, F, B>
where M: Message + Default,
      B: Body<Data = Data>,
      F: Future<Item = http::Response<B>>,
      F::Error: fmt::Debug,
{
    type Item = M;
    type Error = Error<F::Error>;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        // this is not ideal.
        let stream = match *self {
            Receiver::Waiting(ref mut future) => match future.poll() {
                Ok(Async::Ready(response)) => response.into_inner(),
                Ok(Async::NotReady) => return Ok(Async::NotReady),
                Err(e) => return Err(Error::Future(e)),
            },
            Receiver::Streaming(ref mut stream) =>
                return stream.poll().map_err(Error::Stream),
        };
        *self = Receiver::Streaming(stream);
        self.poll()
    }
}
