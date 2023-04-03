use crate::http_route::{HostMatch, HttpRouteMatch};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use futures::prelude::*;
use std::{net::IpAddr, num::NonZeroU16, pin::Pin, time};

/// Models outbound policy discovery.
#[async_trait::async_trait]
pub trait DiscoverOutboundPolicy<T> {
    async fn get_outbound_policy(&self, target: T) -> Result<Option<OutboundPolicy>>;

    async fn watch_outbound_policy(&self, target: T) -> Result<Option<OutboundPolicyStream>>;

    fn lookup_ip(&self, addr: IpAddr, port: NonZeroU16) -> Option<T>;
}

pub type OutboundPolicyStream = Pin<Box<dyn Stream<Item = OutboundPolicy> + Send + Sync + 'static>>;

#[derive(Clone, Debug, PartialEq)]
pub struct OutboundPolicy {
    pub http_routes: HashMap<String, HttpRoute>,
    pub authority: String,
    pub namespace: String,
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
}

#[derive(Clone, Debug, PartialEq)]
pub enum FailureAccrual {
    Consecutive { max_failures: u32, backoff: Backoff },
}

#[derive(Clone, Debug, PartialEq)]
pub struct Backoff {
    pub min_penalty: time::Duration,
    pub max_penalty: time::Duration,
    pub jitter: f32,
}

impl std::default::Default for Backoff {
    fn default() -> Self {
        Self {
            min_penalty: time::Duration::from_secs(1),
            max_penalty: time::Duration::from_secs(60),
            jitter: 0.0,
        }
    }
}
