use http;
use std::sync::{Arc, atomic::AtomicUsize};

use ctx;
use telemetry::metrics::DstLabels;
use std::sync::atomic::Ordering;
use transport::tls;
use conditional::Conditional;


/// A `RequestId` can be mapped to a `u64`. No `RequestId`s will map to the
/// same value within a process.
///
/// XXX `usize` is too small except on 64-bit platforms. TODO: Use `u64` when
/// `AtomicU64` becomes stable.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub struct RequestId(usize);

/// Describes a stream's request headers.
#[derive(Debug)]
pub struct Request {
    // A numeric ID useful for debugging & correlation.
    pub id: RequestId,

    pub uri: http::Uri,
    pub method: http::Method,

    /// Identifies the proxy server that received the request.
    pub server: Arc<ctx::transport::Server>,

    /// Identifies the proxy client that dispatched the request.
    pub client: Arc<ctx::transport::Client>,
}

/// Describes a stream's response headers.
#[derive(Debug)]
pub struct Response {
    pub request: Arc<Request>,

    pub status: http::StatusCode,
}

// TODO Describe a request's EOS.
//pub struct EndRequest {
//    pub response: Arc<Request>,
//
//    pub h2_error_code: Option<u32>,
//}

impl RequestId {
    fn next() -> Self {
        static NEXT_REQUEST_ID: AtomicUsize = AtomicUsize::new(0);
        RequestId(NEXT_REQUEST_ID.fetch_add(1, Ordering::SeqCst))
    }
}

impl Into<u64> for RequestId {
    fn into(self) -> u64 {
        self.0 as u64
    }
}

impl Request {
    pub fn new<B>(
        request: &http::Request<B>,
        server: &Arc<ctx::transport::Server>,
        client: &Arc<ctx::transport::Client>,
    ) -> Arc<Self> {
        let r = Self {
            id: RequestId::next(),
            uri: request.uri().clone(),
            method: request.method().clone(),
            server: Arc::clone(server),
            client: Arc::clone(client),
        };

        Arc::new(r)
    }

    pub fn tls_identity(&self) -> Conditional<&tls::Identity, tls::ReasonForNoIdentity> {
        self.client.tls_identity()
    }

    /// Returns a `TlsStatus` indicating if the request was sent was over TLS.
    pub fn tls_status(&self) -> ctx::transport::TlsStatus {
        if self.server.proxy.is_outbound() {
            // If the request is in the outbound direction, then we opened the
            // client connection, so check if it was secured.
            self.client.tls_status
        } else {
            // Otherwise, the request is inbound, so check if we accepted it
            // over TLS.
            self.server.tls_status
        }
    }

    pub fn dst_labels(&self) -> Option<&DstLabels> {
        self.client.dst_labels()
    }
}

impl Response {
    pub fn new<B>(response: &http::Response<B>, request: &Arc<Request>) -> Arc<Self> {
        let r = Self {
            status: response.status(),
            request: Arc::clone(request),
        };

        Arc::new(r)
    }

    /// Returns a `TlsStatus` indicating if the response was sent was over TLS.
    pub fn tls_status(&self) -> ctx::transport::TlsStatus {
        self.request.tls_status()
    }

    pub fn dst_labels(&self) -> Option<&DstLabels> {
        self.request.dst_labels()
    }
}
