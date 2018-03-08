use std::sync::Arc;


use telemetry::event::Event;
use futures::future::{self, FutureResult};
use hyper;
use hyper::StatusCode;
use hyper::server::{
    Service as HyperService,
    Request as HyperRequest,
    Response as HyperResponse
};


/// Tracks Prometheus metrics
#[derive(Debug, Clone)]
pub struct Metrics {

}

/// Serve scrapable metrics.
#[derive(Debug, Clone)]
pub struct Server {
    metrics: Arc<Metrics>,
}

impl Server {
    pub fn new(metrics: &Arc<Metrics>) -> Self {
        Server { metrics: metrics.clone() }
    }
}


impl Metrics {
    pub fn new() -> Self {
        Metrics { }
    }

    /// Observe the given event.
    ///
    /// This borrows self immutably, so that individual metric fields
    /// can implement their own mutual exclusion strategy (i.e. counters
    /// can just use atomic integers).
    pub fn record_event(&self, event: &Event) {
        trace!("Metrics::record({:?})", event);
        // TODO: record the event.
    }
}

impl HyperService for Server {
    type Request = HyperRequest;
    type Response = HyperResponse;
    type Error = hyper::Error;
    type Future = FutureResult<Self::Response, Self::Error>;

    fn call(& self, req: Self::Request) -> Self::Future {
        if req.path() != "/metrics" {
            let rsp = HyperResponse::new().with_status(StatusCode::NotFound);
            return future::ok(rsp);
        }

        future::ok(HyperResponse::new()
            .with_status(StatusCode::Ok)
            .with_body(""))
    }
}
