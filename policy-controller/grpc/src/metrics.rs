use prometheus_client::encoding::EncodeLabelSet;
use prometheus_client::metrics::histogram::Histogram;
use prometheus_client::{
    metrics::{counter::Counter, family::Family},
    registry::Registry,
};
use tokio::time;

#[derive(Clone, Debug)]
pub struct GrpcServerMetricsFamily {
    started: Family<Labels, Counter>,
    handling: Family<Labels, Histogram>,
    handled: Family<CodeLabels, Counter>,
    msg_received: Family<Labels, Counter>,
    msg_sent: Family<Labels, Counter>,
}

#[derive(Clone, Debug)]
pub(crate) struct GrpcServerRPCMetrics {
    started: Counter,
    msg_received: Counter,
    msg_sent: Counter,
    handling: Histogram,
    handled: Family<CodeLabels, Counter>,
    labels: Labels,
}

pub(crate) struct ResponseObserver {
    msg_sent: Counter,
    handled: Option<ResponseHandle>,
}

struct ResponseHandle {
    start: time::Instant,
    durations: Histogram,
    codes: Family<CodeLabels, Counter>,
    labels: Labels,
}

#[derive(Clone, Hash, PartialEq, Eq, EncodeLabelSet, Debug)]
struct Labels {
    grpc_service: &'static str,
    grpc_method: &'static str,
    grpc_type: &'static str,
}

#[derive(Clone, Hash, PartialEq, Eq, EncodeLabelSet, Debug)]
struct CodeLabels {
    grpc_service: &'static str,
    grpc_method: &'static str,
    grpc_type: &'static str,
    grpc_code: &'static str,
}

// === GrpcServerMetricsFamily ===

impl GrpcServerMetricsFamily {
    pub fn register(reg: &mut Registry) -> Self {
        let started = Family::<Labels, Counter>::default();
        reg.register(
            "started",
            "Total number of RPCs started on the server",
            started.clone(),
        );

        let msg_received = Family::<Labels, Counter>::default();
        reg.register(
            "msg_received",
            "Total number of RPC stream messages received on the server",
            msg_received.clone(),
        );

        let msg_sent = Family::<Labels, Counter>::default();
        reg.register(
            "msg_sent",
            "Total number of gRPC stream messages sent by the server",
            msg_sent.clone(),
        );

        let handled = Family::<CodeLabels, Counter>::default();
        reg.register(
            "handled",
            "Total number of RPCs completed on the server, regardless of success or failure",
            handled.clone(),
        );

        let handling = Family::<Labels, Histogram>::new_with_constructor(|| {
            // Our default client configuration has a 5m idle timeout and a 1h
            // max lifetime.
            Histogram::new([0.1, 1.0, 300.0, 3600.0])
        });
        reg.register_with_unit(
            "handling",
            "Histogram of response latency (seconds) of gRPC that had been application-level handled by the server",
            prometheus_client::registry::Unit::Seconds,
            handling.clone(),
        );

        Self {
            started,
            msg_received,
            msg_sent,
            handled,
            handling,
        }
    }

    pub(crate) fn unary_rpc(
        &self,
        svc: &'static str,
        method: &'static str,
    ) -> GrpcServerRPCMetrics {
        self.rpc(svc, method, "unary")
    }

    pub(crate) fn server_stream_rpc(
        &self,
        svc: &'static str,
        method: &'static str,
    ) -> GrpcServerRPCMetrics {
        self.rpc(svc, method, "server_stream")
    }

    fn rpc(
        &self,
        grpc_service: &'static str,
        grpc_method: &'static str,
        grpc_type: &'static str,
    ) -> GrpcServerRPCMetrics {
        let labels = Labels {
            grpc_service,
            grpc_method,
            grpc_type,
        };
        GrpcServerRPCMetrics {
            started: self.started.get_or_create(&labels).clone(),
            msg_received: self.msg_received.get_or_create(&labels).clone(),
            msg_sent: self.msg_sent.get_or_create(&labels).clone(),
            handled: self.handled.clone(),
            handling: self.handling.get_or_create(&labels).clone(),
            labels,
        }
    }
}

// === GrpcServerRPCMetrics ===

impl GrpcServerRPCMetrics {
    pub(crate) fn start(&self) -> ResponseObserver {
        self.started.inc();

        // All of our interfaces are unary or server-streaming, so we can
        // assume that if we receive a request, we received a single message.
        self.msg_received.inc();

        let handled = {
            // Pre-register OK
            let _ = self.handled.get_or_create(&CodeLabels {
                grpc_service: self.labels.grpc_service,
                grpc_method: self.labels.grpc_method,
                grpc_type: self.labels.grpc_type,
                grpc_code: code_str(tonic::Code::Ok),
            });

            Some(ResponseHandle {
                start: time::Instant::now(),
                durations: self.handling.clone(),
                codes: self.handled.clone(),
                labels: self.labels.clone(),
            })
        };

        ResponseObserver {
            msg_sent: self.msg_sent.clone(),
            handled,
        }
    }
}

// === ResponseObserver ===

impl ResponseObserver {
    pub(crate) fn msg_sent(&self) {
        self.msg_sent.inc();
    }

    pub(crate) fn end(mut self, code: tonic::Code) {
        self.handled
            .take()
            .expect("handle must be set")
            .inc_end(code);
    }
}

impl Drop for ResponseObserver {
    fn drop(&mut self) {
        if let Some(inner) = self.handled.take() {
            inner.inc_end(tonic::Code::Ok);
        }
    }
}

// === ResponseHandle ===

impl ResponseHandle {
    #[inline]
    fn inc_end(self, code: tonic::Code) {
        let Self {
            start,
            durations,
            codes,
            labels,
        } = self;
        durations.observe(start.elapsed().as_secs_f64());
        codes
            .get_or_create(&CodeLabels {
                grpc_service: labels.grpc_service,
                grpc_method: labels.grpc_method,
                grpc_type: labels.grpc_type,
                grpc_code: code_str(code),
            })
            .inc();
    }
}

fn code_str(code: tonic::Code) -> &'static str {
    use tonic::Code::*;
    match code {
        Ok => "OK",
        Cancelled => "CANCELLED",
        Unknown => "UNKNOWN",
        InvalidArgument => "INVALID_ARGUMENT",
        DeadlineExceeded => "DEADLINE_EXCEEDED",
        NotFound => "NOT_FOUND",
        AlreadyExists => "ALREADY_EXISTS",
        PermissionDenied => "PERMISSION_DENIED",
        ResourceExhausted => "RESOURCE_EXHAUSTED",
        FailedPrecondition => "FAILED_PRECONDITION",
        Aborted => "ABORTED",
        OutOfRange => "OUT_OF_RANGE",
        Unimplemented => "UNIMPLEMENTED",
        Internal => "INTERNAL",
        Unavailable => "UNAVAILABLE",
        DataLoss => "DATA_LOSS",
        Unauthenticated => "UNAUTHENTICATED",
    }
}
