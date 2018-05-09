use deflate::CompressionOptions;
use deflate::write::GzEncoder;
use futures::future::{self, FutureResult};
use http::{self, header, StatusCode};
use hyper::{
    service::Service,
    Body,
    Request,
    Response,
};

use std::error::Error;
use std::fmt;
use std::io::{self, Write};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use super::Root;

/// Serve Prometheues metrics.
#[derive(Debug, Clone)]
pub struct Serve {
    metrics: Arc<Mutex<Root>>,
    idle_retain: Duration,
}

#[derive(Debug)]
enum ServeError {
    Http(http::Error),
    Io(io::Error),
}

// ===== impl Serve =====

impl Serve {
    pub(super) fn new(metrics: &Arc<Mutex<Root>>, idle_retain: Duration) -> Self {
        Serve {
            metrics: metrics.clone(),
            idle_retain,
        }
    }

    fn is_gzip<B>(req: &Request<B>) -> bool {
        req.headers()
            .get_all(header::ACCEPT_ENCODING).iter()
            .any(|value| {
                value.to_str().ok()
                    .map(|value| value.contains("gzip"))
                    .unwrap_or(false)
            })
    }
}

impl Service for Serve {
    type ReqBody = Body;
    type ResBody = Body;
    type Error = io::Error;
    type Future = FutureResult<Response<Body>, Self::Error>;

    fn call(&mut self, req: Request<Body>) -> Self::Future {
        if req.uri().path() != "/metrics" {
            let rsp = Response::builder()
                .status(StatusCode::NOT_FOUND)
                .body(Body::empty())
                .expect("builder with known status code should not fail");
            return future::ok(rsp);
        }

        let mut metrics = self.metrics.lock()
            .expect("metrics lock poisoned");

        metrics.retain_since(Instant::now() - self.idle_retain);
        let metrics = metrics;

        let resp = if Self::is_gzip(&req) {
            trace!("gzipping metrics");
            let mut writer = GzEncoder::new(Vec::<u8>::new(), CompressionOptions::fast());
            write!(&mut writer, "{}", *metrics)
                .and_then(|_| writer.finish())
                .map_err(ServeError::from)
                .and_then(|body| {
                    Response::builder()
                        .header(header::CONTENT_ENCODING, "gzip")
                        .header(header::CONTENT_TYPE, "text/plain")
                        .body(Body::from(body))
                        .map_err(ServeError::from)
                })

        } else {
            let mut writer = Vec::<u8>::new();
            write!(&mut writer, "{}", *metrics)
                .map_err(ServeError::from)
                .and_then(|_| {
                    Response::builder()
                        .header(header::CONTENT_TYPE, "text/plain")
                        .body(Body::from(writer))
                        .map_err(ServeError::from)
                })
        };

        let resp = resp.unwrap_or_else(|e| {
            error!("{}", e);
            Response::builder()
                .status(StatusCode::INTERNAL_SERVER_ERROR)
                .body(Body::empty())
                .expect("builder with known status code should not fail")
        });
        future::ok(resp)
    }
}

// ===== impl ServeError =====

impl From<http::Error> for ServeError {
    fn from(err: http::Error) -> Self {
        ServeError::Http(err)
    }
}

impl From<io::Error> for ServeError {
    fn from(err: io::Error) -> Self {
        ServeError::Io(err)
    }
}

impl fmt::Display for ServeError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}: {}",
            self.description(),
            self.cause().expect("ServeError must have cause")
        )
    }
}

impl Error for ServeError {
    fn description(&self) -> &str {
        match *self {
            ServeError::Http(_) => "error constructing HTTP response",
            ServeError::Io(_) => "error writing metrics"
        }
    }

    fn cause(&self) -> Option<&Error> {
        match *self {
            ServeError::Http(ref cause) => Some(cause),
            ServeError::Io(ref cause) => Some(cause),
        }
    }
}
