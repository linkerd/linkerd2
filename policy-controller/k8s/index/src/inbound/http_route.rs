use crate::http_route;
use ahash::AHashMap as HashMap;
use anyhow::{bail, Error, Result};
use k8s_gateway_api as api;
use linkerd_policy_controller_core::http_route::{HttpRouteMatch, Method};
use linkerd_policy_controller_core::inbound::{Filter, HttpRoute, HttpRouteRule};
use linkerd_policy_controller_core::POLICY_CONTROLLER_NAME;
use linkerd_policy_controller_k8s_api::{
    self as k8s, gateway,
    policy::{httproute as policy, Server},
};
use std::fmt;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RouteBinding {
    pub parents: Vec<ParentRef>,
    pub route: HttpRoute,
    pub statuses: Vec<Status>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ParentRef {
    Server(String),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Status {
    pub parent: ParentRef,
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
    #[error("HTTPRoute resource may not reference a parent Server in an other namespace")]
    ServerInAnotherNamespace,

    #[error("HTTPRoute resource may not reference a parent by port")]
    SpecifiesPort,

    #[error("HTTPRoute resource may not reference a parent by section name")]
    SpecifiesSection,
}

impl TryFrom<api::HttpRoute> for RouteBinding {
    type Error = Error;

    fn try_from(route: api::HttpRoute) -> Result<Self, Self::Error> {
        let route_ns = route.metadata.namespace.as_deref();
        let creation_timestamp = route.metadata.creation_timestamp.map(|k8s::Time(t)| t);
        let parents = ParentRef::collect_from(route_ns, route.spec.inner.parent_refs)?;
        let hostnames = route
            .spec
            .hostnames
            .into_iter()
            .flatten()
            .map(http_route::host_match)
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

        Ok(RouteBinding {
            parents,
            route: HttpRoute {
                hostnames,
                rules,
                authorizations: HashMap::default(),
                creation_timestamp,
            },
            statuses,
        })
    }
}

impl TryFrom<policy::HttpRoute> for RouteBinding {
    type Error = Error;

    fn try_from(route: policy::HttpRoute) -> Result<Self, Self::Error> {
        let route_ns = route.metadata.namespace.as_deref();
        let creation_timestamp = route.metadata.creation_timestamp.map(|k8s::Time(t)| t);
        let parents = ParentRef::collect_from(route_ns, route.spec.inner.parent_refs)?;
        let hostnames = route
            .spec
            .hostnames
            .into_iter()
            .flatten()
            .map(http_route::host_match)
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

        Ok(RouteBinding {
            parents,
            route: HttpRoute {
                hostnames,
                rules,
                authorizations: HashMap::default(),
                creation_timestamp,
            },
            statuses,
        })
    }
}

impl RouteBinding {
    #[inline]
    pub fn selects_server(&self, name: &str) -> bool {
        self.parents
            .iter()
            .any(|p| matches!(p, ParentRef::Server(n) if n == name))
    }

    #[inline]
    pub fn accepted_by_server(&self, name: &str) -> bool {
        self.statuses.iter().any(|status| {
            status.parent == ParentRef::Server(name.to_string())
                && status
                    .conditions
                    .iter()
                    .any(|condition| condition.type_ == ConditionType::Accepted && condition.status)
        })
    }

    pub fn try_match(
        api::HttpRouteMatch {
            path,
            headers,
            query_params,
            method,
        }: api::HttpRouteMatch,
    ) -> Result<HttpRouteMatch> {
        let path = path.map(http_route::path_match).transpose()?;

        let headers = headers
            .into_iter()
            .flatten()
            .map(http_route::header_match)
            .collect::<Result<_>>()?;

        let query_params = query_params
            .into_iter()
            .flatten()
            .map(http_route::query_param_match)
            .collect::<Result<_>>()?;

        let method = method.as_deref().map(Method::try_from).transpose()?;

        Ok(HttpRouteMatch {
            path,
            headers,
            query_params,
            method,
        })
    }

    fn try_rule<F>(
        matches: Option<Vec<api::HttpRouteMatch>>,
        filters: Option<Vec<F>>,
        try_filter: impl Fn(F) -> Result<Filter>,
    ) -> Result<HttpRouteRule> {
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

        Ok(HttpRouteRule { matches, filters })
    }

    fn try_gateway_filter(filter: api::HttpRouteFilter) -> Result<Filter> {
        let filter = match filter {
            api::HttpRouteFilter::RequestHeaderModifier {
                request_header_modifier,
            } => {
                let filter = http_route::header_modifier(request_header_modifier)?;
                Filter::RequestHeaderModifier(filter)
            }

            api::HttpRouteFilter::ResponseHeaderModifier {
                response_header_modifier,
            } => {
                let filter = http_route::header_modifier(response_header_modifier)?;
                Filter::ResponseHeaderModifier(filter)
            }

            api::HttpRouteFilter::RequestRedirect { request_redirect } => {
                let filter = http_route::req_redirect(request_redirect)?;
                Filter::RequestRedirect(filter)
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

    fn try_policy_filter(filter: policy::HttpRouteFilter) -> Result<Filter> {
        let filter = match filter {
            policy::HttpRouteFilter::RequestHeaderModifier {
                request_header_modifier,
            } => {
                let filter = http_route::header_modifier(request_header_modifier)?;
                Filter::RequestHeaderModifier(filter)
            }

            policy::HttpRouteFilter::ResponseHeaderModifier {
                response_header_modifier,
            } => {
                let filter = http_route::header_modifier(response_header_modifier)?;
                Filter::ResponseHeaderModifier(filter)
            }

            policy::HttpRouteFilter::RequestRedirect { request_redirect } => {
                let filter = http_route::req_redirect(request_redirect)?;
                Filter::RequestRedirect(filter)
            }
        };
        Ok(filter)
    }
}

impl ParentRef {
    fn collect_from(
        route_ns: Option<&str>,
        parent_refs: Option<Vec<api::ParentReference>>,
    ) -> Result<Vec<Self>, InvalidParentRef> {
        let parents = parent_refs
            .into_iter()
            .flatten()
            .filter_map(|parent_ref| Self::from_parent_ref(route_ns, parent_ref))
            .collect::<Result<Vec<_>, InvalidParentRef>>()?;

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

        Some(Ok(ParentRef::Server(name)))
    }
}

impl Status {
    pub fn collect_from(status: gateway::RouteStatus) -> Vec<Self> {
        status
            .parents
            .iter()
            .filter(|status| status.controller_name == POLICY_CONTROLLER_NAME)
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
            parent: ParentRef::Server(status.parent_ref.name.to_string()),
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
