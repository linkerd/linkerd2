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
    kind = "NetworkAuthentication",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct NetworkAuthenticationSpec {
    pub networks: Vec<Network>,
}

#[derive(Clone, Debug, Default, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Network {
    pub cidr: String,
    pub except: Option<Vec<String>>,
}
