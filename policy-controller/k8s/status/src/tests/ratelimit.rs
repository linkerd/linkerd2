use crate::{
    index::{accepted, no_matching_target, ratelimit_already_exists, SharedIndex, Update},
    resource_id::NamespaceGroupKindName,
    tests::{default_cluster_networks, make_server},
    Index, IndexMetrics,
};
use chrono::{DateTime, Utc};
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::routes::GroupKindName;
use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api,
    policy::{self as linkerd_k8s_api},
    Resource,
};
use std::sync::Arc;
use tokio::sync::{
    mpsc::{self, Receiver},
    watch,
};

#[test]
fn ratelimit_accepted() {
    let (index, mut updates_rx) = make_index_updates_rx();

    // create server
    let server = make_server(
        "ns",
        "server-1",
        8080,
        vec![("app", "server")],
        vec![],
        None,
    );
    index.write().apply(server);

    // create an associated rate limit
    let (ratelimit_id, ratelimit) = make_ratelimit("rl-1".to_string(), "server-1".to_string());
    index.write().apply(ratelimit);

    let expected_status = linkerd_k8s_api::HTTPLocalRateLimitPolicyStatus {
        conditions: vec![accepted()],
        target_ref: linkerd_k8s_api::LocalTargetRef {
            group: Some("policy.linkerd.io".to_string()),
            kind: "Server".to_string(),
            name: "server-1".to_string(),
        },
    };

    let expected_patch = crate::index::make_patch(&ratelimit_id, expected_status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(ratelimit_id, update.id);
    assert_eq!(expected_patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn ratelimit_not_accepted_no_matching_target() {
    let (index, mut updates_rx) = make_index_updates_rx();

    // create server
    let server = make_server(
        "ns",
        "server-1",
        8080,
        vec![("app", "server")],
        vec![],
        None,
    );
    index.write().apply(server);

    // create an associated rate limit
    let (ratelimit_id, ratelimit) = make_ratelimit("rl-1".to_string(), "server-2".to_string());
    index.write().apply(ratelimit);

    let expected_status = linkerd_k8s_api::HTTPLocalRateLimitPolicyStatus {
        conditions: vec![no_matching_target()],
        target_ref: linkerd_k8s_api::LocalTargetRef {
            group: Some("policy.linkerd.io".to_string()),
            kind: "Server".to_string(),
            name: "server-2".to_string(),
        },
    };

    let expected_patch = crate::index::make_patch(&ratelimit_id, expected_status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(ratelimit_id, update.id);
    assert_eq!(expected_patch, update.patch);
    assert!(updates_rx.try_recv().is_err())
}

#[test]
fn ratelimit_not_accepted_already_exists() {
    let (index, mut updates_rx) = make_index_updates_rx();

    // create server
    let server = make_server(
        "ns",
        "server-1",
        8080,
        vec![("app", "server")],
        vec![],
        None,
    );
    index.write().apply(server);

    // create an associated rate limit
    let (rl_1_id, rl_1) = make_ratelimit("rl-1".to_string(), "server-1".to_string());
    index.write().apply(rl_1);

    let expected_status = linkerd_k8s_api::HTTPLocalRateLimitPolicyStatus {
        conditions: vec![accepted()],
        target_ref: linkerd_k8s_api::LocalTargetRef {
            group: Some("policy.linkerd.io".to_string()),
            kind: "Server".to_string(),
            name: "server-1".to_string(),
        },
    };

    let rl_1_expected_patch = crate::index::make_patch(&rl_1_id, expected_status).unwrap();

    let update = updates_rx.try_recv().unwrap();
    assert_eq!(rl_1_id, update.id);
    assert_eq!(rl_1_expected_patch, update.patch);
    assert!(updates_rx.try_recv().is_err());

    // create another rate limit for the same server
    let (rl_2_id, rl_2) = make_ratelimit("rl-2".to_string(), "server-1".to_string());
    index.write().apply(rl_2);

    let expected_status = linkerd_k8s_api::HTTPLocalRateLimitPolicyStatus {
        conditions: vec![ratelimit_already_exists()],
        target_ref: linkerd_k8s_api::LocalTargetRef {
            group: Some("policy.linkerd.io".to_string()),
            kind: "Server".to_string(),
            name: "server-1".to_string(),
        },
    };

    let rl_2_expected_patch = crate::index::make_patch(&rl_2_id, expected_status).unwrap();

    let update_1 = updates_rx.try_recv().unwrap();
    let update_2 = updates_rx.try_recv().unwrap();
    assert!(updates_rx.try_recv().is_err());

    // we should receive updates for both rate limits in any order
    if update_1.id == rl_1_id {
        assert_eq!(rl_1_id, update_1.id);
        assert_eq!(rl_1_expected_patch, update_1.patch);
        assert_eq!(rl_2_id, update_2.id);
        assert_eq!(rl_2_expected_patch, update_2.patch);
    } else {
        assert_eq!(rl_1_id, update_2.id);
        assert_eq!(rl_1_expected_patch, update_2.patch);
        assert_eq!(rl_2_id, update_1.id);
        assert_eq!(rl_2_expected_patch, update_1.patch);
    }
}

fn make_index_updates_rx() -> (SharedIndex, Receiver<Update>) {
    let hostname = "test";
    let claim = kubert::lease::Claim {
        holder: "test".to_string(),
        expiry: DateTime::<Utc>::MAX_UTC,
    };
    let (_claims_tx, claims_rx) = watch::channel(Arc::new(claim));
    let (updates_tx, updates_rx) = mpsc::channel(10000);
    let index = Index::shared(
        hostname,
        claims_rx,
        updates_tx,
        IndexMetrics::register(&mut Default::default()),
        default_cluster_networks(),
    );

    (index, updates_rx)
}

fn make_ratelimit(
    name: String,
    server: String,
) -> (
    NamespaceGroupKindName,
    linkerd_k8s_api::HTTPLocalRateLimitPolicy,
) {
    let ratelimit_id = NamespaceGroupKindName {
        namespace: "ns".to_string(),
        gkn: GroupKindName {
            group: linkerd_k8s_api::HTTPLocalRateLimitPolicy::group(&()),
            kind: linkerd_k8s_api::HTTPLocalRateLimitPolicy::kind(&()),
            name: name.clone().into(),
        },
    };

    let ratelimit = linkerd_k8s_api::HTTPLocalRateLimitPolicy {
        metadata: k8s_core_api::ObjectMeta {
            name: Some(name),
            namespace: Some("ns".to_string()),
            ..Default::default()
        },
        spec: linkerd_k8s_api::RateLimitPolicySpec {
            target_ref: linkerd_k8s_api::LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: server,
            },
            total: Some(linkerd_k8s_api::Limit {
                requests_per_second: 1,
            }),
            identity: None,
            overrides: None,
        },
        status: None,
    };

    (ratelimit_id, ratelimit)
}
