use anyhow::Result;
pub use http::{
    header::{HeaderName, HeaderValue},
    uri::Scheme,
    Method, StatusCode,
};
use regex::Regex;
use std::{
    any::{Any, TypeId},
    borrow::Cow,
    num::NonZeroU16,
    str::FromStr,
};

#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub struct GroupKindName {
    pub group: Cow<'static, str>,
    pub kind: Cow<'static, str>,
    pub name: Cow<'static, str>,
}

#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub struct GroupKindNamespaceName {
    pub group: Cow<'static, str>,
    pub kind: Cow<'static, str>,
    pub namespace: Cow<'static, str>,
    pub name: Cow<'static, str>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum HostMatch {
    Exact(String),
    Suffix { reverse_labels: Vec<String> },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HeaderModifierFilter {
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

pub trait GenericRouteMatch: Any + Default {
    type MethodType: FromStr;
    fn set_path(self, path: impl ToString) -> Self;
    fn set_method(self, method: impl ToString) -> Result<Self>;
    fn is<MatchType: 'static>() -> bool {
        TypeId::of::<MatchType>() == TypeId::of::<Self>()
    }
}

// === impl GroupKindName ===

impl Ord for GroupKindName {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.name.cmp(&other.name).then(
            self.group
                .cmp(&other.group)
                .then(self.kind.cmp(&other.kind)),
        )
    }
}

impl PartialOrd for GroupKindName {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

impl GroupKindName {
    pub fn eq_ignore_ascii_case(&self, other: &Self) -> bool {
        self.group.eq_ignore_ascii_case(&other.group)
            && self.kind.eq_ignore_ascii_case(&other.kind)
            && self.name.eq_ignore_ascii_case(&other.name)
    }

    pub fn namespaced(self, namespace: String) -> GroupKindNamespaceName {
        GroupKindNamespaceName {
            group: self.group,
            kind: self.kind,
            namespace: namespace.into(),
            name: self.name,
        }
    }
}

// === impl HttpRouteMatch ===

impl Default for HttpRouteMatch {
    fn default() -> Self {
        Self {
            method: None,
            headers: Default::default(),
            query_params: Default::default(),
            path: Some(PathMatch::Prefix("/".to_string())),
        }
    }
}

impl GenericRouteMatch for HttpRouteMatch {
    type MethodType = Method;

    fn set_path(mut self, path: impl ToString) -> Self {
        self.path = Some(PathMatch::Exact(path.to_string()));
        self
    }

    fn set_method(mut self, method: impl ToString) -> Result<Self> {
        let method = method.to_string().parse()?;
        self.method = Some(method);
        Ok(self)
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
