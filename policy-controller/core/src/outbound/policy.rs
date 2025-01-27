use super::{
    FailureAccrual, GrpcRetryCondition, GrpcRoute, HttpRetryCondition, HttpRoute, RouteRetry,
    RouteSet, RouteTimeouts, TcpRoute, TlsRoute, TrafficPolicy,
};

use std::num::NonZeroU16;

/// Describes outbound policy parent resource.
#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub struct Parent {
    pub namespace: String,
    pub name: String,
    pub port: ParentPort,
    pub kind: ParentKind,
}

/// Describes outbound policy parent port.
#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub struct ParentPort {
    pub number: NonZeroU16,
    pub name: Option<String>,
    // TODO(ver) replace this with protocol configuration.
    pub opaque: bool,
}

#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub enum ParentKind {
    Service { authority: String },
    EgressNetwork { traffic: TrafficPolicy },
}

#[derive(Clone, Debug, PartialEq)]
pub struct OutboundPolicy {
    pub parent: Parent,

    pub http_routes: RouteSet<HttpRoute>,
    pub grpc_routes: RouteSet<GrpcRoute>,
    pub tls_routes: RouteSet<TlsRoute>,
    pub tcp_routes: RouteSet<TcpRoute>,
    pub accrual: Option<FailureAccrual>,
    pub http_retry: Option<RouteRetry<HttpRetryCondition>>,
    pub grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    pub timeouts: RouteTimeouts,

    pub allow_l5d_request_headers: bool,
}

impl OutboundPolicy {
    pub fn parent_name(&self) -> &str {
        &self.parent.name
    }

    pub fn parent_namespace(&self) -> &str {
        &self.parent.namespace
    }
}
