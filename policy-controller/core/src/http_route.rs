use ahash::AHashMap as HashMap;
pub use hyper::http::{uri::Scheme, Method, StatusCode};

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

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct HttpRouteMatch {
    pub path: Option<PathMatch>,
    pub headers: Vec<HeaderMatch>,
    pub query_params: Vec<QueryParamMatch>,
    pub method: Option<Method>,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum PathMatch {
    Exact(String),
    Prefix(String),
    Regex(String),
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct HeaderMatch {
    pub name: String,
    pub value: Value,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Value {
    Exact(String),
    Regex(String),
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct QueryParamMatch {
    pub name: String,
    pub value: Value,
}
