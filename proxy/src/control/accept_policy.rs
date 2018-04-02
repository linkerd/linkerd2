// use std;
// use std::collections::{HashSet, VecDeque};
// use std::collections::hash_map::{Entry, HashMap};
// use std::net::SocketAddr;
// use std::fmt;
// use std::mem;
use std::marker::PhantomData;

use futures::{Async, Future, Poll, Stream};
use futures_watch::Store;
use indexmap::IndexSet;
use ipnet::{Ipv4Net, Ipv6Net};
use prost::Message;
// use tower::Service;
use tower_h2::{HttpService, BoxBody, RecvBody};
use tower_grpc as grpc;

use conduit_proxy_controller_grpc::{accept_policy as pb};
use conduit_proxy_controller_grpc::accept_policy::client::{AcceptPolicy as Client};
use conduit_proxy_controller_grpc::common::ip_address;

use accept_policy::{self, EndpointMatch, Net};

/// A handle to start watching a destination for address changes.
pub struct AcceptPolicy<S>
where
    S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>
{
    inbound: StreamPublish<InboundStreamCall, S>,
    outbound: StreamPublish<OutboundStreamCall, S>,
}

/// Continually publishes messages from a stream response into a Watch.
///
/// If the stream terminates,
struct StreamPublish<C, S>
where
    C: StreamCall,
    S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
{
    rx: Rx<C::Message, S>,
    store: Store<C::Value>,
    _p: PhantomData<C>
}

/// A helper strategy for initiating a request.
///
/// Enables `Publish` to be generic over the type of response.
trait StreamCall {
    type Message: Message + Default;
    type Value;
    type Error: ::std::fmt::Debug;

    fn name() -> &'static str;

    fn rx<S>(svc: &mut S) -> Rx<Self::Message, S>
    where
        S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>;

    fn convert(msg: Self::Message) -> Result<Self::Value, Self::Error>;
}

#[derive(Debug, Default)]
struct InboundStreamCall;

#[derive(Debug, Default)]
struct OutboundStreamCall;

/// Receives a grpc stream asa stream of messages.
enum Rx<P, S: HttpService<ResponseBody = RecvBody>> {
    Init(grpc::client::server_streaming::ResponseFuture<P, S::Future>),
    Open(grpc::Streaming<P, S::ResponseBody>),
    Closed,
}

#[derive(Debug)]
enum RxError<T> {
    Init(grpc::Error<T>),
    Stream(grpc::Error),
}

impl<S> AcceptPolicy<S>
where
    S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
    S::Error: ::std::fmt::Debug,
{
    pub fn new(
        in_store: Store<accept_policy::Inbound>,
        out_store: Store<accept_policy::Outbound>
    ) -> Self {
        Self {
            inbound: StreamPublish::new(in_store),
            outbound: StreamPublish::new(out_store),
        }
    }

    pub fn poll_rpc(&mut self, svc: &mut S) {
        self.inbound.poll_rpc(svc);
        self.outbound.poll_rpc(svc);
    }
}

impl<C, S> StreamPublish<C, S>
where
    C: StreamCall,
    S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
    S::Error: ::std::fmt::Debug,
{
    fn new(store: Store<C::Value>) -> Self {
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
                Ok(Async::Ready(Some(msg))) => {
                    match C::convert(msg) {
                        Ok(val) => {
                            let _ = self.store.store(val);
                        }
                        Err(e) => {
                            error!("{} error: {:?}", C::name(), e);
                        }
                    }
                }
                Ok(Async::Ready(None)) => {
                    debug!("{} stream completed", C::name());
                    self.rx = C::rx::<S>(svc);
                }
                Err(e) => {
                    // TODO we probably shouldn't always retry immediately.
                    error!("{} error: {:?}", C::name(), e);
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
            let init_rsp = match *self {
                Rx::Init(ref mut p) => {
                    match p.poll() {
                        Ok(Async::Ready(rsp)) => Ok(rsp),
                        Err(e) => Err(RxError::Init(e)),
                        Ok(Async::NotReady) => return Ok(Async::NotReady),
                    }
                }
                Rx::Open(ref mut rx) => {
                    return rx.poll().map_err(RxError::Stream);
                }
                Rx::Closed => {
                    return Ok(Async::Ready(None));
                }
            };

            match init_rsp {
                Ok(rsp) => {
                    *self = Rx::Open(rsp.into_inner());
                }
                Err(e) => {
                    *self = Rx::Closed;
                    return Err(e);
                }
            }
        }
    }
}

impl StreamCall for InboundStreamCall {
    type Message = pb::InboundAcceptPolicy;
    type Value = accept_policy::Inbound;
    type Error = ConvertError;


    fn name() -> &'static str {
        "Inbound"
    }

    fn rx<S>(svc: &mut S) -> Rx<Self::Message, S>
    where
        S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>
    {
        let mut client = Client::new(svc.lift_ref());
        let req = pb::InboundAcceptPolicyRequest {};
        Rx::Init(client.inbound(grpc::Request::new(req)))
    }

    fn convert(msg: Self::Message) -> Result<Self::Value, Self::Error> {
        let mut protocol_detection_disabled = Vec::with_capacity(msg.detection_disabled.len());
        for em in &msg.detection_disabled {
            protocol_detection_disabled.push(convert_match(em)?);
        }
        Ok(::accept_policy::Inbound::new(protocol_detection_disabled))
    }
}

impl StreamCall for OutboundStreamCall {
    type Message = pb::OutboundAcceptPolicy;
    type Value = accept_policy::Outbound;
    type Error = ConvertError;

    fn name() -> &'static str {
        "Outbound"
    }

    fn rx<S>(svc: &mut S) -> Rx<Self::Message, S>
    where
        S: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>
    {
        let mut client = Client::new(svc.lift_ref());
        let req = pb::OutboundAcceptPolicyRequest {};
        Rx::Init(client.outbound(grpc::Request::new(req)))
    }

    fn convert(msg: Self::Message) -> Result<Self::Value, Self::Error> {
        let mut protocol_detection_disabled = Vec::with_capacity(msg.detection_disabled.len());
        for em in &msg.detection_disabled {
            protocol_detection_disabled.push(convert_match(em)?);
        }
        Ok(::accept_policy::Outbound::new(protocol_detection_disabled))
    }

}

#[derive(Copy, Clone, Debug)]
pub enum ConvertError {
    InvalidNetwork,
    InvalidPort,
}

fn convert_match(em: &pb::EndpointMatch) -> Result<EndpointMatch, ConvertError> {
    let net = match em.net {
        None => return Err(ConvertError::InvalidNetwork),
        Some(ref n) => convert_net(n)?,
    };

    let mut ports = IndexSet::with_capacity(em.ports.len());
    for p in &em.ports {
        if *p == 0 || *p > u32::from(::std::u16::MAX) {
            return Err(ConvertError::InvalidPort);
        }
        ports.insert(*p as u16);
    }

    Ok(::accept_policy::EndpointMatch::new(net, ports))
}

fn convert_net(m: &pb::endpoint_match::Net) -> Result<Net, ConvertError> {
    let mask = if m.mask > u32::from(::std::u8::MAX) {
        return Err(ConvertError::InvalidNetwork);
    } else {
        m.mask as u8
    };

    let ip = match m.ip.as_ref().and_then(|a| a.ip.as_ref()) {
        Some(ip) => ip,
        None => return Err(ConvertError::InvalidNetwork),
    };

    let net = match *ip {
        ip_address::Ip::Ipv4(ref n) => {
            let net = Ipv4Net::new((*n).into(), mask).map_err(|_| ConvertError::InvalidNetwork)?;
            Net::V4(net)
        }
        ip_address::Ip::Ipv6(ref ip6) => {
            let net = Ipv6Net::new(ip6.into(), mask).map_err(|_| ConvertError::InvalidNetwork)?;
            Net::V6(net)
        }
    };

    Ok(net)
}
