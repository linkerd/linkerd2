use super::{
    FailureAccrual, GrpcRetryCondition, GrpcRoute, HttpRetryCondition, HttpRoute, RouteRetry,
    RouteSet, RouteTimeouts, TcpRoute, TlsRoute, TrafficPolicy,
};

use std::{net::SocketAddr, num::NonZeroU16};

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

// ParentInfo carries resource-specific information about
// the parent to which outbound policy is associated.
#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub enum ParentInfo {
    Service {
        name: String,
        namespace: String,
        authority: String,
    },
    EgressNetwork {
        name: String,
        namespace: String,
        traffic_policy: TrafficPolicy,
    },
}

#[derive(Clone, Debug, PartialEq)]
pub struct OutboundPolicy {
    pub parent_info: ParentInfo,
    pub http_routes: RouteSet<HttpRoute>,
    pub grpc_routes: RouteSet<GrpcRoute>,
    pub tls_routes: RouteSet<TlsRoute>,
    pub tcp_routes: RouteSet<TcpRoute>,
    pub port: NonZeroU16,
    pub opaque: bool,
    pub accrual: Option<FailureAccrual>,
    pub http_retry: Option<RouteRetry<HttpRetryCondition>>,
    pub grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    pub timeouts: RouteTimeouts,
}

impl ParentInfo {
    pub fn name(&self) -> &str {
        match self {
            Self::EgressNetwork { name, .. } => name,
            Self::Service { name, .. } => name,
        }
    }

    pub fn namespace(&self) -> &str {
        match self {
            Self::EgressNetwork { namespace, .. } => namespace,
            Self::Service { namespace, .. } => namespace,
        }
    }
}

impl OutboundPolicy {
    pub fn parent_name(&self) -> &str {
        self.parent_info.name()
    }

    pub fn parent_namespace(&self) -> &str {
        self.parent_info.namespace()
    }
}

impl ResourceOutboundPolicy {
    pub fn policy(&self) -> &OutboundPolicy {
        match self {
            Self::Egress { policy, .. } => policy,
            Self::Service { policy, .. } => policy,
        }
    }
}