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
        panic!("proxy protocol must be Grpc; actually got:\n{kind:#?}")
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
        panic!("proxy protocol must be Grpc; actually got:\n{kind:#?}")
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
    assert!(
        accrual.is_some(),
        "failure accrual must be configured for service"
    );
    let kind = accrual
        .unwrap().kind
        .as_ref()
        .expect("failure accrual must have kind");
    match kind {
        grpc::outbound::failure_accrual::Kind::ConsecutiveFailures(accrual) => accrual,
        _ => panic!("failure accrual must be consecutive failures; actually got:\n{kind:#?}"),
    }
}

/// Asserts that the load balance config matches.
#[track_caller]
pub fn assert_load_eq(
    config: &grpc::outbound::OutboundPolicy,
    load: Option<grpc::outbound::backend::balance_p2c::Load>,
) {
    detect_http_routes(config, |routes| {
        for backend in routes.iter().flat_map(|route| route.rules.iter())
        .flat_map(|rule| rule.backends.iter()) {
            match backend.kind.as_ref().unwrap() {
                grpc::outbound::http_route::distribution::Kind::RandomAvailable(balance) => {
                    for backend in balance.backends.iter() {
                        match backend.backend.as_ref().unwrap().backend.as_ref().unwrap().kind.as_ref().unwrap() {
                            linkerd2_proxy_api::outbound::backend::Kind::Balancer(balanc) => {
                                assert_eq!(balanc.load, load);
                            }
                            _ => panic!("expected backend to be a balancer, but got:\n{backend:#?}"),
                        }
                    }
                },
                _ => panic!("expected backend to be a RandomAvailable, but got:\n{backend:#?}"),
            }
        }
    });
}

#[track_caller]
pub fn penalty_peak_ewma(
    penalty: Option<Duration>,
    penalty_decay: Option<Duration>,
    max_retry_after: Option<Duration>,
) -> grpc::outbound::backend::balance_p2c::Load {
    grpc::outbound::backend::balance_p2c::Load::PenaltyPeakEwma(
        grpc::outbound::backend::balance_p2c::PenaltyPeakEwma {
            decay: Some(
                    time::Duration::from_secs(10)
                        .try_into()
                        .expect("failed to convert ewma decay to protobuf")
                ),
            default_rtt: Some(
                    time::Duration::from_millis(30)
                        .try_into()
                        .expect("failed to convert ewma default_rtt to protobuf")
                ),
            penalty: penalty.and_then(|duration| duration.try_into().ok()),
            respect_retry_after_hint: max_retry_after.map(|duration| grpc::outbound::backend::balance_p2c::penalty_peak_ewma::RetryAfter {
                max_retry_after: Some(
                    duration.try_into().expect("failed to convert max_retry_after to protobuf")
                ),
            }),
            penalty_decay: penalty_decay.and_then(|duration| duration.try_into().ok()),
            http_status_ranges: vec![
                grpc::outbound::backend::balance_p2c::penalty_peak_ewma::StatusRange {
                    start: 500,
                    end: 599,
                },
                grpc::outbound::backend::balance_p2c::penalty_peak_ewma::StatusRange {
                    start: 429,
                    end: 429,
                },
            ],
            grpc_status_codes: vec![
                8, // ResourceExhausted
                14, // Unavailable
                2, // Unknown
                4, // DeadlineExceeded
                13, // Internal
                15, // DataLoss
            ],

        },
    )
}

#[track_caller]
pub fn peak_ewma() -> grpc::outbound::backend::balance_p2c::Load {
    grpc::outbound::backend::balance_p2c::Load::PeakEwma(grpc::outbound::backend::balance_p2c::PeakEwma {
        default_rtt: Some(
            time::Duration::from_millis(30)
                .try_into()
                .expect("failed to convert ewma default_rtt to protobuf"),
        ),
        decay: Some(
            time::Duration::from_secs(10)
                .try_into()
                .expect("failed to convert ewma decay to protobuf"),
        ),
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
