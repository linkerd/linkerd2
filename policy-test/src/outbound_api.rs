use crate::{grpc, test_route::TestRoute};
use linkerd_policy_controller_k8s_api::gateway;
use std::time::Duration;
use tokio::time;

pub async fn retry_watch_outbound_policy(
    client: &kube::Client,
    ns: &str,
    ip: &str,
    port: u16,
) -> tonic::Streaming<grpc::outbound::OutboundPolicy> {
    // Port-forward to the control plane and start watching the service's
    // outbound policy.
    let mut policy_api = grpc::OutboundPolicyClient::port_forwarded(client).await;
    loop {
        match policy_api.watch_ip(ns, ip, port).await {
            Ok(rx) => return rx,
            Err(error) => {
                tracing::error!(?error, ns, ip, port, "failed to watch outbound policy");
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
pub fn http1_routes(config: &grpc::outbound::OutboundPolicy) -> &[grpc::outbound::HttpRoute] {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Http1(grpc::outbound::proxy_protocol::Http1 {
        routes,
        failure_accrual: _,
    }) = kind
    {
        routes
    } else {
        panic!("proxy protocol must be Http1; actually got:\n{kind:#?}")
    }
}

#[track_caller]
pub fn http2_routes(config: &grpc::outbound::OutboundPolicy) -> &[grpc::outbound::HttpRoute] {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Http2(grpc::outbound::proxy_protocol::Http2 {
        routes,
        failure_accrual: _,
    }) = kind
    {
        routes
    } else {
        panic!("proxy protocol must be Http2; actually got:\n{kind:#?}")
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

#[cfg(feature = "gateway-api-experimental")]
#[track_caller]
pub fn tls_routes(config: &grpc::outbound::OutboundPolicy) -> &[grpc::outbound::TlsRoute] {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Tls(grpc::outbound::proxy_protocol::Tls {
        routes,
    }) = kind
    {
        routes
    } else {
        panic!("proxy protocol must be Tls; actually got:\n{kind:#?}")
    }
}

#[cfg(feature = "gateway-api-experimental")]
#[track_caller]
pub fn tcp_routes(config: &grpc::outbound::OutboundPolicy) -> &[grpc::outbound::OpaqueRoute] {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    if let grpc::outbound::proxy_protocol::Kind::Opaque(grpc::outbound::proxy_protocol::Opaque {
        routes,
    }) = kind
    {
        routes
    } else {
        panic!("proxy protocol must be Opaque; actually got:\n{kind:#?}")
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
    match accrual
        .expect("failure accrual must be configured for service")
        .kind
        .as_ref()
        .expect("failure accrual must have a kind")
    {
        grpc::outbound::failure_accrual::Kind::ConsecutiveFailures(cf) => cf,
        other => panic!("failure accrual must be consecutive; actually got:\n{other:#?}"),
    }
}

#[track_caller]
pub fn failure_accrual_unified(
    accrual: Option<&grpc::outbound::FailureAccrual>,
) -> &grpc::outbound::failure_accrual::Unified {
    match accrual
        .expect("failure accrual must be configured for service")
        .kind
        .as_ref()
        .expect("failure accrual must have a kind")
    {
        grpc::outbound::failure_accrual::Kind::Unified(unified) => unified,
        other => panic!("failure accrual must be unified; actually got:\n{other:#?}"),
    }
}

/// A view of the penalty values held by a service balancer's
/// PenaltyPeakEwma load estimator.
pub struct LoadBias {
    pub penalty: Option<prost_types::Duration>,
    pub penalty_decay: Option<prost_types::Duration>,
}

/// A view of the Retry-After cap held by a service balancer's
/// PenaltyPeakEwma load estimator.
pub struct RetryAfter {
    pub max_duration: Option<prost_types::Duration>,
}

/// Assert that a `LoadBias` view has the default penalty estimator, a 5s
/// base penalty decaying over 10s.
#[track_caller]
pub fn assert_default_penalty_estimator(load_bias: &LoadBias) {
    assert_eq!(
        load_bias.penalty,
        Some(std::time::Duration::from_secs(5).try_into().unwrap()),
        "default penalty should be 5s"
    );
    assert_eq!(
        load_bias.penalty_decay,
        Some(std::time::Duration::from_secs(10).try_into().unwrap()),
        "default penalty_decay should be 10s"
    );
}

/// Assert that a `RetryAfter` view has the default 300s cap.
#[track_caller]
pub fn assert_default_retry_after_cap(retry_after: &RetryAfter) {
    assert_eq!(
        retry_after.max_duration,
        Some(std::time::Duration::from_secs(300).try_into().unwrap()),
        "default max_duration should be 300s"
    );
}

/// Extract the penalty and Retry-After cap held by a service's default
/// backend balancer load estimator.
///
/// Response penalization uses the PenaltyPeakEwma estimator, which holds
/// the penalty and its decay. The Retry-After cap shares the same estimator, so
/// it appears here only when penalization is also enabled. Honoring Retry-After
/// on its own leaves the plain PeakEwma estimator in place and is observed
/// through respect_retry_after_hint on the failure-accrual backoff instead.
#[track_caller]
pub fn detect_load_bias_and_retry_after(
    config: &grpc::outbound::OutboundPolicy,
) -> (Option<LoadBias>, Option<RetryAfter>) {
    interpret_balancer_load(default_backend_balancer_load(config))
}

/// Interpret a balancer load estimator into its penalty and Retry-After views.
///
/// The PenaltyPeakEwma estimator holds the penalty, its decay, and the
/// Retry-After cap. Plain PeakEwma (or no balancer at all, as with
/// EgressNetwork forwarding) means no penalty and no cap.
fn interpret_balancer_load(
    load: Option<grpc::outbound::backend::balance_p2c::Load>,
) -> (Option<LoadBias>, Option<RetryAfter>) {
    match load {
        Some(grpc::outbound::backend::balance_p2c::Load::PenaltyPeakEwma(penalty)) => {
            let load_bias = penalty.penalty.is_some().then_some(LoadBias {
                penalty: penalty.penalty,
                penalty_decay: penalty.penalty_decay,
            });
            let retry_after = penalty.max_retry_after.map(|max_duration| RetryAfter {
                max_duration: Some(max_duration),
            });
            (load_bias, retry_after)
        }
        _ => (None, None),
    }
}

/// Extract the penalty and Retry-After cap held by the default backend
/// balancer of a service served as an opaque proxy protocol.
///
/// An opaque service surfaces under the Opaque proxy-protocol kind rather than
/// Detect, so its generated default backend is reached through the opaque
/// route instead of the default HTTP route.
#[track_caller]
pub fn opaque_default_backend_load_bias_and_retry_after(
    config: &grpc::outbound::OutboundPolicy,
) -> (Option<LoadBias>, Option<RetryAfter>) {
    interpret_balancer_load(opaque_default_backend_balancer_load(config))
}

/// Locate the default backend balancer of an Opaque proxy protocol and return
/// its load estimator, if the backend is a balancer.
#[track_caller]
fn opaque_default_backend_balancer_load(
    config: &grpc::outbound::OutboundPolicy,
) -> Option<grpc::outbound::backend::balance_p2c::Load> {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");

    let routes = if let grpc::outbound::proxy_protocol::Kind::Opaque(
        grpc::outbound::proxy_protocol::Opaque { routes },
    ) = kind
    {
        routes
    } else {
        panic!("proxy protocol must be Opaque; actually got:\n{kind:#?}")
    };

    let backend = routes
        .iter()
        .find_map(opaque_default_route_backend)
        .expect("opaque default route must have a backend");

    match backend.kind.as_ref() {
        Some(grpc::outbound::backend::Kind::Balancer(balancer)) => balancer.load,
        _ => None,
    }
}

/// Return the first-available backend of a generated default opaque route.
fn opaque_default_route_backend(
    route: &grpc::outbound::OpaqueRoute,
) -> Option<&grpc::outbound::Backend> {
    let is_default = matches!(
        route.metadata.as_ref().and_then(|m| m.kind.as_ref()),
        Some(grpc::meta::metadata::Kind::Default(_))
    );
    if !is_default {
        return None;
    }

    route.rules.iter().find_map(|rule| {
        let dist = rule.backends.as_ref()?.kind.as_ref()?;
        let grpc::outbound::opaque_route::distribution::Kind::FirstAvailable(first) = dist else {
            return None;
        };
        first.backends.first()?.backend.as_ref()
    })
}

/// Extract the penalty and Retry-After cap held by the load estimator of a
/// non-default route's first Service backend balancer.
///
/// Routed services send traffic through route backends rather than the
/// generated default backend, so this navigates the first real (Resource)
/// HTTP route to its first balancer backend.
#[track_caller]
pub fn route_backend_load_bias_and_retry_after(
    config: &grpc::outbound::OutboundPolicy,
) -> (Option<LoadBias>, Option<RetryAfter>) {
    interpret_balancer_load(route_backend_balancer_load(config))
}

/// Locate the service's default (non-route) backend balancer and return its
/// load estimator, if the backend is a balancer.
#[track_caller]
fn default_backend_balancer_load(
    config: &grpc::outbound::OutboundPolicy,
) -> Option<grpc::outbound::backend::balance_p2c::Load> {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");

    // For a Detect protocol the default backend is reached through the
    // generated default HTTP route. Both http1 and http2 share the same
    // backend, so http1 is sufficient.
    let routes = if let grpc::outbound::proxy_protocol::Kind::Detect(
        grpc::outbound::proxy_protocol::Detect { http1, .. },
    ) = kind
    {
        &http1
            .as_ref()
            .expect("proxy protocol must have http1 field")
            .routes
    } else {
        panic!("proxy protocol must be Detect; actually got:\n{kind:#?}")
    };

    let backend = routes
        .iter()
        .find_map(default_route_backend)
        .expect("default route must have a backend");

    match backend.kind.as_ref() {
        Some(grpc::outbound::backend::Kind::Balancer(balancer)) => balancer.load,
        _ => None,
    }
}

/// Return the first-available backend of a generated default HTTP route.
fn default_route_backend(route: &grpc::outbound::HttpRoute) -> Option<&grpc::outbound::Backend> {
    let is_default = matches!(
        route.metadata.as_ref().and_then(|m| m.kind.as_ref()),
        Some(grpc::meta::metadata::Kind::Default(_))
    );
    if !is_default {
        return None;
    }

    route.rules.iter().find_map(|rule| {
        let dist = rule.backends.as_ref()?.kind.as_ref()?;
        let grpc::outbound::http_route::distribution::Kind::FirstAvailable(first) = dist else {
            return None;
        };
        first.backends.first()?.backend.as_ref()
    })
}

/// Locate the first balancer backend of a non-default (Resource) HTTP route and
/// return its load estimator.
#[track_caller]
fn route_backend_balancer_load(
    config: &grpc::outbound::OutboundPolicy,
) -> Option<grpc::outbound::backend::balance_p2c::Load> {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");

    // A routed service is served as a Detect protocol. http1 and http2 share
    // the same routes, so http1 is sufficient.
    let routes = if let grpc::outbound::proxy_protocol::Kind::Detect(
        grpc::outbound::proxy_protocol::Detect { http1, .. },
    ) = kind
    {
        &http1
            .as_ref()
            .expect("proxy protocol must have http1 field")
            .routes
    } else {
        panic!("proxy protocol must be Detect; actually got:\n{kind:#?}")
    };

    let backend = routes
        .iter()
        .find_map(route_backend)
        .expect("a route must have a balancer backend");

    match backend.kind.as_ref() {
        Some(grpc::outbound::backend::Kind::Balancer(balancer)) => balancer.load,
        _ => None,
    }
}

/// Return the first balancer backend of a non-default (Resource) HTTP route.
fn route_backend(route: &grpc::outbound::HttpRoute) -> Option<&grpc::outbound::Backend> {
    let is_default = matches!(
        route.metadata.as_ref().and_then(|m| m.kind.as_ref()),
        Some(grpc::meta::metadata::Kind::Default(_))
    );
    if is_default {
        return None;
    }

    // A route with real Service backends emits a RandomAvailable distribution
    // of weighted backends.
    route.rules.iter().find_map(|rule| {
        let dist = rule.backends.as_ref()?.kind.as_ref()?;
        let grpc::outbound::http_route::distribution::Kind::RandomAvailable(random) = dist else {
            return None;
        };
        random.backends.iter().find_map(|weighted| {
            let backend = weighted.backend.as_ref()?.backend.as_ref()?;
            matches!(
                backend.kind.as_ref(),
                Some(grpc::outbound::backend::Kind::Balancer(_))
            )
            .then_some(backend)
        })
    })
}

/// Extract the penalty and Retry-After cap held by the load estimator of a
/// non-default gRPC route's first Service backend balancer.
///
/// Routed services send traffic through route backends rather than the
/// generated default backend, so this navigates the first real (Resource)
/// gRPC route to its first balancer backend.
#[track_caller]
pub fn grpc_route_backend_load_bias_and_retry_after(
    config: &grpc::outbound::OutboundPolicy,
) -> (Option<LoadBias>, Option<RetryAfter>) {
    interpret_balancer_load(grpc_route_backend_balancer_load(config))
}

/// Locate the first balancer backend of a non-default (Resource) gRPC route and
/// return its load estimator.
#[track_caller]
fn grpc_route_backend_balancer_load(
    config: &grpc::outbound::OutboundPolicy,
) -> Option<grpc::outbound::backend::balance_p2c::Load> {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");

    // A routed gRPC service is served as a Grpc protocol that holds the gRPC
    // routes directly.
    let routes =
        if let grpc::outbound::proxy_protocol::Kind::Grpc(grpc::outbound::proxy_protocol::Grpc {
            routes,
            ..
        }) = kind
        {
            routes
        } else {
            panic!("proxy protocol must be Grpc; actually got:\n{kind:#?}")
        };

    let backend = routes
        .iter()
        .find_map(grpc_route_backend)
        .expect("a gRPC route must have a balancer backend");

    match backend.kind.as_ref() {
        Some(grpc::outbound::backend::Kind::Balancer(balancer)) => balancer.load,
        _ => None,
    }
}

/// Return the first balancer backend of a non-default (Resource) gRPC route.
fn grpc_route_backend(route: &grpc::outbound::GrpcRoute) -> Option<&grpc::outbound::Backend> {
    let is_default = matches!(
        route.metadata.as_ref().and_then(|m| m.kind.as_ref()),
        Some(grpc::meta::metadata::Kind::Default(_))
    );
    if is_default {
        return None;
    }

    // A gRPC route with real Service backends emits a RandomAvailable
    // distribution of weighted backends.
    route.rules.iter().find_map(|rule| {
        let dist = rule.backends.as_ref()?.kind.as_ref()?;
        let grpc::outbound::grpc_route::distribution::Kind::RandomAvailable(random) = dist else {
            return None;
        };
        random.backends.iter().find_map(|weighted| {
            let backend = weighted.backend.as_ref()?.backend.as_ref()?;
            matches!(
                backend.kind.as_ref(),
                Some(grpc::outbound::backend::Kind::Balancer(_))
            )
            .then_some(backend)
        })
    })
}

#[track_caller]
pub fn assert_route_is_default<R: TestRoute>(
    route: &R::Route,
    parent: &gateway::HTTPRouteParentRefs,
    port: u16,
) {
    let rules = &R::rules_first_available(route);
    let backends = assert_singleton(rules);
    let backend = R::backend(*assert_singleton(backends));
    assert_backend_matches_reference(backend, parent, port);

    let route_meta = R::extract_meta(route);
    match route_meta.kind.as_ref().unwrap() {
        grpc::meta::metadata::Kind::Default(_) => {}
        grpc::meta::metadata::Kind::Resource(r) => {
            panic!("route expected to be default but got resource {r:?}")
        }
    }
}

#[track_caller]
pub fn assert_backend_matches_reference(
    backend: &grpc::outbound::Backend,
    obj_ref: &gateway::HTTPRouteParentRefs,
    port: u16,
) {
    let mut group = obj_ref.group.as_deref();
    if group == Some("") {
        group = Some("core");
    }
    match backend.metadata.as_ref().unwrap().kind.as_ref().unwrap() {
        grpc::meta::metadata::Kind::Resource(resource) => {
            assert_eq!(resource.name, obj_ref.name);
            assert_eq!(Some(&resource.namespace), obj_ref.namespace.as_ref());
            assert_eq!(Some(resource.group.as_str()), group);
            assert_eq!(Some(&resource.kind), obj_ref.kind.as_ref());
            assert_eq!(resource.port, u32::from(port));
        }
        grpc::meta::metadata::Kind::Default(_) => {
            panic!("backend expected to be resource but got default")
        }
    }
}

#[track_caller]
pub fn assert_singleton<T>(ts: &[T]) -> &T {
    assert_eq!(ts.len(), 1);
    ts.first().unwrap()
}
