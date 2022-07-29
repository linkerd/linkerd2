use crate::{AuthorizationRef, ClientAuthorization};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use chrono::{offset::Utc, DateTime};
pub use http::{
    header::{HeaderName, HeaderValue},
    uri::Scheme,
    Method, StatusCode,
};
use regex::Regex;
use std::num::NonZeroU16;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InboundHttpRoute {
    pub hostnames: Vec<HostMatch>,
    pub rules: Vec<InboundHttpRouteRule>,
    pub authorizations: HashMap<AuthorizationRef, ClientAuthorization>,

    /// This is required for ordering returned `HttpRoute`s by their creation
    /// timestamp.
    pub creation_timestamp: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum HostMatch {
    Exact(String),
    Suffix { reverse_labels: Vec<String> },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InboundHttpRouteRule {
    pub matches: Vec<HttpRouteMatch>,
    pub filters: Vec<InboundFilter>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum InboundFilter {
    RequestHeaderModifier(RequestHeaderModifierFilter),
    RequestRedirect(RequestRedirectFilter),
    FailureInjector(FailureInjectorFilter),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RequestHeaderModifierFilter {
    pub add: Vec<(HeaderName, HeaderValue)>,
    pub set: Vec<(HeaderName, HeaderValue)>,
    pub remove: Vec<HeaderName>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RequestRedirectFilter {
    pub scheme: Option<Scheme>,
    pub host: Option<String>,
    pub path: Option<PathModifier>,
    pub port: Option<NonZeroU16>,
    pub status: Option<StatusCode>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FailureInjectorFilter {
    pub status: StatusCode,
    pub message: String,
    pub ratio: Ratio,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum PathModifier {
    Full(String),
    Prefix(String),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Ratio {
    pub numerator: u32,
    pub denominator: u32,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HttpRouteMatch {
    pub path: Option<PathMatch>,
    pub headers: Vec<HeaderMatch>,
    pub query_params: Vec<QueryParamMatch>,
    pub method: Option<Method>,
}

#[derive(Clone, Debug)]
pub enum PathMatch {
    Exact(String),
    Prefix(String),
    Regex(Regex),
}

#[derive(Clone, Debug)]
pub enum HeaderMatch {
    Exact(HeaderName, HeaderValue),
    Regex(HeaderName, Regex),
}

#[derive(Clone, Debug)]
pub enum QueryParamMatch {
    Exact(String, String),
    Regex(String, Regex),
}

// === impl InboundHttpRoute ===

/// The default `InboundHttpRoute` used for any `InboundServer` that
/// does not have routes.
impl Default for InboundHttpRoute {
    fn default() -> Self {
        Self {
            hostnames: vec![],
            rules: vec![InboundHttpRouteRule {
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

// === impl PathMatch ===

impl PartialEq for PathMatch {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Exact(l0), Self::Exact(r0)) => l0 == r0,
            (Self::Prefix(l0), Self::Prefix(r0)) => l0 == r0,
            (Self::Regex(l0), Self::Regex(r0)) => l0.as_str() == r0.as_str(),
            _ => false,
        }
    }
}

impl Eq for PathMatch {}

impl PathMatch {
    pub fn regex(s: &str) -> Result<Self> {
        Ok(Self::Regex(Regex::new(s)?))
    }
}

// === impl HeaderMatch ===

impl PartialEq for HeaderMatch {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Exact(n0, v0), Self::Exact(n1, v1)) => n0 == n1 && v0 == v1,
            (Self::Regex(n0, r0), Self::Regex(n1, r1)) => n0 == n1 && r0.as_str() == r1.as_str(),
            _ => false,
        }
    }
}

impl Eq for HeaderMatch {}

// === impl QueryParamMatch ===

impl PartialEq for QueryParamMatch {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Exact(n0, v0), Self::Exact(n1, v1)) => n0 == n1 && v0 == v1,
            (Self::Regex(n0, r0), Self::Regex(n1, r1)) => n0 == n1 && r0.as_str() == r1.as_str(),
            _ => false,
        }
    }
}

impl Eq for QueryParamMatch {}
