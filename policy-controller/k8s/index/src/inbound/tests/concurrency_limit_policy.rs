use super::*;
use linkerd_policy_controller_core::inbound::ConcurrencyLimit;

#[test]
fn concurrency_limit_policy_with_server() {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: Default::default(),
            ratelimit: None,
            concurrency_limit: None,
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_http_routes(),
            grpc_routes: mk_default_grpc_routes(),
        },
    );

    let concurrency_limit = ConcurrencyLimit {
        name: "concurrency-limit-0".to_string(),
        max_in_flight_requests: 100,
    };
    test.index.write().apply(mk_concurrency_limit(
        "ns-0",
        "concurrency-limit-0",
        100,
        "srv-8080",
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: Default::default(),
            ratelimit: None,
            concurrency_limit: Some(concurrency_limit),
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_http_routes(),
            grpc_routes: mk_default_grpc_routes(),
        },
    );
}

#[test]
fn concurrency_limit_policy_update() {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    rx.borrow_and_update();

    // Apply initial concurrency limit
    test.index.write().apply(mk_concurrency_limit(
        "ns-0",
        "concurrency-limit-0",
        100,
        "srv-8080",
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        rx.borrow_and_update().concurrency_limit,
        Some(ConcurrencyLimit {
            name: "concurrency-limit-0".to_string(),
            max_in_flight_requests: 100,
        })
    );

    // Update concurrency limit to a new value
    test.index.write().apply(mk_concurrency_limit(
        "ns-0",
        "concurrency-limit-0",
        200,
        "srv-8080",
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        rx.borrow_and_update().concurrency_limit,
        Some(ConcurrencyLimit {
            name: "concurrency-limit-0".to_string(),
            max_in_flight_requests: 200,
        })
    );
}

#[test]
fn concurrency_limit_policy_delete() {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    rx.borrow_and_update();

    // Apply concurrency limit
    test.index.write().apply(mk_concurrency_limit(
        "ns-0",
        "concurrency-limit-0",
        100,
        "srv-8080",
    ));
    assert!(rx.has_changed().unwrap());
    assert!(rx.borrow_and_update().concurrency_limit.is_some());

    // Delete concurrency limit
    <crate::inbound::Index as kubert::index::IndexNamespacedResource<
        k8s::policy::HttpLocalConcurrencyLimitPolicy,
    >>::delete(
        &mut test.index.write(),
        "ns-0".to_string(),
        "concurrency-limit-0".to_string(),
    );
    assert!(rx.has_changed().unwrap());
    assert!(rx.borrow_and_update().concurrency_limit.is_none());
}

#[test]
fn concurrency_limit_policy_with_ratelimit() {
    use linkerd_policy_controller_core::inbound::{Limit, RateLimit};

    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    rx.borrow_and_update();

    // Apply both rate limit and concurrency limit
    test.index.write().apply(mk_ratelimit(
        "ns-0",
        "ratelimit-0",
        Some(k8s::policy::Limit {
            requests_per_second: 1000,
        }),
        "srv-8080",
    ));
    assert!(rx.has_changed().unwrap());
    rx.borrow_and_update();

    test.index.write().apply(mk_concurrency_limit(
        "ns-0",
        "concurrency-limit-0",
        100,
        "srv-8080",
    ));
    assert!(rx.has_changed().unwrap());

    let server = rx.borrow_and_update();
    assert_eq!(
        server.ratelimit,
        Some(RateLimit {
            name: "ratelimit-0".to_string(),
            total: Some(Limit {
                requests_per_second: 1000,
            }),
            identity: None,
            overrides: vec![],
        })
    );
    assert_eq!(
        server.concurrency_limit,
        Some(ConcurrencyLimit {
            name: "concurrency-limit-0".to_string(),
            max_in_flight_requests: 100,
        })
    );
}

fn mk_concurrency_limit(
    ns: impl ToString,
    name: impl ToString,
    max_in_flight_requests: u32,
    server_name: impl ToString,
) -> k8s::policy::HttpLocalConcurrencyLimitPolicy {
    k8s::policy::HttpLocalConcurrencyLimitPolicy {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::ConcurrencyLimitPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: server_name.to_string(),
            },
            max_in_flight_requests,
        },
        status: Some(k8s::policy::HttpLocalConcurrencyLimitPolicyStatus {
            conditions: vec![k8s::Condition {
                last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
                message: "".to_string(),
                observed_generation: None,
                reason: "".to_string(),
                status: "True".to_string(),
                type_: "Accepted".to_string(),
            }],
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: server_name.to_string(),
            },
        }),
    }
}

fn mk_ratelimit(
    ns: impl ToString,
    name: impl ToString,
    total: Option<k8s::policy::Limit>,
    server_name: impl ToString,
) -> k8s::policy::HttpLocalRateLimitPolicy {
    k8s::policy::HttpLocalRateLimitPolicy {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::RateLimitPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: server_name.to_string(),
            },
            total,
            identity: None,
            overrides: None,
        },
        status: Some(k8s::policy::HttpLocalRateLimitPolicyStatus {
            conditions: vec![k8s::Condition {
                last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
                message: "".to_string(),
                observed_generation: None,
                reason: "".to_string(),
                status: "True".to_string(),
                type_: "Accepted".to_string(),
            }],
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: server_name.to_string(),
            },
        }),
    }
}
