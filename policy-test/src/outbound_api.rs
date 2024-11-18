use crate::{assert_resource_meta, grpc, Resource};
use kube::ResourceExt;
use std::time::Duration;
use tokio::time;

pub async fn retry_watch_outbound_policy(
    client: &kube::Client,
    ns: &str,
    resource: &Resource,
    port: u16,
) -> tonic::Streaming<grpc::outbound::OutboundPolicy> {
    // Port-forward to the control plane and start watching the service's
    // outbound policy.
    let mut policy_api = grpc::OutboundPolicyClient::port_forwarded(client).await;
    loop {
        match policy_api.watch_ip(ns, &resource.ip(), port).await {
            Ok(rx) => return rx,
            Err(error) => {
                tracing::error!(
                    ?error,
                    ns,
                    resource = resource.name(),
                    "failed to watch outbound policy for port 4191"
                );
                time::sleep(Duration::from_secs(1)).await;
            }
        }
    }
}

// detect_http_routes asserts that the given outbound policy has a proxy protcol
// of "Detect" and then invokes the given function with the Http1 and Http2
// routes from the Detect.
#[track_caller]
pub fn detect_http_routes<F>(config: &grpc::outbound::OutboundPolicy, f: F)
where
    F: Fn(&[grpc::outbound::HttpRoute]),
{
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Detect(grpc::outbound::proxy_protocol::Detect {
        opaque: _,
        timeout: _,
        http1,
        http2,
    }) = kind
    {
        let http1 = http1
            .as_ref()
            .expect("proxy protocol must have http1 field");
        let http2 = http2
            .as_ref()
            .expect("proxy protocol must have http2 field");
        f(&http1.routes);
        f(&http2.routes);
    } else {
        panic!("proxy protocol must be Detect; actually got:\n{kind:#?}")
    }
}

#[track_caller]
pub fn grpc_routes(config: &grpc::outbound::OutboundPolicy) -> &[grpc::outbound::GrpcRoute] {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Grpc(grpc::outbound::proxy_protocol::Grpc {
        routes,
        failure_accrual: _,
    }) = kind
    {
        routes
    } else {
        panic!("proxy protocol must be Grpc; actually got:\n{kind:#?}")
    }
}

#[track_caller]
pub fn detect_failure_accrual<F>(config: &grpc::outbound::OutboundPolicy, f: F)
where
    F: Fn(Option<&grpc::outbound::FailureAccrual>),
{
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Detect(grpc::outbound::proxy_protocol::Detect {
        opaque: _,
        timeout: _,
        http1,
        http2,
    }) = kind
    {
        let http1 = http1
            .as_ref()
            .expect("proxy protocol must have http1 field");
        let http2 = http2
            .as_ref()
            .expect("proxy protocol must have http2 field");
        f(http1.failure_accrual.as_ref());
        f(http2.failure_accrual.as_ref());
    } else {
        panic!("proxy protocol must be Detect; actually got:\n{kind:#?}")
    }
}

#[track_caller]
pub fn failure_accrual_consecutive(
    accrual: Option<&grpc::outbound::FailureAccrual>,
) -> &grpc::outbound::failure_accrual::ConsecutiveFailures {
    assert!(
        accrual.is_some(),
        "failure accrual must be configured for service"
    );
    let kind = accrual
        .unwrap()
        .kind
        .as_ref()
        .expect("failure accrual must have kind");
    let grpc::outbound::failure_accrual::Kind::ConsecutiveFailures(accrual) = kind;
    accrual
}

#[track_caller]
pub fn route_backends_first_available(
    route: &grpc::outbound::HttpRoute,
) -> &[grpc::outbound::http_route::RouteBackend] {
    let kind = assert_singleton(&route.rules)
        .backends
        .as_ref()
        .expect("Rule must have backends")
        .kind
        .as_ref()
        .expect("Backend must have kind");
    match kind {
        grpc::outbound::http_route::distribution::Kind::FirstAvailable(fa) => &fa.backends,
        _ => panic!("Distribution must be FirstAvailable"),
    }
}

#[track_caller]
pub fn route_backends_random_available(
    route: &grpc::outbound::HttpRoute,
) -> &[grpc::outbound::http_route::WeightedRouteBackend] {
    let kind = assert_singleton(&route.rules)
        .backends
        .as_ref()
        .expect("Rule must have backends")
        .kind
        .as_ref()
        .expect("Backend must have kind");
    match kind {
        grpc::outbound::http_route::distribution::Kind::RandomAvailable(dist) => &dist.backends,
        _ => panic!("Distribution must be RandomAvailable"),
    }
}

#[track_caller]
pub fn route_name(route: &grpc::outbound::HttpRoute) -> &str {
    match route.metadata.as_ref().unwrap().kind.as_ref().unwrap() {
        grpc::meta::metadata::Kind::Resource(grpc::meta::Resource { ref name, .. }) => name,
        _ => panic!("route must be a resource kind"),
    }
}

#[track_caller]
pub fn assert_backend_has_failure_filter(
    backend: &grpc::outbound::http_route::WeightedRouteBackend,
) {
    let filter = assert_singleton(&backend.backend.as_ref().unwrap().filters);
    match filter.kind.as_ref().unwrap() {
        grpc::outbound::http_route::filter::Kind::FailureInjector(_) => {}
        _ => panic!("backend must have FailureInjector filter"),
    };
}

#[track_caller]
pub fn assert_route_is_default(route: &grpc::outbound::HttpRoute, parent: &Resource, port: u16) {
    let kind = route.metadata.as_ref().unwrap().kind.as_ref().unwrap();
    match kind {
        grpc::meta::metadata::Kind::Default(_) => {}
        grpc::meta::metadata::Kind::Resource(r) => {
            panic!("route expected to be default but got resource {r:?}")
        }
    }

    let backends = route_backends_first_available(route);
    let backend = assert_singleton(backends);
    assert_backend_matches_parent(backend, parent, port);

    let rule = assert_singleton(&route.rules);
    let route_match = assert_singleton(&rule.matches);
    let path_match = route_match.path.as_ref().unwrap().kind.as_ref().unwrap();
    assert_eq!(
        *path_match,
        grpc::http_route::path_match::Kind::Prefix("/".to_string())
    );
}

#[track_caller]
pub fn assert_backend_matches_parent(
    backend: &grpc::outbound::http_route::RouteBackend,
    parent: &Resource,
    port: u16,
) {
    let backend = backend.backend.as_ref().unwrap();

    match parent {
        Resource::Service(svc) => {
            let dst = match backend.kind.as_ref().unwrap() {
                grpc::outbound::backend::Kind::Balancer(balance) => {
                    let kind = balance.discovery.as_ref().unwrap().kind.as_ref().unwrap();
                    match kind {
                        grpc::outbound::backend::endpoint_discovery::Kind::Dst(dst) => &dst.path,
                    }
                }
                grpc::outbound::backend::Kind::Forward(_) => {
                    panic!("service default route backend must be Balancer")
                }
            };
            assert_eq!(
                *dst,
                format!(
                    "{}.{}.svc.{}:{}",
                    svc.name_unchecked(),
                    svc.namespace().unwrap(),
                    "cluster.local",
                    port
                )
            );
        }

        Resource::EgressNetwork(_) => {
            match backend.kind.as_ref().unwrap() {
                grpc::outbound::backend::Kind::Forward(_) => {}
                grpc::outbound::backend::Kind::Balancer(_) => {
                    panic!("egress net default route backend must be Forward")
                }
            };
        }
    }

    assert_resource_meta(&backend.metadata, parent, port)
}

#[track_caller]
pub fn assert_singleton<T>(ts: &[T]) -> &T {
    assert_eq!(ts.len(), 1);
    ts.first().unwrap()
}

#[track_caller]
pub fn assert_route_attached<'a, T>(routes: &'a [T], parent: &Resource) -> &'a T {
    match parent {
        Resource::EgressNetwork(_) => {
            assert_eq!(routes.len(), 2);
            routes.first().unwrap()
        }
        Resource::Service(_) => assert_singleton(routes),
    }
}

#[track_caller]
pub fn assert_route_name_eq(route: &grpc::outbound::HttpRoute, name: &str) {
    assert_name_eq(route.metadata.as_ref().unwrap(), name)
}

#[track_caller]
pub fn assert_name_eq(meta: &grpc::meta::Metadata, name: &str) {
    let kind = meta.kind.as_ref().unwrap();
    match kind {
        grpc::meta::metadata::Kind::Default(d) => {
            panic!("route expected to not be default, but got default {d:?}")
        }
        grpc::meta::metadata::Kind::Resource(resource) => assert_eq!(resource.name, *name),
    }
}
