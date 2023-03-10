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
pub enum HostMatch {
    Exact(String),
    Suffix { reverse_labels: Vec<String> },
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

pub mod inbound {
    pub use super::*;

    #[derive(Clone, Debug, PartialEq, Eq, Hash)]
    pub enum HttpRouteRef {
        Default(&'static str),
        Linkerd(String),
    }

    #[derive(Clone, Debug, PartialEq, Eq)]
    pub struct HttpRoute {
        pub hostnames: Vec<HostMatch>,
        pub rules: Vec<HttpRouteRule>,
        pub authorizations: HashMap<AuthorizationRef, ClientAuthorization>,

        /// This is required for ordering returned `HttpRoute`s by their creation
        /// timestamp.
        pub creation_timestamp: Option<DateTime<Utc>>,
    }

    #[derive(Clone, Debug, PartialEq, Eq)]
    pub struct HttpRouteRule {
        pub matches: Vec<HttpRouteMatch>,
        pub filters: Vec<Filter>,
    }

    #[derive(Clone, Debug, PartialEq, Eq)]
    pub enum Filter {
        RequestHeaderModifier(RequestHeaderModifierFilter),
        RequestRedirect(RequestRedirectFilter),
        FailureInjector(FailureInjectorFilter),
    }

    // === impl HttpRoute ===

    /// The default `InboundHttpRoute` used for any `InboundServer` that
    /// does not have routes.
    impl Default for HttpRoute {
        fn default() -> Self {
            Self {
                hostnames: vec![],
                rules: vec![HttpRouteRule {
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

    // === impl HttpRouteRef ===

    impl std::cmp::Ord for HttpRouteRef {
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

    impl std::cmp::PartialOrd for HttpRouteRef {
        fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
            Some(self.cmp(other))
        }
    }
}

pub mod outbound {
    use std::net::IpAddr;

    pub use super::*;

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
        InvalidDst(WeightedDst),
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
}
