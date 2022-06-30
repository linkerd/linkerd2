#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct HttpRoute {
    pub hostnames: Vec<Hostname>,
    pub matches: Vec<HttpRouteMatch>,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Hostname {
    Exact(String),
    Suffix { reverse_labels: Vec<String> },
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct HttpRouteMatch {
    pub path: Option<PathMatch>,
    pub headers: Vec<HeaderMatch>,
    pub query_params: Vec<QueryParamMatch>,
    pub method: Option<HttpMethod>,
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

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum HttpMethod {
    GET,
    POST,
    PUT,
    DELETE,
    PATCH,
    HEAD,
    OPTIONS,
    CONNECT,
    TRACE,
    Unregistered(String),
}
