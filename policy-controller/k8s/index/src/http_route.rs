use ahash::AHashMap as HashMap;
use anyhow::{anyhow, bail, Error, Result};
use k8s_gateway_api as api;
use linkerd_policy_controller_core::http_route;
use linkerd_policy_controller_k8s_api::{
    self as k8s, gateway,
    policy::{httproute as policy, Server},
};
use std::{fmt, num::NonZeroU16};

const STATUS_CONTROLLER_NAME: &str = "policy.linkerd.io/status-controller";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InboundRouteBinding {
    pub parents: Vec<InboundParentRef>,
    pub route: http_route::InboundHttpRoute,
    pub statuses: Vec<Status>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum InboundParentRef {
    Server(String),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Status {
    pub parent: String,
    pub conditions: Vec<Condition>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Condition {
    pub type_: ConditionType,
    pub status: bool,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ConditionType {
    Accepted,
}

#[derive(Clone, Debug, thiserror::Error)]
pub enum InvalidParentRef {
    #[error("HTTPRoute resource does not reference a Server resource")]
    DoesNotSelectServer,

    #[error("HTTPRoute resource may not reference a parent Server in an other namespace")]
    ServerInAnotherNamespace,

    #[error("HTTPRoute resource may not reference a parent by port")]
    SpecifiesPort,

    #[error("HTTPRoute resource may not reference a parent by section name")]
    SpecifiesSection,
}

impl TryFrom<api::HttpRoute> for InboundRouteBinding {
    type Error = Error;

    fn try_from(route: api::HttpRoute) -> Result<Self, Self::Error> {
        let route_ns = route.metadata.namespace.as_deref();
        let creation_timestamp = route.metadata.creation_timestamp.map(|k8s::Time(t)| t);
        let parents = InboundParentRef::collect_from(route_ns, route.spec.inner.parent_refs)?;
        let hostnames = route
            .spec
            .hostnames
            .into_iter()
            .flatten()
            .map(convert::host_match)
            .collect();

        let rules = route
            .spec
            .rules
            .into_iter()
            .flatten()
            .map(
                |api::HttpRouteRule {
                     matches,
                     filters,
                     backend_refs: _,
                 }| Self::try_rule(matches, filters, Self::try_gateway_filter),
            )
            .collect::<Result<_>>()?;

        let statuses = route
            .status
            .map_or_else(Vec::new, |status| Status::collect_from(status.inner));

        Ok(InboundRouteBinding {
            parents,
            route: http_route::InboundHttpRoute {
                hostnames,
                rules,
                authorizations: HashMap::default(),
                creation_timestamp,
            },
            statuses,
        })
    }
}

impl TryFrom<policy::HttpRoute> for InboundRouteBinding {
    type Error = Error;

    fn try_from(route: policy::HttpRoute) -> Result<Self, Self::Error> {
        let route_ns = route.metadata.namespace.as_deref();
        let creation_timestamp = route.metadata.creation_timestamp.map(|k8s::Time(t)| t);
        let parents = InboundParentRef::collect_from(route_ns, route.spec.inner.parent_refs)?;
        let hostnames = route
            .spec
            .hostnames
            .into_iter()
            .flatten()
            .map(convert::host_match)
            .collect();

        let rules = route
            .spec
            .rules
            .into_iter()
            .flatten()
            .map(
                |policy::HttpRouteRule {
                     matches, filters, ..
                 }| { Self::try_rule(matches, filters, Self::try_policy_filter) },
            )
            .collect::<Result<_>>()?;

        let statuses = route
            .status
            .map_or_else(Vec::new, |status| Status::collect_from(status.inner));

        Ok(InboundRouteBinding {
            parents,
            route: http_route::InboundHttpRoute {
                hostnames,
                rules,
                authorizations: HashMap::default(),
                creation_timestamp,
            },
            statuses,
        })
    }
}

impl InboundRouteBinding {
    #[inline]
    pub fn selects_server(&self, name: &str) -> bool {
        self.parents
            .iter()
            .any(|p| matches!(p, InboundParentRef::Server(n) if n == name))
    }

    #[inline]
    pub fn accepted_by_server(&self, name: &str) -> bool {
        self.statuses.iter().any(|status| {
            status.parent == name
                && status
                    .conditions
                    .iter()
                    .any(|condition| condition.type_ == ConditionType::Accepted && condition.status)
        })
    }

    fn try_match(
        api::HttpRouteMatch {
            path,
            headers,
            query_params,
            method,
        }: api::HttpRouteMatch,
    ) -> Result<http_route::HttpRouteMatch> {
        let path = path.map(convert::path_match).transpose()?;

        let headers = headers
            .into_iter()
            .flatten()
            .map(convert::header_match)
            .collect::<Result<_>>()?;

        let query_params = query_params
            .into_iter()
            .flatten()
            .map(convert::query_param_match)
            .collect::<Result<_>>()?;

        let method = method
            .as_deref()
            .map(http_route::Method::try_from)
            .transpose()?;

        Ok(http_route::HttpRouteMatch {
            path,
            headers,
            query_params,
            method,
        })
    }

    fn try_rule<F>(
        matches: Option<Vec<api::HttpRouteMatch>>,
        filters: Option<Vec<F>>,
        try_filter: impl Fn(F) -> Result<http_route::InboundFilter>,
    ) -> Result<http_route::InboundHttpRouteRule> {
        let matches = matches
            .into_iter()
            .flatten()
            .map(Self::try_match)
            .collect::<Result<_>>()?;

        let filters = filters
            .into_iter()
            .flatten()
            .map(try_filter)
            .collect::<Result<_>>()?;

        Ok(http_route::InboundHttpRouteRule { matches, filters })
    }

    fn try_gateway_filter(filter: api::HttpRouteFilter) -> Result<http_route::InboundFilter> {
        let filter = match filter {
            api::HttpRouteFilter::RequestHeaderModifier {
                request_header_modifier,
            } => {
                let filter = convert::req_header_modifier(request_header_modifier)?;
                http_route::InboundFilter::RequestHeaderModifier(filter)
            }

            api::HttpRouteFilter::RequestRedirect { request_redirect } => {
                let filter = convert::req_redirect(request_redirect)?;
                http_route::InboundFilter::RequestRedirect(filter)
            }

            api::HttpRouteFilter::RequestMirror { .. } => {
                bail!("RequestMirror filter is not supported")
            }
            api::HttpRouteFilter::URLRewrite { .. } => {
                bail!("URLRewrite filter is not supported")
            }
            api::HttpRouteFilter::ExtensionRef { .. } => {
                bail!("ExtensionRef filter is not supported")
            }
        };
        Ok(filter)
    }

    fn try_policy_filter(filter: policy::HttpRouteFilter) -> Result<http_route::InboundFilter> {
        let filter = match filter {
            policy::HttpRouteFilter::RequestHeaderModifier {
                request_header_modifier,
            } => {
                let filter = convert::req_header_modifier(request_header_modifier)?;
                http_route::InboundFilter::RequestHeaderModifier(filter)
            }

            policy::HttpRouteFilter::RequestRedirect { request_redirect } => {
                let filter = convert::req_redirect(request_redirect)?;
                http_route::InboundFilter::RequestRedirect(filter)
            }
        };
        Ok(filter)
    }
}

impl InboundParentRef {
    fn collect_from(
        route_ns: Option<&str>,
        parent_refs: Option<Vec<api::ParentReference>>,
    ) -> Result<Vec<Self>, InvalidParentRef> {
        let parents = parent_refs
            .into_iter()
            .flatten()
            .filter_map(|parent_ref| Self::from_parent_ref(route_ns, parent_ref))
            .collect::<Result<Vec<_>, InvalidParentRef>>()?;

        // If there are no valid parents, then the route is invalid.
        if parents.is_empty() {
            return Err(InvalidParentRef::DoesNotSelectServer);
        }

        Ok(parents)
    }

    fn from_parent_ref(
        route_ns: Option<&str>,
        parent_ref: api::ParentReference,
    ) -> Option<Result<Self, InvalidParentRef>> {
        // Skip parent refs that don't target a `Server` resource.
        if !policy::parent_ref_targets_kind::<Server>(&parent_ref) || parent_ref.name.is_empty() {
            return None;
        }

        let api::ParentReference {
            group: _,
            kind: _,
            namespace,
            name,
            section_name,
            port,
        } = parent_ref;

        if namespace.is_some() && namespace.as_deref() != route_ns {
            return Some(Err(InvalidParentRef::ServerInAnotherNamespace));
        }
        if port.is_some() {
            return Some(Err(InvalidParentRef::SpecifiesPort));
        }
        if section_name.is_some() {
            return Some(Err(InvalidParentRef::SpecifiesSection));
        }

        Some(Ok(InboundParentRef::Server(name)))
    }
}

impl Status {
    pub fn collect_from(status: gateway::RouteStatus) -> Vec<Self> {
        status
            .parents
            .iter()
            .filter(|status| status.controller_name == STATUS_CONTROLLER_NAME)
            .filter_map(Self::from_parent_status)
            .collect::<Vec<_>>()
    }

    fn from_parent_status(status: &gateway::RouteParentStatus) -> Option<Self> {
        // Only match parent statuses that belong to resources of
        // `kind: Server`.
        match status.parent_ref.kind.as_deref() {
            Some("Server") => (),
            _ => return None,
        }

        let conditions = status
            .conditions
            .iter()
            .filter_map(|condition| {
                let type_ = match condition.type_.as_ref() {
                    "Accepted" => ConditionType::Accepted,
                    condition_type => {
                        tracing::error!(%status.parent_ref.name, %condition_type, "Unexpected condition type found in parent status");
                        return None;
                    }
                };
                let status = match condition.status.as_ref() {
                    "True" => true,
                    "False" => false,
                    condition_status => {
                        tracing::error!(%status.parent_ref.name, %type_, %condition_status, "Unexpected condition status found in parent status");
                        return None
                    },
                };
                Some(Condition { type_, status })
            })
            .collect();

        Some(Status {
            parent: status.parent_ref.name.to_string(),
            conditions,
        })
    }
}

impl fmt::Display for ConditionType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Accepted => write!(f, "Accepted"),
        }
    }
}

pub mod convert {
    use super::*;

    pub fn path_match(path_match: api::HttpPathMatch) -> Result<http_route::PathMatch> {
        match path_match {
            api::HttpPathMatch::Exact { value } | api::HttpPathMatch::PathPrefix { value }
                if !value.starts_with('/') =>
            {
                Err(anyhow!("HttpPathMatch paths must be absolute (begin with `/`); {value:?} is not an absolute path"))
            }
            api::HttpPathMatch::Exact { value } => Ok(http_route::PathMatch::Exact(value)),
            api::HttpPathMatch::PathPrefix { value } => Ok(http_route::PathMatch::Prefix(value)),
            api::HttpPathMatch::RegularExpression { value } => value
                .parse()
                .map(http_route::PathMatch::Regex)
                .map_err(Into::into),
        }
    }

    pub fn host_match(hostname: api::Hostname) -> http_route::HostMatch {
        if hostname.starts_with("*.") {
            let mut reverse_labels = hostname
                .split('.')
                .skip(1)
                .map(|label| label.to_string())
                .collect::<Vec<String>>();
            reverse_labels.reverse();
            http_route::HostMatch::Suffix { reverse_labels }
        } else {
            http_route::HostMatch::Exact(hostname)
        }
    }

    pub fn header_match(header_match: api::HttpHeaderMatch) -> Result<http_route::HeaderMatch> {
        match header_match {
            api::HttpHeaderMatch::Exact { name, value } => Ok(http_route::HeaderMatch::Exact(
                name.parse()?,
                value.parse()?,
            )),
            api::HttpHeaderMatch::RegularExpression { name, value } => Ok(
                http_route::HeaderMatch::Regex(name.parse()?, value.parse()?),
            ),
        }
    }

    pub fn query_param_match(
        query_match: api::HttpQueryParamMatch,
    ) -> Result<http_route::QueryParamMatch> {
        match query_match {
            api::HttpQueryParamMatch::Exact { name, value } => {
                Ok(http_route::QueryParamMatch::Exact(name, value))
            }
            api::HttpQueryParamMatch::RegularExpression { name, value } => {
                Ok(http_route::QueryParamMatch::Regex(name, value.parse()?))
            }
        }
    }

    pub fn req_header_modifier(
        api::HttpRequestHeaderFilter { set, add, remove }: api::HttpRequestHeaderFilter,
    ) -> Result<http_route::RequestHeaderModifierFilter> {
        Ok(http_route::RequestHeaderModifierFilter {
            add: add
                .into_iter()
                .flatten()
                .map(|api::HttpHeader { name, value }| Ok((name.parse()?, value.parse()?)))
                .collect::<Result<Vec<_>>>()?,
            set: set
                .into_iter()
                .flatten()
                .map(|api::HttpHeader { name, value }| Ok((name.parse()?, value.parse()?)))
                .collect::<Result<Vec<_>>>()?,
            remove: remove
                .into_iter()
                .flatten()
                .map(http_route::HeaderName::try_from)
                .collect::<Result<_, _>>()?,
        })
    }

    pub fn req_redirect(
        api::HttpRequestRedirectFilter {
            scheme,
            hostname,
            path,
            port,
            status_code,
        }: api::HttpRequestRedirectFilter,
    ) -> Result<http_route::RequestRedirectFilter> {
        Ok(http_route::RequestRedirectFilter {
            scheme: scheme.as_deref().map(TryInto::try_into).transpose()?,
            host: hostname,
            path: path.map(path_modifier).transpose()?,
            port: port.and_then(|p| NonZeroU16::try_from(p).ok()),
            status: status_code
                .map(http_route::StatusCode::try_from)
                .transpose()?,
        })
    }

    fn path_modifier(path_modifier: api::HttpPathModifier) -> Result<http_route::PathModifier> {
        use api::HttpPathModifier::*;
        match path_modifier {
            ReplaceFullPath {
                replace_full_path: path,
            }
            | ReplacePrefixMatch {
                replace_prefix_match: path,
            } if !path.starts_with('/') => {
                bail!(
                    "RequestRedirect filters may only contain absolute paths \
                    (starting with '/'); {path:?} is not an absolute path"
                )
            }
            ReplaceFullPath { replace_full_path } => {
                Ok(http_route::PathModifier::Full(replace_full_path))
            }
            ReplacePrefixMatch {
                replace_prefix_match,
            } => Ok(http_route::PathModifier::Prefix(replace_prefix_match)),
        }
    }
}
