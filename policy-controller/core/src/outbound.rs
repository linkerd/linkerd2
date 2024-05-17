use crate::routes::{
    FailureInjectorFilter, GroupKindNamespaceName, GrpcRouteMatch, HeaderModifierFilter, HostMatch,
    HttpRouteMatch, RequestRedirectFilter,
};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use futures::prelude::*;
use std::{
    any::type_name_of_val as type_of, net::IpAddr, num::NonZeroU16, pin::Pin, time,
    vec::IntoIter as IntoVecIter,
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

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum TypedOutboundRoute {
    Grpc(OutboundRoute<GrpcRouteMatch>),
    Http(OutboundRoute<HttpRouteMatch>),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum OutboundRouteCollection {
    Http(HashMap<GroupKindNamespaceName, OutboundRoute<HttpRouteMatch>>),
    Grpc(HashMap<GroupKindNamespaceName, OutboundRoute<GrpcRouteMatch>>),
}

#[derive(Clone, Debug, PartialEq)]
pub struct OutboundPolicy {
    pub routes: Option<OutboundRouteCollection>,
    pub authority: String,
    pub name: String,
    pub namespace: String,
    pub port: NonZeroU16,
    pub opaque: bool,
    pub accrual: Option<FailureAccrual>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutboundRoute<RouteType> {
    pub hostnames: Vec<HostMatch>,
    pub rules: Vec<OutboundRouteRule<RouteType>>,

    /// This is required for ordering returned routes
    /// by their creation timestamp.
    pub creation_timestamp: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutboundRouteRule<RouteType> {
    pub matches: Vec<RouteType>,
    pub backends: Vec<Backend>,
    pub request_timeout: Option<time::Duration>,
    pub backend_request_timeout: Option<time::Duration>,
    pub filters: Vec<Filter>,
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

// === impl TypedOutboundRoute ===

impl TypedOutboundRoute {
    pub fn from_gknn_and_route<Route: Into<TypedOutboundRoute>>(
        (gknn, route): (GroupKindNamespaceName, Route),
    ) -> (GroupKindNamespaceName, Self) {
        (gknn, route.into())
    }
}
impl From<OutboundRoute<GrpcRouteMatch>> for TypedOutboundRoute {
    fn from(rule: OutboundRoute<GrpcRouteMatch>) -> Self {
        Self::Grpc(rule)
    }
}

impl From<OutboundRoute<HttpRouteMatch>> for TypedOutboundRoute {
    fn from(rule: OutboundRoute<HttpRouteMatch>) -> Self {
        Self::Http(rule)
    }
}

// === impl OutboundRouteCollection ===

impl IntoIterator for OutboundRouteCollection {
    type Item = (GroupKindNamespaceName, TypedOutboundRoute);
    type IntoIter = IntoVecIter<(GroupKindNamespaceName, TypedOutboundRoute)>;

    fn into_iter(self) -> Self::IntoIter {
        match self {
            Self::Grpc(routes) => routes
                .into_iter()
                .map(TypedOutboundRoute::from_gknn_and_route)
                .collect::<Vec<(GroupKindNamespaceName, TypedOutboundRoute)>>()
                .into_iter(),
            Self::Http(routes) => routes
                .into_iter()
                .map(|(key, value)| (key, TypedOutboundRoute::from(value)))
                .collect::<Vec<(GroupKindNamespaceName, TypedOutboundRoute)>>()
                .into_iter(),
        }
    }
}

impl OutboundRouteCollection {
    pub fn for_gknn(gknn: &GroupKindNamespaceName) -> Option<Self> {
        match gknn.kind.as_ref() {
            "GRPCRoute" => Some(Self::Grpc(Default::default())),
            "HTTPRoute" => Some(Self::Http(Default::default())),
            _ => None,
        }
    }
    pub fn remove(&mut self, key: &GroupKindNamespaceName) {
        match self {
            Self::Grpc(routes) => {
                routes.remove(key);
            }
            Self::Http(routes) => {
                routes.remove(key);
            }
        }
    }

    pub fn insert<Route: Into<TypedOutboundRoute>>(
        &mut self,
        key: GroupKindNamespaceName,
        route: Route,
    ) -> Result<Option<TypedOutboundRoute>> {
        let route = route.into();

        match (self, route) {
            (Self::Http(routes), TypedOutboundRoute::Http(route)) => {
                Ok(routes.insert(key, route).map(Into::into))
            }
            (Self::Grpc(routes), TypedOutboundRoute::Grpc(route)) => {
                Ok(routes.insert(key, route).map(Into::into))
            }
            (routes, route) => anyhow::bail!(
                "cannot insert a {:?}-type route into a {:?}-type collection",
                type_of(&route),
                type_of(routes)
            ),
        }
    }
}
