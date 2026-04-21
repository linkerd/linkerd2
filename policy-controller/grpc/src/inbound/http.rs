use super::to_authz;
use crate::routes;
use linkerd2_proxy_api::{inbound, meta};
use linkerd_policy_controller_core::{
    inbound::{Filter, HttpRoute, InboundRouteRule, RouteRef},
    IpNet,
};

pub(crate) fn to_route_list<'r>(
    routes: impl IntoIterator<Item = (&'r RouteRef, &'r HttpRoute)>,
    cluster_networks: &[IpNet],
) -> Vec<inbound::HttpRoute> {
    // Per the Gateway API spec:
    //
    // > If ties still exist across multiple Routes, matching precedence MUST be
    // > determined in order of the following criteria, continuing on ties:
    // >
    // >    The oldest Route based on creation timestamp.
    // >    The Route appearing first in alphabetical order by
    // >   "{namespace}/{name}".
    //
    // Note that we don't need to include the route's namespace in this
    // comparison, because all these routes will exist in the same
    // namespace.
    let mut route_list = routes.into_iter().collect::<Vec<_>>();
    route_list.sort_by(|(a_ref, a), (b_ref, b)| {
        let by_ts = match (&a.creation_timestamp, &b.creation_timestamp) {
            (Some(a_ts), Some(b_ts)) => a_ts.cmp(b_ts),
            (None, None) => std::cmp::Ordering::Equal,
            // Routes with timestamps are preferred over routes without.
            (Some(_), None) => return std::cmp::Ordering::Less,
            (None, Some(_)) => return std::cmp::Ordering::Greater,
        };
        by_ts.then_with(|| a_ref.cmp(b_ref))
    });

    route_list
        .into_iter()
        .map(|(route_ref, route)| to_http_route(route_ref, route.clone(), cluster_networks))
        .collect()
}

fn to_http_route(
    reference: &RouteRef,
    HttpRoute {
        hostnames,
        rules,
        authorizations,
        creation_timestamp: _,
    }: HttpRoute,
    cluster_networks: &[IpNet],
) -> inbound::HttpRoute {
    let metadata = meta::Metadata {
        kind: Some(match reference {
            RouteRef::Default(name) => meta::metadata::Kind::Default(name.to_string()),
            RouteRef::Resource(gkn) => meta::metadata::Kind::Resource(meta::Resource {
                group: gkn.group.to_string(),
                kind: gkn.kind.to_string(),
                name: gkn.name.to_string(),
                ..Default::default()
            }),
        }),
    };

    let hosts = hostnames
        .into_iter()
        .map(routes::convert_host_match)
        .collect();

    let rules = rules
        .into_iter()
        .map(
            |InboundRouteRule { matches, filters }| inbound::http_route::Rule {
                matches: matches
                    .into_iter()
                    .map(routes::http::convert_match)
                    .collect(),
                filters: filters
                    .into_iter()
                    .filter_map(convert_http_filter)
                    .collect(),
            },
        )
        .collect();

    let authorizations = authorizations
        .iter()
        .map(|(n, c)| to_authz(n, c, cluster_networks))
        .collect();

    inbound::HttpRoute {
        metadata: Some(metadata),
        hosts,
        rules,
        authorizations,
    }
}

fn convert_http_filter(filter: Filter) -> Option<inbound::http_route::Filter> {
    use inbound::http_route::filter::Kind;

    let kind = match filter {
        Filter::FailureInjector(f) => Some(Kind::FailureInjector(
            routes::http::convert_failure_injector_filter(f),
        )),
        Filter::RequestHeaderModifier(f) => Some(Kind::RequestHeaderModifier(
            routes::convert_request_header_modifier_filter(f),
        )),
        Filter::ResponseHeaderModifier(_) => None,
        Filter::RequestRedirect(f) => Some(Kind::Redirect(routes::convert_redirect_filter(f))),
    };

    kind.map(|kind| inbound::http_route::Filter { kind: Some(kind) })
}
