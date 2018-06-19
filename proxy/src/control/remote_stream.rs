use futures::{Future, Poll, Stream};
use http::HeaderMap;
use prost::Message;
use std::fmt;
use tower_grpc as grpc;

/// Tracks the state of a gRPC response stream from a remote.
///
/// A remote may hold a `Receiver` that can be used to read `M`-typed messages from the
/// remote stream.
pub enum Remote<M, E, F, S>
where
    F: Future<Item = grpc::Response<S>, Error = grpc::Error<E>>,
    S: Stream<Item = M, Error = grpc::Error>
{
    NeedsReconnect,
    ConnectedOrConnecting {
        rx: Receiver<M, E, F, S>
    },
}

/// Receives streaming RPCs updates.
///
/// Streaming gRPC endpoints return a `ResponseFuture` whose item is a `Response<Stream>`.
/// A `Receiver` holds the state of that RPC call, exposing a `Stream` that drives both
/// the gRPC response and its streaming body.
pub struct Receiver<M, E, F, S>
where
    F: Future<Item = grpc::Response<S>, Error = grpc::Error<E>>,
    S: Stream<Item = M, Error = grpc::Error>,
{
    rx: Rx<M, E, F, S>,
}

enum Rx<M, E, F, S>
where
    F: Future<Item = grpc::Response<S>, Error = grpc::Error<E>>,
    S: Stream<Item = M, Error = grpc::Error>,
{
    Waiting(F),
    Streaming(S),
}

// ===== impl Receiver =====

impl<M, E, F, S> Receiver<M, E, F, S>
where
    M: Message + Default,
    E: fmt::Debug,
    F: Future<Item = grpc::Response<S>, Error = grpc::Error<E>>,
    S: Stream<Item = M, Error = grpc::Error>
{
    pub fn new(future: F) -> Self {
        Receiver { rx: Rx::Waiting(future) }
    }

    // Coerces the stream's Error<()> to an Error<S::Error>.
    fn coerce_stream_err(e: grpc::Error<()>) -> grpc::Error<E> {
        match e {
            grpc::Error::Grpc(s, h) => grpc::Error::Grpc(s, h),
            grpc::Error::Decode(e) => grpc::Error::Decode(e),
            grpc::Error::Protocol(e) => grpc::Error::Protocol(e),
            grpc::Error::Inner(()) => {
                // `stream.poll` shouldn't return this variant. If it for
                // some reason does, we report this as an unknown error.
                warn!("unexpected gRPC stream error");
                debug_assert!(false);
                grpc::Error::Grpc(grpc::Status::UNKNOWN, HeaderMap::new())
            }
        }
    }
}

impl<M, E, F, S> Stream for Receiver<M, E, F ,S>
where
    M: Message + Default,
    E: fmt::Debug,
    F: Future<Item = grpc::Response<S>, Error = grpc::Error<E>>,
    S: Stream<Item = M, Error = grpc::Error>
{
    type Item = M;
    type Error = grpc::Error<E>;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        loop {
            let stream = match self.rx {
                Rx::Waiting(ref mut future) => {
                    try_ready!(future.poll()).into_inner()
                }

                Rx::Streaming(ref mut stream) => {
                    return stream.poll().map_err(Self::coerce_stream_err);
                }
            };

            self.rx = Rx::Streaming(stream);
        }
    }
}
