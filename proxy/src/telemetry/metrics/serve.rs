use deflate::CompressionOptions;
use deflate::write::GzEncoder;
use futures::future::{self, FutureResult};
use hyper::{self, Body, StatusCode};
use hyper::header::{AcceptEncoding, ContentEncoding, ContentType, Encoding, QualityItem};
use hyper::server::{Request, Response, Service};
use std::io::Write;
use std::sync::{Arc, Mutex};

use super::Root;

/// Serve Prometheues metrics.
#[derive(Debug, Clone)]
pub struct Serve {
    metrics: Arc<Mutex<Root>>,
}

// ===== impl Serve =====

impl Serve {
    pub(super) fn new(metrics: &Arc<Mutex<Root>>) -> Self {
        Serve {
            metrics: metrics.clone(),
        }
    }

    fn is_gzip(req: &Request) -> bool {
        if let Some(accept_encodings) = req
            .headers()
            .get::<AcceptEncoding>()
        {
            return accept_encodings
                .iter()
                .any(|&QualityItem { ref item, .. }| item == &Encoding::Gzip)
        }
        false
    }
}

impl Service for Serve {
    type Request = Request;
    type Response = Response;
    type Error = hyper::Error;
    type Future = FutureResult<Self::Response, Self::Error>;

    fn call(&self, req: Self::Request) -> Self::Future {
        if req.path() != "/metrics" {
            return future::ok(Response::new()
                .with_status(StatusCode::NotFound));
        }

        let metrics = self.metrics.lock()
            .expect("metrics lock poisoned");

        let resp = if Self::is_gzip(&req) {
            trace!("gzipping metrics");
            let mut writer = GzEncoder::new(Vec::<u8>::new(), CompressionOptions::fast());
            write!(&mut writer, "{}", *metrics)
                .and_then(|_| writer.finish())
                .map(|body| {
                    Response::new()
                        .with_header(ContentEncoding(vec![Encoding::Gzip]))
                        .with_header(ContentType::plaintext())
                        .with_body(Body::from(body))
                })
        } else {
            let mut writer = Vec::<u8>::new();
            write!(&mut writer, "{}", *metrics)
                .map(|_| {
                    Response::new()
                        .with_header(ContentType::plaintext())
                        .with_body(Body::from(writer))
                })
        };

        future::result(resp.map_err(hyper::Error::Io))
    }
}
