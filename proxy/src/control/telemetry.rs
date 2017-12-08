use std::time::{Duration, Instant};

use futures::{Async, Future, Stream};
use tower::Service;
use tower_grpc;
use tokio_core::reactor::Handle;

use super::codec::Protobuf;
use super::pb::proxy::telemetry::{ReportRequest, ReportResponse};
use super::pb::proxy::telemetry::client::Telemetry as TelemetrySvc;
use super::pb::proxy::telemetry::client::telemetry_methods::Report as ReportRpc;
use ::timeout::{Timeout, TimeoutFuture};

pub type ClientBody = tower_grpc::client::codec::EncodingBody<
    Protobuf<ReportRequest, ReportResponse>,
    tower_grpc::client::codec::Unary<ReportRequest>,
>;

type TelemetryStream<F> = tower_grpc::client::BodyFuture<
    tower_grpc::client::Unary<
        tower_grpc::client::ResponseFuture<Protobuf<ReportRequest, ReportResponse>, TimeoutFuture<F>>,
        Protobuf<ReportRequest, ReportResponse>,
    >,
>;

#[derive(Debug)]
pub struct Telemetry<T, F> {
    reports: T,
    in_flight: Option<(Instant, TelemetryStream<F>)>,
    report_timeout: Duration,
    handle: Handle,
}

impl<T, F> Telemetry<T, F>
where
    T: Stream<Item = ReportRequest>,
    T::Error: ::std::fmt::Debug,
    F: Future<Item = ::http::Response<::tower_h2::RecvBody>>,
    F::Error: ::std::fmt::Debug,
{
    pub fn new(reports: T, report_timeout: Duration, handle: &Handle) -> Self {
        Telemetry {
            reports,
            in_flight: None,
            report_timeout,
            handle: handle.clone(),
        }
    }

    pub fn poll_rpc<S>(&mut self, client: &mut S)
    where
        S: Service<
            Request = ::http::Request<ClientBody>,
            Response = F::Item,
            Error = F::Error,
            Future = F,
        >,
    {
        let client = Timeout::new(client, self.report_timeout, &self.handle);
        let grpc = tower_grpc::Client::new(Protobuf::new(), client);
        let mut rpc = ReportRpc::new(grpc);

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


            let controller_ready = self.in_flight.is_none() && match rpc.poll_ready() {
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
                    error!("report stream complete");
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
                        let rep = TelemetrySvc::new(&mut rpc).report(report);
                        self.in_flight = Some((Instant::now(), rep));
                    }
                }
            }
        }
    }
}
