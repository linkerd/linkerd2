use ahash::AHashMap as HashMap;
use anyhow::Result;
pub use hyper::http::{uri::Scheme, Method, StatusCode};
use regex::Regex;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HttpRoute {
    pub hostnames: Vec<Hostname>,
    pub rules: Vec<HttpRouteRule>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Hostname {
    Exact(String),
    Suffix { reverse_labels: Vec<String> },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HttpRouteRule {
    pub matches: Vec<HttpRouteMatch>,
    pub filters: Vec<HttpFilter>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum HttpFilter {
    RequestHeaderModifier {
        add: HashMap<String, String>,
        set: HashMap<String, String>,
        remove: Vec<String>,
    },
    RequestRedirect {
        scheme: Option<Scheme>,
        host: Option<String>,
        path: Option<PathModifier>,
        port: Option<u32>,
        status: Option<StatusCode>,
    },
    HttpFailureInjector {
        status: StatusCode,
        message: String,
        ratio: Ratio,
    },
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

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HeaderMatch {
    pub name: String,
    pub value: Value,
}

#[derive(Clone, Debug)]
pub enum Value {
    Exact(String),
    Regex(Regex),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct QueryParamMatch {
    pub name: String,
    pub value: Value,
}

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

impl PartialEq for Value {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Exact(l0), Self::Exact(r0)) => l0 == r0,
            (Self::Regex(l0), Self::Regex(r0)) => l0.as_str() == r0.as_str(),
            _ => false,
        }
    }
}

impl Eq for Value {}

impl Value {
    pub fn regex(s: &str) -> Result<Self> {
        Ok(Self::Regex(Regex::new(s)?))
    }
}
