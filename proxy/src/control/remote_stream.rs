
use futures::{Future, Poll, Stream};
use http;
use prost::Message;
use std::fmt;
use tower_h2::{Body, Data, RecvBody};
use tower_grpc::{
    self as grpc,
    Streaming,
    client::server_streaming::ResponseFuture,
};

/// Tracks the state of a remote response stream.
///
/// A remote may hold a `Receiver` that can be used to read `M`-typed messages from the
/// remote stream.
///
/// If the Remote does not hold an active `Receiver`, `needs_reconnect()` returns true and
/// `take_receiver()` returns `None`.
#[derive(Debug)]
pub enum Remote<M, F, B: Body = RecvBody> {
    NeedsReconnect,
    ConnectedOrConnecting {
        rx: Receiver<M, F, B>
    },
}

/// Receives streaming RPCs updates.
///
/// Streaming gRPC endpoints return a `ResponseFuture` whose item is a `Response<Stream>`.
/// A `Receiver` holds the state of that RPC call, exposing a `Stream` that drives both
/// the gRPC response and its streaming body.
#[derive(Debug)]
pub struct Receiver<M, F, B: Body = RecvBody>(Rx<M, F, B>);

#[derive(Debug)]
enum Rx<M, F, B: Body = RecvBody> {
    Waiting(ResponseFuture<M, F>),
    Streaming(Streaming<M, B>),
}

/// Wraps the error types returned by `Receiver` polls.
///
/// A `Receiver` error is either the error type of the response future or that of the open
/// stream.
#[derive(Debug)]
pub enum Error<T> {
    Future(grpc::Error<T>),
    Stream(grpc::Error),
}

// ===== impl Remote =====

impl<M, F, B> Remote<M, F, B>
where
    M: Message + Default,
    B: Body<Data = Data>,
    F: Future<Item = http::Response<B>>,
{
    pub fn new() -> Self {
        Remote::NeedsReconnect
    }

    pub fn from_future(future: ResponseFuture<M, F>) -> Self {
        Remote::ConnectedOrConnecting {
            rx: Receiver(Rx::Waiting(future))
        }
    }

    pub fn from_receiver(rx: Receiver<M, F, B>) -> Self {
        Remote::ConnectedOrConnecting { rx }
    }

    /// Returns true if there is not an active `Receiver` on this `Remote`..
    pub fn needs_reconnect(&self) -> bool {
        match *self {
            Remote::NeedsReconnect => true,
            _ => false,
        }
    }

    /// Consumes the `Remote`, returning a `Receiver` if one is active.
    pub fn into_receiver_maybe(self) -> Option<Receiver<M, F, B>> {
        match self {
            Remote::NeedsReconnect => None,
            Remote::ConnectedOrConnecting { rx } => Some(rx),
        }
    }
}

// ===== impl Receiver =====

impl<M, F, B> Stream for Receiver<M, F, B>
where
    M: Message + Default,
    B: Body<Data = Data>,
    F: Future<Item = http::Response<B>>,
    F::Error: fmt::Debug,
{
    type Item = M;
    type Error = Error<F::Error>;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        loop {
            let stream = match self.0 {
                Rx::Waiting(ref mut future) => {
                    let rsp = future.poll().map_err(Error::Future);
                    try_ready!(rsp).into_inner()
                }

                Rx::Streaming(ref mut stream) => {
                    return stream.poll().map_err(Error::Stream);
                }
            };

            self.0 = Rx::Streaming(stream);
        }
    }
}
