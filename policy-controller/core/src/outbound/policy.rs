use super::{
    FailureAccrual, GrpcRetryCondition, GrpcRoute, HttpRetryCondition, HttpRoute, RouteRetry,
    RouteSet, RouteTimeouts, TcpRoute, TlsRoute, TrafficPolicy,
};

use std::{net::SocketAddr, num::NonZeroU16};

/// OutboundPolicyKind describes a resolved outbound policy that is
/// either attributed to a resource or is a fallback one.
#[allow(clippy::large_enum_variant)]
#[derive(Clone, Debug, PartialEq)]
pub enum OutboundPolicyKind {
    Fallback(SocketAddr),
    Resource(ResourceOutboundPolicy),
}

/// ResourceOutboundPolicy expresses the known resource types
/// that can be parents for outbound policy. They each come with
/// specific metadata that is used when putting together the final
/// policy response.
#[derive(Clone, Debug, PartialEq)]
pub enum ResourceOutboundPolicy {
    Service {
        authority: String,
        policy: OutboundPolicy,
    },
    Egress {
        traffic_policy: TrafficPolicy,
        original_dst: SocketAddr,
        policy: OutboundPolicy,
    },
}

// ParentMeta carries information resource-specific
// information about the parent to which outbound policy
// is associated.
#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub enum ParentMeta {
    Service { authority: String },
    EgressNetwork(TrafficPolicy),
}

#[derive(Clone, Debug, PartialEq)]
pub struct OutboundPolicy {
    pub parent_meta: ParentMeta,
    pub http_routes: RouteSet<HttpRoute>,
    pub grpc_routes: RouteSet<GrpcRoute>,
    pub tls_routes: RouteSet<TlsRoute>,
    pub tcp_routes: RouteSet<TcpRoute>,
    pub name: String,
    pub namespace: String,
    pub port: NonZeroU16,
    pub opaque: bool,
    pub accrual: Option<FailureAccrual>,
    pub http_retry: Option<RouteRetry<HttpRetryCondition>>,
    pub grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    pub timeouts: RouteTimeouts,
}

impl ResourceOutboundPolicy {
    pub fn policy(&self) -> &OutboundPolicy {
        match self {
            Self::Egress { policy, .. } => policy,
            Self::Service { policy, .. } => policy,
        }
    }
}
