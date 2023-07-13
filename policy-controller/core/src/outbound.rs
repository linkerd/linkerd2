use crate::http_route::{
    FailureInjectorFilter, GroupKindNamespaceName, HeaderModifierFilter, HostMatch, HttpRouteMatch,
    RequestRedirectFilter,
};
use ahash::AHashMap as HashMap;
use anyhow::{Context, Result};
use chrono::{offset::Utc, DateTime};
use futures::prelude::*;
use std::{
    net::IpAddr,
    num::{NonZeroU16, NonZeroU32},
    pin::Pin,
    str::FromStr,
    time,
};

/// Models outbound policy discovery.
#[async_trait::async_trait]
pub trait DiscoverOutboundPolicy<T> {
    async fn get_outbound_policy(&self, target: T) -> Result<Option<OutboundPolicy>>;

    async fn watch_outbound_policy(&self, target: T) -> Result<Option<OutboundPolicyStream>>;

    fn lookup_ip(&self, addr: IpAddr, port: NonZeroU16, source_namespace: String) -> Option<T>;
}

pub type OutboundPolicyStream = Pin<Box<dyn Stream<Item = OutboundPolicy> + Send + Sync + 'static>>;

pub struct OutboundDiscoverTarget {
    pub service_name: String,
    pub service_namespace: String,
    pub service_port: NonZeroU16,
    pub source_namespace: String,
}

#[derive(Clone, Debug, PartialEq)]
pub struct OutboundPolicy {
    pub http_routes: HashMap<GroupKindNamespaceName, HttpRoute>,
    pub authority: String,
    pub name: String,
    pub namespace: String,
    pub port: NonZeroU16,
    pub opaque: bool,
    pub accrual: Option<FailureAccrual>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HttpRoute {
    pub hostnames: Vec<HostMatch>,
    pub rules: Vec<HttpRouteRule>,

    /// This is required for ordering returned `HttpRoute`s by their creation
    /// timestamp.
    pub creation_timestamp: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HttpRouteRule {
    pub matches: Vec<HttpRouteMatch>,
    pub backends: Vec<Backend>,
    pub request_timeout: Option<time::Duration>,
    pub backend_request_timeout: Option<time::Duration>,
    pub filters: Vec<Filter>,
    /// This is generic: it is either an `Option<RouteRetryPolicy>` when the
    /// rule has resolved its retry policy, or an
    pub retry_policy: Option<RouteRetryPolicy>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Backend {
    Addr(WeightedAddr),
    Service(WeightedService),
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

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RouteRetryPolicy {
    pub max_per_request: Option<NonZeroU32>,
    pub statuses: Vec<StatusRange>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct StatusRange {
    pub min: http::StatusCode,
    pub max: http::StatusCode,
}

impl FromStr for StatusRange {
    type Err = anyhow::Error;
    fn from_str(s: &str) -> std::result::Result<Self, Self::Err> {
        let s = s.trim();
        let mut parts = s.split('-');
        let min = parts
            .next()
            .ok_or_else(|| anyhow::anyhow!("status range must be non-empty"))?;
        let min = min
            .trim()
            .parse::<http::StatusCode>()
            .with_context(|| format!("invalid status range minimum {min:?}"))?;
        let max = parts
            .next()
            .map(|max| {
                max.trim()
                    .parse::<http::StatusCode>()
                    .with_context(|| format!("invalid status range maximum {max:?}"))
            })
            .transpose()?
            // if the range only specifies a minimum, set it as the max as well.
            .unwrap_or(min);
        Ok(Self { min, max })
    }
}
