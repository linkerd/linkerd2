#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod http_route;
mod identity_match;
mod network_match;

pub use self::{
    http_route::InboundHttpRoute, identity_match::IdentityMatch, network_match::NetworkMatch,
};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use futures::prelude::*;
pub use ipnet::{IpNet, Ipv4Net, Ipv6Net};
use std::{hash::Hash, pin::Pin, time::Duration};

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
    pub http_routes: HashMap<InboundHttpRouteRef, InboundHttpRoute>,
}

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
pub enum InboundHttpRouteRef {
    Default(&'static str),
    Linkerd(String),
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

// === impl InboundHttpRouteRef ===

impl Ord for InboundHttpRouteRef {
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

impl PartialOrd for InboundHttpRouteRef {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}
