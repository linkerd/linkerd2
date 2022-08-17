use super::NamespacedTargetRef;

#[derive(
    Clone,
    Debug,
    Default,
    Eq,
    PartialEq,
    kube::CustomResource,
    serde::Deserialize,
    serde::Serialize,
    schemars::JsonSchema,
)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "MeshTLSAuthentication",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct MeshTLSAuthenticationSpec {
    pub identities: Option<Vec<String>>,
    pub identity_refs: Option<Vec<NamespacedTargetRef>>,
}
