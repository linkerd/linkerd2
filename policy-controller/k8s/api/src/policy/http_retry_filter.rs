/// `HttpRetryFilter` defines a retry policy for an HTTPRoute rule.
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
    kind = "HTTPRetryFilter",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct HttpRetryFilter {
    /// The maximum number of retries allowed per request. If this
    /// is zero or not present, no per-request limit is enforced.
    pub max_retries_per_request: Option<u32>,

    /// A list of HTTP status codes which will be retried. Status
    /// codes may be individual statuses (e.g. "500"), or ranges
    /// delimited by a hyphen (e.g. "500-503"). If this list is
    /// empty or not present, all 5xx status codes will be retried.
    pub retry_statuses: Option<Vec<String>>,
}
