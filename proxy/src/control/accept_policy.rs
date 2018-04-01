// use std;
// use std::collections::{HashSet, VecDeque};
// use std::collections::hash_map::{Entry, HashMap};
// use std::net::SocketAddr;
// use std::fmt;
// use std::mem;
use std::marker::PhantomData;

use futures::{Async, Future, Poll, Stream};
use futures_watch::Store;
use prost::Message;
// use tower::Service;
use tower_h2::{HttpService, BoxBody, RecvBody};
use tower_grpc as grpc;

use conduit_proxy_controller_grpc::accept_policy::{
    InboundAcceptPolicy,
    InboundAcceptPolicyRequest,
    OutboundAcceptPolicy,
    OutboundAcceptPolicyRequest,
};
use conduit_proxy_controller_grpc::accept_policy::client::{AcceptPolicy as Client};

/// A handle to start watching a destination for address changes.
pub struct AcceptPolicy<S>
where
    S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>
{
    inbound: Policy<InboundKind, S>,
    outbound: Policy<OutboundKind, S>,
}

struct Policy<K, S>
where
    K: Kind,
    S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
{
    rx: Rx<K::Message, S>,
    store: Store<K::Message>,
    _p: PhantomData<K>
}

trait Kind {
    type Message: Message + Default;

    fn stream<S>(svc: &mut S)
        -> grpc::client::server_streaming::ResponseFuture<Self::Message, S::Future>
    where
        S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>;
}

struct InboundKind;
struct OutboundKind;

enum Rx<P, S: HttpService<ResponseBody = RecvBody>> {
    Pending(grpc::client::server_streaming::ResponseFuture<P, S::Future>),
    Open(grpc::Streaming<P, S::ResponseBody>),
    Closed,
}

#[derive(Debug)]
enum RxError<T> {
    Response(grpc::Error<T>),
    Stream(grpc::Error),
}

impl<S> AcceptPolicy<S>
where
    S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
    S::Error: ::std::fmt::Debug,
{
    pub fn new(
        in_store: Store<InboundAcceptPolicy>,
        out_store: Store<OutboundAcceptPolicy>
    ) -> Self {
        Self {
            inbound: Policy::new(in_store),
            outbound: Policy::new(out_store),
        }
    }

    pub fn poll_rpc(&mut self, svc: &mut S) {
        self.inbound.poll_rpc(svc);
        self.outbound.poll_rpc(svc);
    }
}

impl<K, S> Policy<K, S>
where
    K: Kind,
    S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
    S::Error: ::std::fmt::Debug,
{
    fn new(store: Store<K::Message>) -> Self {
        Self {
            store,
            rx: Rx::Closed,
            _p: PhantomData
        }
    }

    fn poll_rpc(&mut self, svc: &mut S) {
        loop {
            match self.rx.poll() {
                Ok(Async::NotReady) => return,
                Ok(Async::Ready(Some(message))) => {
                    let _ = self.store.store(message);
                }
                Ok(Async::Ready(None)) => {
                    self.rx = Rx::Pending(K::stream::<S>(svc));
                }
                Err(e) => {
                    error!("accept_policy error: {:?}", e);
                    self.rx = Rx::Pending(K::stream::<S>(svc));
                }
            }
        }
    }
}

impl<P, S> Stream for Rx<P, S>
where
    P: Message + Default,
    S: HttpService<ResponseBody = RecvBody>,
{
    type Item = P;
    type Error = RxError<S::Error>;

    fn poll(&mut self) -> Poll<Option<P>, Self::Error> {
        loop {
            let stream = match *self {
                Rx::Open(ref mut rx) => {
                    return rx.poll().map_err(RxError::Stream);
                }
                Rx::Pending(ref mut p) => {
                    match p.poll() {
                        Ok(Async::NotReady) => return Ok(Async::NotReady),
                        Ok(Async::Ready(rsp)) => Ok(rsp.into_inner()),
                        Err(e) => Err(RxError::Response(e)),
                    }
                }
                Rx::Closed => {
                    return Ok(Async::Ready(None));
                }
            };

            match stream {
                Ok(s) => {
                    *self = Rx::Open(s);
                }
                Err(e) => {
                    *self = Rx::Closed;
                    return Err(e);
                }
            }
        }
    }
}

impl Kind for InboundKind {
    type Message = InboundAcceptPolicy;

    fn stream<S>(svc: &mut S)
        -> grpc::client::server_streaming::ResponseFuture<Self::Message, S::Future>
    where
        S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>
    {
        let mut client = Client::new(svc.lift_ref());
        let req = InboundAcceptPolicyRequest {};
        client.inbound(grpc::Request::new(req))
    }
}

impl Kind for OutboundKind {
    type Message = OutboundAcceptPolicy;

    fn stream<S>(svc: &mut S)
        -> grpc::client::server_streaming::ResponseFuture<Self::Message, S::Future>
    where
        S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>
    {
        let mut client = Client::new(svc.lift_ref());
        let req = OutboundAcceptPolicyRequest {};
        client.outbound(grpc::Request::new(req))
    }
}
