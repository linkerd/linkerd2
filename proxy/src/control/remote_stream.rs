
use futures::{Future, Poll, Stream};
use prost::Message;
use std::fmt;
use tower_h2::{HttpService, Body, Data};
use tower_grpc::{
    self as grpc,
    Streaming,
    client::server_streaming::ResponseFuture,
};

/// Tracks the state of a gRPC response stream from a remote.
///
/// A remote may hold a `Receiver` that can be used to read `M`-typed messages from the
/// remote stream.
pub enum Remote<M, S: HttpService> {
    NeedsReconnect,
    ConnectedOrConnecting {
        rx: Receiver<M, S>
    },
}

/// Receives streaming RPCs updates.
///
/// Streaming gRPC endpoints return a `ResponseFuture` whose item is a `Response<Stream>`.
/// A `Receiver` holds the state of that RPC call, exposing a `Stream` that drives both
/// the gRPC response and its streaming body.
pub struct Receiver<M, S: HttpService>(Rx<M, S>);

enum Rx<M, S: HttpService> {
    Waiting(ResponseFuture<M, S::Future>),
    Streaming(Streaming<M, S::ResponseBody>),
}

// ===== impl Remote =====

impl<M: Message + Default, S: HttpService> Remote<M, S> {
    pub fn connecting(future: ResponseFuture<M, S::Future>) -> Self {
        Remote::ConnectedOrConnecting {
            rx: Receiver(Rx::Waiting(future))
        }
    }

    pub fn connected(rx: Receiver<M, S>) -> Self {
        Remote::ConnectedOrConnecting { rx }
    }
}

// ===== impl Receiver =====

impl<M: Message + Default, S: HttpService> Stream for Receiver<M, S>
where
    S::ResponseBody: Body<Data = Data>,
    S::Error: fmt::Debug,
{
    type Item = M;
    type Error = grpc::Error<S::Error>;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        loop {
            let stream = match self.0 {
                Rx::Waiting(ref mut future) => {
                    try_ready!(future.poll()).into_inner()
                }

                Rx::Streaming(ref mut stream) => {
                    return stream.poll().map_err(|e| grpc::Error::Grpc(match e {
                        grpc::Error::Inner(()) => grpc::Status::UNKNOWN,
                        grpc::Error::Grpc(status) => status,
                    }));
                }
            };

            self.0 = Rx::Streaming(stream);
        }
    }
}
