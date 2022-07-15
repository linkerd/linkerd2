use ahash::AHashMap as HashMap;
use anyhow::{bail, Error, Result};
use k8s_gateway_api as api;
use linkerd_policy_controller_core::http_route;

#[derive(Clone, Debug, PartialEq)]
pub struct InboundRouteBinding {
    pub parents: Vec<InboundParentRef>,
    pub route: http_route::InboundHttpRoute,
}

#[derive(Clone, Debug, PartialEq)]
pub enum InboundParentRef {
    Server(String),
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
        let parents = route
            .spec
            .inner
            .parent_refs
            .into_iter()
            .flatten()
            .filter_map(
                |api::ParentReference {
                     group,
                     kind,
                     namespace,
                     name,
                     section_name,
                     port,
                 }| {
                    // Ignore parents that are not a Server.
                    if let Some(g) = group {
                        if let Some(k) = kind {
                            if !g.eq_ignore_ascii_case("policy.linkerd.io")
                                || !k.eq_ignore_ascii_case("server")
                                || name.is_empty()
                            {
                                return None;
                            }
                        }
                    }

                    if namespace.is_some() && namespace != route.metadata.namespace {
                        return Some(Err(InvalidParentRef::ServerInAnotherNamespace));
                    }
                    if port.is_some() {
                        return Some(Err(InvalidParentRef::SpecifiesPort));
                    }
                    if section_name.is_some() {
                        return Some(Err(InvalidParentRef::SpecifiesSection));
                    }

                    Some(Ok(InboundParentRef::Server(name)))
                },
            )
            .collect::<Result<Vec<_>, InvalidParentRef>>()?;
        // If there are no valid parents, then the route is invalid.
        if parents.is_empty() {
            return Err(InvalidParentRef::DoesNotSelectServer.into());
        }

        let hostnames = route
            .spec
            .hostnames
            .into_iter()
            .flatten()
            .map(|hostname| {
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
            })
            .collect();

        let rules = route
            .spec
            .rules
            .into_iter()
            .flatten()
            .map(Self::try_rule)
            .collect::<Result<_>>()?;

        Ok(InboundRouteBinding {
            parents,
            route: http_route::InboundHttpRoute {
                hostnames,
                rules,
                authorizations: HashMap::default(),
            },
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

    fn try_match(
        api::HttpRouteMatch {
            path,
            headers,
            query_params,
            method,
        }: api::HttpRouteMatch,
    ) -> Result<http_route::HttpRouteMatch> {
        let path = path
            .map(|pm| match pm {
                api::HttpPathMatch::Exact { value } => Ok(http_route::PathMatch::Exact(value)),
                api::HttpPathMatch::PathPrefix { value } => {
                    Ok(http_route::PathMatch::Prefix(value))
                }
                api::HttpPathMatch::RegularExpression { value } => {
                    value.parse().map(http_route::PathMatch::Regex)
                }
            })
            .transpose()?;

        let headers = headers
            .into_iter()
            .flatten()
            .map(|hm| match hm {
                api::HttpHeaderMatch::Exact { name, value } => Ok(http_route::HeaderMatch::Exact(
                    name.parse()?,
                    value.parse()?,
                )),
                api::HttpHeaderMatch::RegularExpression { name, value } => Ok(
                    http_route::HeaderMatch::Regex(name.parse()?, value.parse()?),
                ),
            })
            .collect::<Result<_>>()?;

        let query_params = query_params
            .into_iter()
            .flatten()
            .map(|query_param| match query_param {
                api::HttpQueryParamMatch::Exact { name, value } => {
                    Ok(http_route::QueryParamMatch::Exact(name, value))
                }
                api::HttpQueryParamMatch::RegularExpression { name, value } => {
                    Ok(http_route::QueryParamMatch::Exact(name, value.parse()?))
                }
            })
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

    fn try_rule(rule: api::HttpRouteRule) -> Result<http_route::InboundHttpRouteRule> {
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

        Ok(http_route::InboundHttpRouteRule { matches, filters })
    }

    fn try_filter(filter: api::HttpRouteFilter) -> Result<http_route::InboundFilter> {
        let filter = match filter {
            api::HttpRouteFilter::RequestHeaderModifier {
                request_header_modifier: api::HttpRequestHeaderFilter { set, add, remove },
            } => http_route::InboundFilter::RequestHeaderModifier(
                http_route::RequestHeaderModifierFilter {
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
                },
            ),

            api::HttpRouteFilter::RequestRedirect {
                request_redirect:
                    api::HttpRequestRedirectFilter {
                        scheme,
                        hostname,
                        path,
                        port,
                        status_code,
                    },
            } => http_route::InboundFilter::RequestRedirect(http_route::RequestRedirectFilter {
                scheme: scheme.as_deref().map(TryInto::try_into).transpose()?,
                host: hostname,
                path: path.map(|path_mod| match path_mod {
                    api::HttpPathModifier::ReplaceFullPath(s) => http_route::PathModifier::Full(s),
                    api::HttpPathModifier::ReplacePrefixMatch(s) => {
                        http_route::PathModifier::Prefix(s)
                    }
                }),
                port: port.map(Into::into),
                status: status_code.map(TryFrom::try_from).transpose()?,
            }),

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
}
