use crate::routes::{
    FailureInjectorFilter, GroupKindNamespaceName, GrpcRouteMatch, HeaderModifierFilter, HostMatch,
    HttpRouteMatch, RequestRedirectFilter,
};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use futures::prelude::*;
use std::{net::IpAddr, num::NonZeroU16, pin::Pin, time};

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
    Consecutive { max_failures: u32, backoff: Backoff },
}

#[derive(Copy, Clone, Debug, PartialEq)]
pub struct Backoff {
    pub min_penalty: time::Duration,
    pub max_penalty: time::Duration,
    pub jitter: f32,
}

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
