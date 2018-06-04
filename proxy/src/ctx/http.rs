use http;
use std::sync::Arc;

use ctx;
use control::destination;
use telemetry::metrics::DstLabels;

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

impl From<usize> for RequestId {
    fn from(value: usize) -> Self {
        RequestId(value)
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
        id: RequestId,
    ) -> Arc<Self> {
        let r = Self {
            id,
            uri: request.uri().clone(),
            method: request.method().clone(),
            server: Arc::clone(server),
            client: Arc::clone(client),
        };

        Arc::new(r)
    }

    pub fn tls_identity(&self) -> Option<&destination::TlsIdentity> {
        self.client.tls_identity()
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

    pub fn tls_identity(&self) -> Option<&destination::TlsIdentity> {
        self.request.tls_identity()
    }

    pub fn dst_labels(&self) -> Option<&DstLabels> {
        self.request.dst_labels()
    }
}
