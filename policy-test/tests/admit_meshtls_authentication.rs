use linkerd_policy_controller_k8s_api::{
    self as api,
    policy::{MeshTLSAuthentication, MeshTLSAuthenticationSpec, TargetRef},
};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_ref() {
    admission::accepts(|ns| MeshTLSAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: MeshTLSAuthenticationSpec {
            identity_refs: Some(vec![TargetRef {
                kind: "ServiceAccount".to_string(),
                name: Some("default".to_string()),
                ..Default::default()
            }]),
            ..Default::default()
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_strings() {
    admission::accepts(|ns| MeshTLSAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: MeshTLSAuthenticationSpec {
            identities: Some(vec!["example.id".to_string()]),
            ..Default::default()
        },
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_empty() {
    admission::rejects(|ns| MeshTLSAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: MeshTLSAuthenticationSpec::default(),
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_both_refs_and_strings() {
    admission::rejects(|ns| MeshTLSAuthentication {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: MeshTLSAuthenticationSpec {
            identities: Some(vec!["example.id".to_string()]),
            identity_refs: Some(vec![TargetRef {
                kind: "ServiceAccount".to_string(),
                name: Some("default".to_string()),
                ..Default::default()
            }]),
        },
    })
    .await;
}
