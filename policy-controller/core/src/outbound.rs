use crate::routes::{
    FailureInjectorFilter, GroupKindNamespaceName, GrpcRouteMatch, HeaderModifierFilter, HostMatch,
    HttpRouteMatch, RequestRedirectFilter,
};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use futures::prelude::*;
use std::{net::IpAddr, num::NonZeroU16, pin::Pin, str::FromStr, sync::Arc, time};

mod policy;
mod target;

type FallbackPolicy = ();

pub use self::{
    policy::{OutboundPolicy, ParentInfo},
    target::{Kind, OutboundDiscoverTarget, ResourceTarget},
};

pub trait Route {
    fn creation_timestamp(&self) -> Option<DateTime<Utc>>;
}

/// Models outbound policy discovery.
#[async_trait::async_trait]
pub trait DiscoverOutboundPolicy<R, T> {
    async fn get_outbound_policy(&self, target: R) -> Result<Option<OutboundPolicy>>;

    async fn watch_outbound_policy(&self, target: R) -> Result<Option<OutboundPolicyStream>>;

    async fn watch_external_policy(&self) -> ExternalPolicyStream;

    fn lookup_ip(&self, addr: IpAddr, port: NonZeroU16, source_namespace: String) -> Option<T>;
}

pub type OutboundPolicyStream = Pin<Box<dyn Stream<Item = OutboundPolicy> + Send + Sync + 'static>>;
pub type ExternalPolicyStream = Pin<Box<dyn Stream<Item = FallbackPolicy> + Send + Sync + 'static>>;

pub type HttpRoute = OutboundRoute<HttpRouteMatch, HttpRetryCondition>;
pub type GrpcRoute = OutboundRoute<GrpcRouteMatch, GrpcRetryCondition>;

pub type RouteSet<T> = HashMap<GroupKindNamespaceName, T>;

#[derive(Debug, Clone, PartialEq)]
pub enum AppProtocol {
    Http1,
    Http2,
    Opaque,
    Unknown(Arc<str>),
}

impl FromStr for AppProtocol {
    type Err = std::convert::Infallible;

    fn from_str(s: &str) -> std::result::Result<Self, Self::Err> {
        let protocol = match s.to_ascii_lowercase().as_str() {
            "http" => AppProtocol::Http1,
            "kubernetes.io/h2c" => AppProtocol::Http2,
            "linkerd.io/tcp" | "linkerd.io/opaque" => AppProtocol::Opaque,
            protocol => AppProtocol::Unknown(Arc::from(protocol)),
        };
        Ok(protocol)
    }
}

#[derive(Debug, Copy, Clone, Hash, PartialEq, Eq)]
pub enum TrafficPolicy {
    Allow,
    Deny,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutboundRoute<M, R> {
    pub hostnames: Vec<HostMatch>,
    pub rules: Vec<OutboundRouteRule<M, R>>,

    /// This is required for ordering returned routes
    /// by their creation timestamp.
    pub creation_timestamp: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TlsRoute {
    pub hostnames: Vec<HostMatch>,
    pub rule: TcpRouteRule,
    /// This is required for ordering returned routes
    /// by their creation timestamp.
    pub creation_timestamp: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TcpRoute {
    pub rule: TcpRouteRule,

    /// This is required for ordering returned routes
    /// by their creation timestamp.
    pub creation_timestamp: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutboundRouteRule<M, R> {
    pub matches: Vec<M>,
    pub backends: Vec<Backend>,
    pub retry: Option<RouteRetry<R>>,
    pub timeouts: RouteTimeouts,
    pub filters: Vec<Filter>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TcpRouteRule {
    pub backends: Vec<Backend>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Backend {
    Addr(WeightedAddr),
    Service(WeightedService),
    EgressNetwork(WeightedEgressNetwork),
    Invalid { weight: u32, message: String },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WeightedAddr {
    pub weight: u32,
    pub addr: IpAddr,
    pub port: NonZeroU16,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WeightedService {
    pub weight: u32,
    pub authority: String,
    pub name: String,
    pub namespace: String,
    pub port: NonZeroU16,
    pub filters: Vec<Filter>,
    pub exists: bool,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WeightedEgressNetwork {
    pub weight: u32,
    pub name: String,
    pub namespace: String,
    pub port: Option<NonZeroU16>,
    pub filters: Vec<Filter>,
    pub exists: bool,
}

#[derive(Copy, Clone, Debug, PartialEq)]
pub enum FailureAccrual {
    Consecutive {
        max_failures: u32,
        backoff: Backoff,
    },
    Unified {
        max_failures: u32,
        backoff: Backoff,
        success_rate: SuccessRateConfig,
    },
}

#[derive(Copy, Clone, Debug, PartialEq)]
pub struct Backoff {
    pub min_penalty: time::Duration,
    pub max_penalty: time::Duration,
    pub jitter: f32,
}

#[derive(Copy, Clone, Debug, PartialEq)]
pub struct SuccessRateConfig {
    pub threshold: f64,
    /// Length of the trailing window over which the success ratio is
    /// measured. The proxy samples responses into a ring of fixed-duration
    /// buckets that span this window. The value sets a window length and
    /// does not act as an EWMA time constant.
    pub window: time::Duration,
    pub min_requests: u32,
}

pub const DEFAULT_SUCCESS_RATE_THRESHOLD: f64 = 0.8;
pub const DEFAULT_SUCCESS_RATE_WINDOW: time::Duration = time::Duration::from_secs(10);
pub const DEFAULT_SUCCESS_RATE_MIN_REQUESTS: u32 = 5;

/// Minimum success-rate window the proxy accepts. A shorter window
/// invalidates the whole client policy (MIN_SUCCESS_RATE_DECAY in the proxy).
pub const MIN_SUCCESS_RATE_WINDOW: time::Duration = time::Duration::from_millis(10);
/// Maximum success-rate min-requests the proxy accepts. A larger value
/// invalidates the whole client policy (MAX_SUCCESS_RATE_MIN_REQUESTS in the proxy).
pub const MAX_SUCCESS_RATE_MIN_REQUESTS: u32 = 1_000_000;

#[derive(Copy, Clone, Debug, PartialEq, Eq)]
pub struct LoadBiaserConfig {
    pub penalty: time::Duration,
    /// Ceiling the biaser applies to a Retry-After hint before it raises the
    /// effective RTT. The breaker honors Retry-After on its own, bounded by its
    /// backoff maximum.
    pub max_retry_after: time::Duration,
}

pub const DEFAULT_LOAD_BIASER_PENALTY: time::Duration = time::Duration::from_secs(5);
/// Decay the proxy folds into its single rtt_decay EWMA. The biaser has no
/// separate penalty-decay knob, so the default is emitted to keep the wire
/// stable.
pub const DEFAULT_LOAD_BIASER_PENALTY_DECAY: time::Duration = time::Duration::from_secs(10);

pub const DEFAULT_LOAD_BIASER_MAX_RETRY_AFTER: time::Duration = time::Duration::from_secs(300);

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Filter {
    RequestHeaderModifier(HeaderModifierFilter),
    ResponseHeaderModifier(HeaderModifierFilter),
    RequestRedirect(RequestRedirectFilter),
    FailureInjector(FailureInjectorFilter),
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct RouteTimeouts {
    pub response: Option<time::Duration>,
    pub request: Option<time::Duration>,
    pub idle: Option<time::Duration>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RouteRetry<R> {
    pub limit: u16,
    pub timeout: Option<time::Duration>,
    pub conditions: Option<Vec<R>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HttpRetryCondition {
    pub status_min: u32,
    pub status_max: u32,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum GrpcRetryCondition {
    Cancelled,
    DeadlineExceeded,
    ResourceExhausted,
    Internal,
    Unavailable,
}

impl<M, R> Route for OutboundRoute<M, R> {
    fn creation_timestamp(&self) -> Option<DateTime<Utc>> {
        self.creation_timestamp
    }
}

impl Route for TcpRoute {
    fn creation_timestamp(&self) -> Option<DateTime<Utc>> {
        self.creation_timestamp
    }
}

impl Route for TlsRoute {
    fn creation_timestamp(&self) -> Option<DateTime<Utc>> {
        self.creation_timestamp
    }
}
