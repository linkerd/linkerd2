use crate::{
    identity_match::IdentityMatch,
    network_match::NetworkMatch,
    routes::{
        FailureInjectorFilter, GroupKindName, GrpcRouteMatch, HeaderModifierFilter, HostMatch,
        HttpRouteMatch, PathMatch, RequestRedirectFilter,
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
pub enum HttpRouteRef {
    Default(&'static str),
    Linkerd(GroupKindName),
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
    pub http_routes: HashMap<HttpRouteRef, InboundRoute<HttpRouteMatch>>,
    pub grpc_routes: HashMap<GroupKindName, InboundRoute<GrpcRouteMatch>>,
}

pub type HttpRoute = InboundRoute<HttpRouteMatch>;
pub type GrpcRoute = InboundRoute<GrpcRouteMatch>;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InboundRoute<MatchType> {
    pub hostnames: Vec<HostMatch>,
    pub rules: Vec<InboundRouteRule<MatchType>>,
    pub authorizations: HashMap<AuthorizationRef, ClientAuthorization>,

    /// This is required for ordering returned `HttpRoute`s by their creation
    /// timestamp.
    pub creation_timestamp: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InboundRouteRule<MatchType> {
    pub matches: Vec<MatchType>,
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

// === impl InboundHttpRouteRef ===

impl Ord for HttpRouteRef {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        match (self, other) {
            (Self::Default(a), Self::Default(b)) => a.cmp(b),
            (Self::Linkerd(a), Self::Linkerd(b)) => a.cmp(b),
            // Route resources are always preferred over default resources, so they should sort
            // first in a list.
            (Self::Linkerd(_), Self::Default(_)) => std::cmp::Ordering::Less,
            (Self::Default(_), Self::Linkerd(_)) => std::cmp::Ordering::Greater,
        }
    }
}

impl PartialOrd for HttpRouteRef {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}
