use linkerd_policy_controller_k8s_api::{
    self as api,
    policy::{AuthorizationPolicy, AuthorizationPolicySpec, LocalTargetRef, NamespacedTargetRef},
};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: "api".to_string(),
            },
            required_authentication_refs: vec![
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "MeshTLSAuthentication".to_string(),
                    name: "mtls-clients".to_string(),
                    namespace: None,
                },
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "NetworkAuthentication".to_string(),
                    name: "cluster-nets".to_string(),
                    namespace: Some("linkerd".to_string()),
                },
            ],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_targets_namespace() {
    admission::accepts(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: None,
                kind: "Namespace".to_string(),
                name: ns,
            },
            required_authentication_refs: vec![
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "MeshTLSAuthentication".to_string(),
                    name: "mtls-clients".to_string(),
                    namespace: None,
                },
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "NetworkAuthentication".to_string(),
                    name: "cluster-nets".to_string(),
                    namespace: Some("linkerd".to_string()),
                },
            ],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_targets_other_namespace() {
    admission::rejects(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: None,
                kind: "Namespace".to_string(),
                name: "foobar".to_string(),
            },
            required_authentication_refs: vec![
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "MeshTLSAuthentication".to_string(),
                    name: "mtls-clients".to_string(),
                    namespace: None,
                },
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "NetworkAuthentication".to_string(),
                    name: "cluster-nets".to_string(),
                    namespace: Some("linkerd".to_string()),
                },
            ],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_targets_route() {
    admission::accepts(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "HttpRoute".to_string(),
                name: "route-foo".to_string(),
            },
            required_authentication_refs: vec![
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "MeshTLSAuthentication".to_string(),
                    name: "mtls-clients".to_string(),
                    namespace: None,
                },
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "NetworkAuthentication".to_string(),
                    name: "cluster-nets".to_string(),
                    namespace: Some("linkerd".to_string()),
                },
            ],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_with_only_meshtls() {
    admission::accepts(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: "api".to_string(),
            },
            required_authentication_refs: vec![NamespacedTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "MeshTLSAuthentication".to_string(),
                name: "mtls-clients".to_string(),
                namespace: None,
            }],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_with_only_network() {
    admission::accepts(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: "api".to_string(),
            },
            required_authentication_refs: vec![NamespacedTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "NetworkAuthentication".to_string(),
                name: "cluster-nets".to_string(),
                namespace: Some("linkerd".to_string()),
            }],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_empty_required_authentications() {
    admission::accepts(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: "deny".to_string(),
            },
            required_authentication_refs: vec![],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_missing_required_authentications() {
    #[derive(
        Clone,
        Debug,
        kube::CustomResource,
        serde::Deserialize,
        serde::Serialize,
        schemars::JsonSchema,
    )]
    #[kube(
        group = "policy.linkerd.io",
        version = "v1alpha1",
        kind = "AuthorizationPolicy",
        namespaced
    )]
    #[serde(rename_all = "camelCase")]
    pub struct FakeAuthorizationPolicySpec {
        pub target_ref: LocalTargetRef,
        pub required_authentication_refs: Option<Vec<NamespacedTargetRef>>,
    }

    admission::rejects(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: FakeAuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: "deny".to_string(),
            },
            required_authentication_refs: None,
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_target_ref_deployment() {
    admission::rejects(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("apps".to_string()),
                kind: "Deployment".to_string(),
                name: "someapp".to_string(),
            },
            required_authentication_refs: vec![NamespacedTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "NetworkAuthentication".to_string(),
                namespace: Some("linkerd".to_string()),
                name: "cluster-nets".to_string(),
            }],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_duplicate_mtls_authns() {
    admission::rejects(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: "some-srv".to_string(),
            },
            required_authentication_refs: vec![
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "MeshTLSAuthentication".to_string(),
                    namespace: Some("some-ns".to_string()),
                    name: "some-ids".to_string(),
                },
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "MeshTLSAuthentication".to_string(),
                    namespace: Some("other-ns".to_string()),
                    name: "other-ids".to_string(),
                },
            ],
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_duplicate_network_authns() {
    admission::rejects(|ns| AuthorizationPolicy {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: "some-srv".to_string(),
            },
            required_authentication_refs: vec![
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "NetworkAuthentication".to_string(),
                    namespace: Some("some-ns".to_string()),
                    name: "some-nets".to_string(),
                },
                NamespacedTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "NetworkAuthentication".to_string(),
                    namespace: Some("other-ns".to_string()),
                    name: "other-nets".to_string(),
                },
            ],
        },
    })
    .await;
}
