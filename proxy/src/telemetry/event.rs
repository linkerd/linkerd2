use std::sync::Arc;
use std::time::Duration;

use h2;

use ctx;

#[derive(Clone, Debug)]
pub enum Event {
    TransportOpen(Arc<ctx::transport::Ctx>),
    TransportClose(Arc<ctx::transport::Ctx>, TransportClose),

    StreamRequestOpen(Arc<ctx::http::Request>),
    StreamRequestFail(Arc<ctx::http::Request>, StreamRequestFail),

    StreamResponseOpen(Arc<ctx::http::Response>, StreamResponseOpen),
    StreamResponseFail(Arc<ctx::http::Response>, StreamResponseFail),
    StreamResponseEnd(Arc<ctx::http::Response>, StreamResponseEnd),
}

#[derive(Clone, Debug)]
pub struct TransportClose {
    /// Indicates that the transport was closed without error.
    // TODO include details.
    pub clean: bool,

    pub duration: Duration,

    // TODO
    //pub rx_bytes: usize,
    //pub tx_bytes: usize,
}

#[derive(Clone, Debug)]
pub struct StreamRequestFail {
    pub since_request_open: Duration,
    pub error: h2::Reason,
}

#[derive(Clone, Debug)]
pub struct StreamResponseOpen {
    pub since_request_open: Duration,
}

#[derive(Clone, Debug)]
pub struct StreamResponseFail {
    pub since_request_open: Duration,
    pub since_response_open: Duration,
    pub error: h2::Reason,
    pub bytes_sent: u64,
    pub frames_sent: u32,
}

#[derive(Clone, Debug)]
pub struct StreamResponseEnd {
    pub grpc_status: Option<u32>,
    pub since_request_open: Duration,
    pub since_response_open: Duration,
    pub bytes_sent: u64,
    pub frames_sent: u32,
}

// ===== impl Event =====

impl Event {
    pub fn is_http(&self) -> bool {
        match *self {
            Event::StreamRequestOpen(_) |
            Event::StreamRequestFail(_, _) |
            Event::StreamResponseOpen(_, _) |
            Event::StreamResponseFail(_, _) |
            Event::StreamResponseEnd(_, _) => true,
            _ => false,
        }
    }

    pub fn is_transport(&self) -> bool {
        match *self {
            Event::TransportOpen(_) | Event::TransportClose(_, _) => true,
            _ => false,
        }
    }

    pub fn proxy(&self) -> &Arc<ctx::Proxy> {
        match *self {
            Event::TransportOpen(ref ctx) | Event::TransportClose(ref ctx, _) => ctx.proxy(),
            Event::StreamRequestOpen(ref req) | Event::StreamRequestFail(ref req, _) => {
                &req.server.proxy
            }
            Event::StreamResponseOpen(ref rsp, _) |
            Event::StreamResponseFail(ref rsp, _) |
            Event::StreamResponseEnd(ref rsp, _) => &rsp.request.server.proxy,
        }
    }
}
