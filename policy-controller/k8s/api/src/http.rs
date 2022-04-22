use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use std::collections::BTreeSet;

#[derive(Clone, Debug, kube::CustomResource, Deserialize, Serialize, JsonSchema)]
#[kube(
    group = "http.linkerd.io",
    version = "v1alpha1",
    kind = "RouteGroup",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct RouteGroupSpec {
    pub authorities: Vec<String>,

    pub rules: Vec<RouteRule>,

    pub labels: crate::labels::Map,
}

#[derive(Clone, Debug, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct RouteRule {
    pub r#match: RouteMatch,

    pub labels: crate::labels::Map,
}

#[derive(Clone, Debug, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct RouteMatch {
    pub path: pathscheme::PathScheme,
    pub methods: Option<Vec<MethodMatch>>,
    pub query_params: Option<Vec<MatchExpression>>,
    pub headers: Option<Vec<MatchExpression>>,
}

#[derive(Clone, Debug, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct MethodMatch {
    pub method: String,
}

#[derive(Clone, Debug, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct MatchExpression {
    pub key: String,
    pub operator: MatchOperator,
    pub values: Option<BTreeSet<String>>,
}

#[derive(Clone, Debug, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub enum MatchOperator {
    In,
    NotIn,
    Exists,
    DoesNotExist,
}
