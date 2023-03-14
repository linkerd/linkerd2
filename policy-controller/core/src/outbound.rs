use crate::http_route::{HostMatch, HttpRouteMatch};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use futures::prelude::*;
use std::{net::IpAddr, num::NonZeroU16, pin::Pin};

/// Models outbound policy discovery.
#[async_trait::async_trait]
pub trait DiscoverOutboundPolicy<T> {
    async fn get_outbound_policy(&self, target: T) -> Result<Option<OutboundPolicy>>;

    async fn watch_outbound_policy(&self, target: T) -> Result<Option<OutboundPolicyStream>>;

    fn lookup_ip(&self, addr: IpAddr, port: NonZeroU16) -> Option<T>;
}

pub type OutboundPolicyStream = Pin<Box<dyn Stream<Item = OutboundPolicy> + Send + Sync + 'static>>;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutboundPolicy {
    pub http_routes: HashMap<String, HttpRoute>,
    pub authority: String,
    pub namespace: String,
    pub opaque: bool,
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
    Dst(WeightedDst),
    InvalidDst { weight: u32, message: String },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WeightedAddr {
    pub weight: u32,
    pub addr: IpAddr,
    pub port: NonZeroU16,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WeightedDst {
    pub weight: u32,
    pub authority: String,
}
