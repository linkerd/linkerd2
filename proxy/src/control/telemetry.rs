use std::fmt;
use std::time::{Duration, Instant};

use futures::{Async, Future, Stream};
use tower_h2::{HttpService, BoxBody};
use tower_grpc as grpc;

use conduit_proxy_controller_grpc::telemetry::{ReportRequest, ReportResponse};
use conduit_proxy_controller_grpc::telemetry::client::Telemetry as TelemetrySvc;
use time::{NewTimeout, Timer, TimeoutFuture};

type TelemetryStream<F, B, T> = grpc::client::unary::ResponseFuture<
    ReportResponse,
    TimeoutFuture<F, T>,
    B
>;

#[derive(Debug)]
pub struct Telemetry<R, S: HttpService, T: Timer> {
    reports: R,
    in_flight: Option<(
        Instant,
        TelemetryStream<S::Future, S::ResponseBody, T::Sleep>
    )>,
    report_timeout: NewTimeout<T>,
    timer: T,
}

impl<R, S, T> Telemetry<R, S, T>
where
    S: HttpService<RequestBody = BoxBody, ResponseBody = ::tower_h2::RecvBody>,
    S::Error: fmt::Debug,
    R: Stream<Item = ReportRequest>,
    R::Error: fmt::Debug,
    T: Timer,
    T::Error: fmt::Debug,
{
    pub fn new(reports: R,
               report_timeout: Duration,
               timer: &T)
               -> Self
    {
        let timer = timer.clone();
        let report_timeout = timer
            .new_timeout(report_timeout)
            .with_description("report");

        Telemetry {
            reports,
            in_flight: None,
            report_timeout,
            timer,
        }
    }

    pub fn poll_rpc(&mut self, client: &mut S)
    {
        let client = self.report_timeout.apply_to(client.lift_ref());
        let mut svc = TelemetrySvc::new(client);

        //let _ctxt = ::logging::context("Telemetry.Report".into());

        loop {
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
                        self.in_flight = Some((self.timer.now(), rep));
                    }
                }
            }
        }
    }
}
