use http;
use std::sync::Arc;

use ctx;
use control::destination;
use telemetry::metrics::DstLabels;

/// Describes a stream's request headers.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct Request {
    // A numeric ID useful for debugging & correlation.
    pub id: usize,

    pub uri: http::Uri,
    pub method: http::Method,

    /// Identifies the proxy server that received the request.
    pub server: Arc<ctx::transport::Server>,

    /// Identifies the proxy client that dispatched the request.
    pub client: Arc<ctx::transport::Client>,
}

/// Describes a stream's response headers.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
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

impl Request {
    pub fn new<B>(
        request: &http::Request<B>,
        server: &Arc<ctx::transport::Server>,
        client: &Arc<ctx::transport::Client>,
        id: usize,
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
