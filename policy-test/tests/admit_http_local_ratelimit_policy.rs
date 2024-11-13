use k8s_openapi::chrono;
use linkerd_policy_controller_k8s_api::{
    self as api,
    policy::{
        HTTPLocalRateLimitPolicy, HTTPLocalRateLimitPolicyStatus, Limit, LocalTargetRef,
        NamespacedTargetRef, Override, RateLimitPolicySpec,
    },
};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| {
        mk_ratelimiter(ns, default_target_ref(), 1000, 100, default_overrides())
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_target_ref_deployment() {
    let target_ref = LocalTargetRef {
        group: Some("apps".to_string()),
        kind: "Deployment".to_string(),
        name: "api".to_string(),
    };
    admission::rejects(|ns| mk_ratelimiter(ns, target_ref, 1000, 100, default_overrides())).await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_identity_rps_higher_than_total() {
    admission::rejects(|ns| {
        mk_ratelimiter(ns, default_target_ref(), 1000, 2000, default_overrides())
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_overrides_rps_higher_than_total() {
    let overrides = vec![Override {
        requests_per_second: 2000,
        client_refs: vec![NamespacedTargetRef {
            group: Some("".to_string()),
            kind: "ServiceAccount".to_string(),
            name: "sa-1".to_string(),
            namespace: Some("linkerd".to_string()),
        }],
    }];
    admission::rejects(|ns| mk_ratelimiter(ns, default_target_ref(), 1000, 2000, overrides)).await;
}

fn default_target_ref() -> LocalTargetRef {
    LocalTargetRef {
        group: Some("policy.linkerd.io".to_string()),
        kind: "Server".to_string(),
        name: "api".to_string(),
    }
}

fn default_overrides() -> Vec<Override> {
    vec![Override {
        requests_per_second: 200,
        client_refs: vec![NamespacedTargetRef {
            group: Some("".to_string()),
            kind: "ServiceAccount".to_string(),
            name: "sa-1".to_string(),
            namespace: Some("linkerd".to_string()),
        }],
    }]
}

fn mk_ratelimiter(
    namespace: String,
    target_ref: LocalTargetRef,
    total_rps: u32,
    identity_rps: u32,
    overrides: Vec<Override>,
) -> HTTPLocalRateLimitPolicy {
    HTTPLocalRateLimitPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(namespace),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: RateLimitPolicySpec {
            target_ref,
            total: Some(Limit {
                requests_per_second: total_rps,
            }),
            identity: Some(Limit {
                requests_per_second: identity_rps,
            }),
            overrides: Some(overrides),
        },
        status: Some(HTTPLocalRateLimitPolicyStatus {
            conditions: vec![api::Condition {
                last_transition_time: api::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
                message: "".to_string(),
                observed_generation: None,
                reason: "".to_string(),
                status: "True".to_string(),
                type_: "Accepted".to_string(),
            }],
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: "linkerd-admin".to_string(),
            },
        }),
    }
}
