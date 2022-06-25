use linkerd_policy_controller_core::http_route::{
    HeaderMatch, Hostname, HttpMethod, HttpRoute, HttpRouteMatch, PathMatch, QueryParamMatch, Value,
};

fn try_http_route_from_resource(route: k8s_gateway_api::HttpRouteSpec) -> HttpRoute {
    let hostnames = route
        .hostnames
        .iter()
        .flatten()
        .map(|hostname| {
            if hostname.starts_with("*.") {
                let mut reverse_labels = hostname
                    .split(".")
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
        .rules
        .iter()
        .flatten()
        .map(|rule| rule.matches)
        .flatten()
        .flatten()
        .map(|route_match| {
            let path = route_match.path.map(|path_match| match path_match {
                k8s_gateway_api::HttpPathMatch::Exact { value } => PathMatch::Exact(value),
                k8s_gateway_api::HttpPathMatch::PathPrefix { value } => PathMatch::Prefix(value),
                k8s_gateway_api::HttpPathMatch::RegularExpression { value } => {
                    PathMatch::Regex(value)
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

            let method = route_match
                .method
                .map(|method| match method.to_lowercase().as_str() {
                    "connect" => HttpMethod::CONNECT,
                    "get" => HttpMethod::GET,
                    "post" => HttpMethod::POST,
                    "put" => HttpMethod::PUT,
                    "delete" => HttpMethod::DELETE,
                    "patch" => HttpMethod::PATCH,
                    "head" => HttpMethod::HEAD,
                    "options" => HttpMethod::OPTIONS,
                    "connect" => HttpMethod::CONNECT,
                    "trace" => HttpMethod::TRACE,
                });

            HttpRouteMatch {
                path,
                headers,
                query_params,
                method,
            }
        })
        .collect();

    HttpRoute { hostnames, matches }
}
