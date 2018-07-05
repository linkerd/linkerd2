use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime};

use h2;

use ctx;

#[derive(Clone, Debug)]
pub enum Event {
    TransportOpen(Arc<ctx::transport::Ctx>),
    TransportClose(Arc<ctx::transport::Ctx>, TransportClose),

    StreamRequestOpen(Arc<ctx::http::Request>),
    StreamRequestFail(Arc<ctx::http::Request>, StreamRequestFail),
    StreamRequestEnd(Arc<ctx::http::Request>, StreamRequestEnd),

    StreamResponseOpen(Arc<ctx::http::Response>, StreamResponseOpen),
    StreamResponseFail(Arc<ctx::http::Response>, StreamResponseFail),
    StreamResponseEnd(Arc<ctx::http::Response>, StreamResponseEnd),

    TlsConfigReloaded(SystemTime),
    TlsConfigReloadFailed(::transport::tls::ConfigError),
}

#[derive(Clone, Debug)]
pub struct TransportClose {
    /// Indicates that the transport was closed without error.
    // TODO include details.
    pub clean: bool,

    pub duration: Duration,

    pub rx_bytes: u64,
    pub tx_bytes: u64,
}

#[derive(Clone, Debug)]
pub struct StreamRequestFail {
    pub request_open_at: Instant,
    pub request_fail_at: Instant,
    pub error: h2::Reason,
}

#[derive(Clone, Debug)]
pub struct StreamRequestEnd {
    pub request_open_at: Instant,
    pub request_end_at: Instant,
}

#[derive(Clone, Debug)]
pub struct StreamResponseOpen {
    pub request_open_at: Instant,
    pub response_open_at: Instant,
}

#[derive(Clone, Debug)]
pub struct StreamResponseFail {
    pub request_open_at: Instant,
    pub response_open_at: Instant,
    pub response_first_frame_at: Option<Instant>,
    pub response_fail_at: Instant,
    pub error: h2::Reason,
    pub bytes_sent: u64,
    pub frames_sent: u32,
}

#[derive(Clone, Debug)]
pub struct StreamResponseEnd {
    pub request_open_at: Instant,
    pub response_open_at: Instant,
    pub response_first_frame_at: Instant,
    pub response_end_at: Instant,
    pub grpc_status: Option<u32>,
    pub bytes_sent: u64,
    pub frames_sent: u32,
}

// ===== impl Event =====

impl Event {
    pub fn is_http(&self) -> bool {
        match *self {
            Event::StreamRequestOpen(_) |
            Event::StreamRequestFail(_, _) |
            Event::StreamRequestEnd(_, _) |
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
}
