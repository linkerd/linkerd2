use crate::{grpc, test_route::TestRoute};
use k8s_gateway_api::ParentReference;
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
        .unwrap()
        .kind
        .as_ref()
        .expect("failure accrual must have kind");
    let grpc::outbound::failure_accrual::Kind::ConsecutiveFailures(accrual) = kind;
    accrual
}

#[track_caller]
pub fn assert_route_is_default<R: TestRoute>(
    route: &R::Route,
    parent: &ParentReference,
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
    obj_ref: &ParentReference,
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
