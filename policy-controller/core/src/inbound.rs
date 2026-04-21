use crate::{
    identity_match::IdentityMatch,
    network_match::NetworkMatch,
    routes::{
        FailureInjectorFilter, GroupKindName, GrpcMethodMatch, GrpcRouteMatch,
        HeaderModifierFilter, HostMatch, HttpRouteMatch, PathMatch, RequestRedirectFilter,
    },
};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use futures::prelude::*;
use std::{pin::Pin, time::Duration};

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ServerRef {
    Default(&'static str),
    Server(String),
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum AuthorizationRef {
    Default(&'static str),
    ServerAuthorization(String),
    AuthorizationPolicy(String),
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum RouteRef {
    Default(&'static str),
    Resource(GroupKindName),
}

/// Describes how a proxy should handle inbound connections.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum ProxyProtocol {
    /// Indicates that the protocol should be discovered dynamically.
    Detect {
        timeout: Duration,
    },

    Http1,
    Http2,
    Grpc,

    /// Indicates that connections should be handled opaquely.
    Opaque,

    /// Indicates that connections should be handled as application-terminated TLS.
    Tls,
}

/// Describes a class of authorized clients.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ClientAuthorization {
    /// Limits which source networks this authorization applies to.
    pub networks: Vec<NetworkMatch>,

    /// Describes the client's authentication requirements.
    pub authentication: ClientAuthentication,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ClientAuthentication {
    /// Indicates that clients need not be authenticated.
    Unauthenticated,

    /// Indicates that clients must use TLS but need not provide a client identity.
    TlsUnauthenticated,

    /// Indicates that clients must use mutually-authenticated TLS.
    TlsAuthenticated(Vec<IdentityMatch>),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RateLimit {
    pub name: String,
    pub total: Option<Limit>,
    pub identity: Option<Limit>,
    pub overrides: Vec<Override>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Limit {
    pub requests_per_second: u32,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Override {
    pub requests_per_second: u32,
    pub client_identities: Vec<String>,
}

/// Models inbound server configuration discovery.
#[async_trait::async_trait]
pub trait DiscoverInboundServer<T> {
    async fn get_inbound_server(&self, target: T) -> Result<Option<InboundServer>>;

    async fn watch_inbound_server(&self, target: T) -> Result<Option<InboundServerStream>>;
}

pub type InboundServerStream = Pin<Box<dyn Stream<Item = InboundServer> + Send + Sync + 'static>>;

/// Inbound server configuration.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InboundServer {
    pub reference: ServerRef,

    pub protocol: ProxyProtocol,
    pub authorizations: HashMap<AuthorizationRef, ClientAuthorization>,
    pub ratelimit: Option<RateLimit>,
    pub http_routes: HashMap<RouteRef, InboundRoute<HttpRouteMatch>>,
    pub grpc_routes: HashMap<RouteRef, InboundRoute<GrpcRouteMatch>>,
}

pub type HttpRoute = InboundRoute<HttpRouteMatch>;
pub type GrpcRoute = InboundRoute<GrpcRouteMatch>;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InboundRoute<M> {
    pub hostnames: Vec<HostMatch>,
    pub rules: Vec<InboundRouteRule<M>>,
    pub authorizations: HashMap<AuthorizationRef, ClientAuthorization>,

    /// This is required for ordering returned `HttpRoute`s by their creation
    /// timestamp.
    pub creation_timestamp: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InboundRouteRule<M> {
    pub matches: Vec<M>,
    pub filters: Vec<Filter>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Filter {
    RequestHeaderModifier(HeaderModifierFilter),
    ResponseHeaderModifier(HeaderModifierFilter),
    RequestRedirect(RequestRedirectFilter),
    FailureInjector(FailureInjectorFilter),
}

// === impl InboundRoute ===

/// The default `InboundRoute` used for any `InboundServer` that
/// does not have routes.
impl Default for InboundRoute<HttpRouteMatch> {
    fn default() -> Self {
        Self {
            hostnames: vec![],
            rules: vec![InboundRouteRule {
                matches: vec![HttpRouteMatch {
                    path: Some(PathMatch::Prefix("/".to_string())),
                    headers: vec![],
                    query_params: vec![],
                    method: None,
                }],
                filters: vec![],
            }],
            // Default routes do not have authorizations; the default policy's
            // authzs will be configured by the default `InboundServer`, not by
            // the route.
            authorizations: HashMap::new(),
            creation_timestamp: None,
        }
    }
}

/// The default `InboundRoute` used for any `InboundServer` that
/// does not have routes.
impl Default for InboundRoute<GrpcRouteMatch> {
    fn default() -> Self {
        Self {
            hostnames: vec![],
            rules: vec![InboundRouteRule {
                matches: vec![GrpcRouteMatch {
                    headers: vec![],
                    method: Some(GrpcMethodMatch {
                        method: None,
                        service: None,
                    }),
                }],
                filters: vec![],
            }],
            // Default routes do not have authorizations; the default policy's
            // authzs will be configured by the default `InboundServer`, not by
            // the route.
            authorizations: HashMap::new(),
            creation_timestamp: None,
        }
    }
}

// === impl InboundHttpRouteRef ===

impl Ord for RouteRef {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        match (self, other) {
            (Self::Default(a), Self::Default(b)) => a.cmp(b),
            (Self::Resource(a), Self::Resource(b)) => a.cmp(b),
            // Route resources are always preferred over default resources, so they should sort
            // first in a list.
            (Self::Resource(_), Self::Default(_)) => std::cmp::Ordering::Less,
            (Self::Default(_), Self::Resource(_)) => std::cmp::Ordering::Greater,
        }
    }
}

impl PartialOrd for RouteRef {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}
