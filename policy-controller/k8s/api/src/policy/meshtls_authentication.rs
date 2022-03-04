use super::TargetRef;

#[derive(
    Clone,
    Debug,
    Default,
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
pub struct MeshTlsAuthenticationSpec {
    pub identities: Vec<String>,
    pub identity_refs: Vec<TargetRef>,
}
