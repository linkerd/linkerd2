use k8s_gateway_api::ParentReference;
use linkerd_policy_controller_core::http_route::{
    HeaderMatch, Hostname, HttpMethod, HttpRoute, HttpRouteMatch, PathMatch, QueryParamMatch, Value,
};

#[derive(Clone, Debug, PartialEq)]
pub struct RouteBinding {
    pub route: HttpRoute,
    pub parent_refs: Vec<ParentReference>,
}

impl RouteBinding {
    pub fn from_resource(route: k8s_gateway_api::HttpRoute) -> Self {
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

        let matches = route
            .spec
            .rules
            .iter()
            .flatten()
            .filter_map(|rule| rule.matches.as_ref())
            .flatten()
            .map(|route_match| {
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
                        k8s_gateway_api::HttpQueryParamMatch::Exact { name, value } => {
                            QueryParamMatch {
                                name: name.to_owned(),
                                value: Value::Exact(value.to_owned()),
                            }
                        }
                        k8s_gateway_api::HttpQueryParamMatch::RegularExpression { name, value } => {
                            QueryParamMatch {
                                name: name.to_owned(),
                                value: Value::Regex(value.to_owned()),
                            }
                        }
                    })
                    .collect();

                let method = route_match.method.as_ref().and_then(|method| {
                    match method.to_lowercase().as_str() {
                        "connect" => Some(HttpMethod::CONNECT),
                        "get" => Some(HttpMethod::GET),
                        "post" => Some(HttpMethod::POST),
                        "put" => Some(HttpMethod::PUT),
                        "delete" => Some(HttpMethod::DELETE),
                        "patch" => Some(HttpMethod::PATCH),
                        "head" => Some(HttpMethod::HEAD),
                        "options" => Some(HttpMethod::OPTIONS),
                        "trace" => Some(HttpMethod::TRACE),
                        _ => None,
                    }
                });

                HttpRouteMatch {
                    path,
                    headers,
                    query_params,
                    method,
                }
            })
            .collect();

        RouteBinding {
            route: HttpRoute { hostnames, matches },
            parent_refs: route.spec.inner.parent_refs.unwrap_or_default(),
        }
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
}
