use anyhow::{bail, Result};
use k8s_gateway_api::ParentReference;
use linkerd_policy_controller_core::http_route::{
    HeaderMatch, Hostname, HttpFilter, HttpRoute, HttpRouteMatch, HttpRouteRule, PathMatch,
    PathModifier, QueryParamMatch, Value,
};

#[derive(Clone, Debug, PartialEq)]
pub struct RouteBinding {
    pub route: HttpRoute,
    pub parent_refs: Vec<ParentReference>,
}

impl RouteBinding {
    pub fn try_from_resource(route: k8s_gateway_api::HttpRoute) -> Result<Self> {
        let hostnames = route
            .spec
            .hostnames
            .iter()
            .flatten()
            .map(|hostname| {
                if hostname.starts_with("*.") {
                    let mut reverse_labels = hostname
                        .split('.')
                        .skip(1)
                        .map(|label| label.to_owned())
                        .collect::<Vec<String>>();
                    reverse_labels.reverse();
                    Hostname::Suffix { reverse_labels }
                } else {
                    Hostname::Exact(hostname.to_owned())
                }
            })
            .collect();

        let rules = route
            .spec
            .rules
            .into_iter()
            .flatten()
            .map(Self::try_rule)
            .collect::<Result<_>>()?;

        Ok(RouteBinding {
            route: HttpRoute { hostnames, rules },
            parent_refs: route.spec.inner.parent_refs.unwrap_or_default(),
        })
    }

    pub fn selects_server(&self, name: &str) -> bool {
        for parent_ref in self.parent_refs.iter() {
            if parent_ref.group.as_deref() == Some("policy.linkerd.io")
                && parent_ref.kind.as_deref() == Some("Server")
                && parent_ref.name == name
            {
                return true;
            }
        }
        false
    }

    fn try_match(route_match: k8s_gateway_api::HttpRouteMatch) -> Result<HttpRouteMatch> {
        let path = route_match
            .path
            .as_ref()
            .map(|path_match| match path_match {
                k8s_gateway_api::HttpPathMatch::Exact { value } => {
                    PathMatch::Exact(value.to_owned())
                }
                k8s_gateway_api::HttpPathMatch::PathPrefix { value } => {
                    PathMatch::Prefix(value.to_owned())
                }
                k8s_gateway_api::HttpPathMatch::RegularExpression { value } => {
                    PathMatch::Regex(value.to_owned())
                }
            });

        let headers = route_match
            .headers
            .iter()
            .flatten()
            .map(|header_match| match header_match {
                k8s_gateway_api::HttpHeaderMatch::Exact { name, value } => HeaderMatch {
                    name: name.to_owned(),
                    value: Value::Exact(value.to_owned()),
                },
                k8s_gateway_api::HttpHeaderMatch::RegularExpression { name, value } => {
                    HeaderMatch {
                        name: name.to_owned(),
                        value: Value::Regex(value.to_owned()),
                    }
                }
            })
            .collect();

        let query_params = route_match
            .query_params
            .iter()
            .flatten()
            .map(|query_param| match query_param {
                k8s_gateway_api::HttpQueryParamMatch::Exact { name, value } => QueryParamMatch {
                    name: name.to_owned(),
                    value: Value::Exact(value.to_owned()),
                },
                k8s_gateway_api::HttpQueryParamMatch::RegularExpression { name, value } => {
                    QueryParamMatch {
                        name: name.to_owned(),
                        value: Value::Regex(value.to_owned()),
                    }
                }
            })
            .collect();

        let method = match route_match.method {
            Some(m) => Some(m.as_str().try_into()?),
            None => None,
        };

        Ok(HttpRouteMatch {
            path,
            headers,
            query_params,
            method,
        })
    }

    fn try_rule(rule: k8s_gateway_api::HttpRouteRule) -> Result<HttpRouteRule> {
        let matches = rule
            .matches
            .into_iter()
            .flatten()
            .map(Self::try_match)
            .collect::<Result<_>>()?;

        let filters = rule
            .filters
            .into_iter()
            .flatten()
            .map(Self::try_filter)
            .collect::<Result<_>>()?;

        Ok(HttpRouteRule { matches, filters })
    }

    fn try_filter(filter: k8s_gateway_api::HttpRouteFilter) -> Result<HttpFilter> {
        let filter = match filter {
            k8s_gateway_api::HttpRouteFilter::RequestHeaderModifier {
                request_header_modifier:
                    k8s_gateway_api::HttpRequestHeaderFilter { set, add, remove },
            } => HttpFilter::RequestHeaderModifier {
                add: add
                    .into_iter()
                    .flatten()
                    .map(|header| (header.name, header.value))
                    .collect(),
                set: set
                    .into_iter()
                    .flatten()
                    .map(|header| (header.name, header.value))
                    .collect(),
                remove: remove.unwrap_or_default(),
            },
            k8s_gateway_api::HttpRouteFilter::RequestMirror { .. } => {
                bail!("RequestMirror filter is not supported")
            }
            k8s_gateway_api::HttpRouteFilter::RequestRedirect {
                request_redirect:
                    k8s_gateway_api::HttpRequestRedirectFilter {
                        scheme,
                        hostname,
                        path,
                        port,
                        status_code,
                    },
            } => HttpFilter::RequestRedirect {
                scheme: scheme.as_deref().map(TryInto::try_into).transpose()?,
                host: hostname,
                path: path.map(|path_mod| match path_mod {
                    k8s_gateway_api::HttpPathModifier::ReplaceFullPath(s) => PathModifier::Full(s),
                    k8s_gateway_api::HttpPathModifier::ReplacePrefixMatch(s) => {
                        PathModifier::Prefix(s)
                    }
                }),
                port: port.map(Into::into),
                status: status_code.map(TryFrom::try_from).transpose()?,
            },
            k8s_gateway_api::HttpRouteFilter::URLRewrite { .. } => {
                bail!("URLRewrite filter is not supported")
            }
            k8s_gateway_api::HttpRouteFilter::ExtensionRef { .. } => {
                bail!("ExtensionRef filter is not supported")
            }
        };
        Ok(filter)
    }
}
