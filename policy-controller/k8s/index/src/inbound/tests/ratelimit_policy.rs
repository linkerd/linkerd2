use super::*;
use linkerd_policy_controller_core::inbound::{Limit, Override, RateLimit};

#[test]
fn ratelimit_policy_with_server() {
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

    let ratelimit = RateLimit {
        name: "ratelimit-0".to_string(),
        total: Some(Limit {
            requests_per_second: 1000,
        }),
        identity: None,
        overrides: vec![Override {
            requests_per_second: 500,
            client_identities: vec![
                "client-0.ns-0.serviceaccount.identity.linkerd.cluster.example.com".to_string(),
            ],
        }],
    };
    test.index.write().apply(mk_ratelimit(
        "ns-0",
        "ratelimit-0",
        Some(k8s::policy::Limit {
            requests_per_second: 1000,
        }),
        vec![k8s::policy::Override {
            requests_per_second: 500,
            client_refs: vec![NamespacedTargetRef {
                group: None,
                kind: "ServiceAccount".to_string(),
                name: "client-0".to_string(),
                namespace: None,
            }],
        }],
        "srv-8080",
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: Default::default(),
            ratelimit: Some(ratelimit),
            concurrency_limit: None,
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_http_routes(),
            grpc_routes: mk_default_grpc_routes(),
        },
    );
}

fn mk_ratelimit(
    ns: impl ToString,
    name: impl ToString,
    total: Option<k8s::policy::Limit>,
    overrides: Vec<k8s::policy::Override>,
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
            overrides: Some(overrides),
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
