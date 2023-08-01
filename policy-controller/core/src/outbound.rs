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
    sync::Arc,
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
    /// The route rule's retry policy, if one was specified.
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

/// A retry policy for an outbound HTTPRoute rule, as defined by a
/// HTTPRetryFilter `extensionRef` filter.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RouteRetryPolicy {
    /// The name of the referenced HTTPRetryFilter object.
    pub name: String,
    /// The retry policy provided by the referenced HTTPRetryFilter object.
    pub state: RetryPolicyState,
}

/// An HTTPRoute rule's retry policy may be in one of two states:
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum RetryPolicyState {
    /// The referenced HTTPRetryFilter object was not resolved (no
    /// HTTPRetryFilter with that name exists in the route's namespace).
    ///
    /// As specified in [the Gateway API spec][spec], routes which reference an
    /// unresolved filter must fail all requests:
    ///
    /// > If a reference to a custom filter type cannot be resolved, the filter
    /// > MUST NOT be skipped. Instead, requests that would have been processed
    /// > by that filter MUST receive a HTTP error response.
    ///
    /// If an HTTPRetryFilter object that matches the reference is later created
    /// in the HTTPRoute's namespace, we transition to [`RetryPolicyState::Resolved`].
    ///
    /// [spec]: https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io%2fv1beta1.HTTPRouteFilter
    NotResolved(FailureInjectorFilter),
    /// The referenced HTTPRetryFilter object exists in the route's namespace
    /// and provides a valid retry policy.
    ///
    /// If the HTTPRetryFilter object that matches the reference is deleted, we
    /// will transition back to [`RetryPolicyState::NotResolved`].
    Resolved(Arc<RetryPolicy>),
}

/// An HTTP retry policy for an HTTPRoute, as provided by a HTTPRetryFilter.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RetryPolicy {
    /// An optional maximum number of retries allowed per request.
    pub max_per_request: Option<NonZeroU32>,

    /// A list of HTTP status ranges for which retries are permitted.
    pub statuses: Vec<StatusRange>,
}

#[derive(Copy, Clone, Debug, PartialEq, Eq)]
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
