use std::fmt;
use std::time::{Duration, Instant};

use futures::{Async, Future, Stream};
use tokio_core::reactor::Handle;
use tower_h2::{HttpService, BoxBody};
use tower_grpc as grpc;

use super::pb::proxy::telemetry::{ReportRequest, ReportResponse};
use super::pb::proxy::telemetry::client::Telemetry as TelemetrySvc;
use ::timeout::{Timeout, TimeoutFuture};

type TelemetryStream<F, B> = grpc::client::unary::ResponseFuture<
    ReportResponse, TimeoutFuture<F>, B>;

#[derive(Debug)]
pub struct Telemetry<T, S: HttpService> {
    reports: T,
    in_flight: Option<(Instant, TelemetryStream<S::Future, S::ResponseBody>)>,
    report_timeout: Duration,
    handle: Handle,
}

impl<T, S> Telemetry<T, S>
where
    S: HttpService<RequestBody = BoxBody, ResponseBody = ::tower_h2::RecvBody>,
    S::Error: fmt::Debug,
    T: Stream<Item = ReportRequest>,
    T::Error: ::std::fmt::Debug,
{
    pub fn new(reports: T, report_timeout: Duration, handle: &Handle) -> Self {
        Telemetry {
            reports,
            in_flight: None,
            report_timeout,
            handle: handle.clone(),
        }
    }

    pub fn poll_rpc(&mut self, client: &mut S)
    {
        let client = Timeout::new(client.lift_ref(), self.report_timeout, &self.handle);
        let mut svc = TelemetrySvc::new(client);

        //let _ctxt = ::logging::context("Telemetry.Report".into());

        loop {
            trace!("poll_rpc");
            if let Some((t0, mut fut)) = self.in_flight.take() {
                match fut.poll() {
                    Ok(Async::NotReady) => {
                        // TODO: can we just move this logging logic to `Timeout`?
                        trace!("report in flight to controller for {:?}", t0.elapsed());
                        self.in_flight = Some((t0, fut))
                    }
                    Ok(Async::Ready(_)) => {
                        trace!("report sent to controller in {:?}", t0.elapsed())
                    }
                    Err(err) => warn!("controller error: {:?}", err),
                }
            }

            let controller_ready = self.in_flight.is_none() && match svc.poll_ready() {
                Ok(Async::Ready(_)) => true,
                Ok(Async::NotReady) => {
                    trace!("controller unavailable");
                    false
                }
                Err(err) => {
                    warn!("controller error: {:?}", err);
                    false
                }
            };

            match self.reports.poll() {
                Ok(Async::NotReady) => {
                    return;
                }
                Ok(Async::Ready(None)) => {
                    debug!("report stream complete");
                    return;
                }
                Err(err) => {
                    warn!("report stream error: {:?}", err);
                }
                Ok(Async::Ready(Some(report))) => {
                    // Attempt to send the report.  Continue looping so that `reports` is
                    // polled until it's not ready.
                    if !controller_ready {
                        info!(
                            "report dropped; requests={} accepts={} connects={}",
                            report.requests.len(),
                            report.server_transports.len(),
                            report.client_transports.len(),
                        );
                    } else {
                        trace!(
                            "report sent; requests={} accepts={} connects={}",
                            report.requests.len(),
                            report.server_transports.len(),
                            report.client_transports.len(),
                        );
                        let rep = svc.report(grpc::Request::new(report));
                        self.in_flight = Some((Instant::now(), rep));
                    }
                }
            }
        }
    }
}
