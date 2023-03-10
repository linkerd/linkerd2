#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod http_route;
mod identity_match;
mod network_match;

pub use self::{identity_match::IdentityMatch, network_match::NetworkMatch};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use futures::prelude::*;
pub use ipnet::{IpNet, Ipv4Net, Ipv6Net};
use std::{hash::Hash, net::IpAddr, num::NonZeroU16, pin::Pin, time::Duration};

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
    pub http_routes: HashMap<http_route::inbound::HttpRouteRef, http_route::inbound::HttpRoute>,
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

/// Models outbound policy discovery.
#[async_trait::async_trait]
pub trait DiscoverOutboundPolicy<T> {
    async fn get_outbound_policy(&self, target: T) -> Result<Option<OutboundPolicy>>;

    async fn watch_outbound_policy(&self, target: T) -> Result<Option<OutboundPolicyStream>>;

    fn service_lookup(&self, addr: IpAddr, port: NonZeroU16) -> Option<T>;
}

pub type OutboundPolicyStream = Pin<Box<dyn Stream<Item = OutboundPolicy> + Send + Sync + 'static>>;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutboundPolicy {
    pub http_routes: HashMap<String, http_route::outbound::HttpRoute>,
    pub authority: String,
    pub namespace: String,
    pub opaque: bool,
}
