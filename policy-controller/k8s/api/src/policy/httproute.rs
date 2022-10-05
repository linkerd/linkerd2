pub use k8s_gateway_api::{
    CommonRouteSpec, Hostname, HttpHeader, HttpHeaderMatch, HttpHeaderName, HttpMethod,
    HttpPathMatch, HttpPathModifier, HttpQueryParamMatch, HttpRequestHeaderFilter,
    HttpRequestRedirectFilter, HttpRouteMatch, LocalObjectReference, ParentReference, RouteStatus,
};

/// HTTPRoute provides a way to route HTTP requests. This includes the
/// capability to match requests by hostname, path, header, or query param.
/// Filters can be used to specify additional processing steps. Backends specify
/// where matching requests should be routed.
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
    kind = "HTTPRoute",
    struct = "HttpRoute",
    status = "HttpRouteStatus",
    namespaced
)]
pub struct HttpRouteSpec {
    /// Common route information.
    #[serde(flatten)]
    pub inner: CommonRouteSpec,

    /// Hostnames defines a set of hostname that should match against the HTTP
    /// Host header to select a HTTPRoute to process the request. This matches
    /// the RFC 1123 definition of a hostname with 2 notable exceptions:
    ///
    /// 1. IPs are not allowed.
    /// 2. A hostname may be prefixed with a wildcard label (`*.`). The wildcard
    ///    label must appear by itself as the first label.
    pub hostnames: Option<Vec<Hostname>>,

    /// Rules are a list of HTTP matchers, filters and actions.
    pub rules: Option<Vec<HttpRouteRule>>,
}

/// HTTPRouteRule defines semantics for matching an HTTP request based on
/// conditions (matches), processing it (filters), and forwarding the request to
/// an API object (backendRefs).
#[derive(
    Clone, Debug, PartialEq, Eq, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
#[serde(rename_all = "camelCase")]
pub struct HttpRouteRule {
    /// Matches define conditions used for matching the rule against incoming
    /// HTTP requests. Each match is independent, i.e. this rule will be matched
    /// if **any** one of the matches is satisfied.
    ///
    /// For example, take the following matches configuration:
    ///
    /// ```yaml
    /// matches:
    /// - path:
    ///     value: "/foo"
    ///   headers:
    ///   - name: "version"
    ///     value: "v2"
    /// - path:
    ///     value: "/v2/foo"
    /// ```
    ///
    /// For a request to match against this rule, a request must satisfy
    /// EITHER of the two conditions:
    ///
    /// - path prefixed with `/foo` AND contains the header `version: v2`
    /// - path prefix of `/v2/foo`
    ///
    /// See the documentation for HTTPRouteMatch on how to specify multiple
    /// match conditions that should be ANDed together.
    ///
    /// If no matches are specified, the default is a prefix
    /// path match on "/", which has the effect of matching every
    /// HTTP request.
    ///
    /// Proxy or Load Balancer routing configuration generated from HTTPRoutes
    /// MUST prioritize rules based on the following criteria, continuing on
    /// ties. Precedence must be given to the the Rule with the largest number
    /// of:
    ///
    /// * Characters in a matching non-wildcard hostname.
    /// * Characters in a matching hostname.
    /// * Characters in a matching path.
    /// * Header matches.
    /// * Query param matches.
    ///
    /// If ties still exist across multiple Routes, matching precedence MUST be
    /// determined in order of the following criteria, continuing on ties:
    ///
    /// * The oldest Route based on creation timestamp.
    /// * The Route appearing first in alphabetical order by
    ///   "{namespace}/{name}".
    ///
    /// If ties still exist within the Route that has been given precedence,
    /// matching precedence MUST be granted to the first matching rule meeting
    /// the above criteria.
    ///
    /// When no rules matching a request have been successfully attached to the
    /// parent a request is coming from, a HTTP 404 status code MUST be returned.
    pub matches: Option<Vec<HttpRouteMatch>>,

    /// Filters define the filters that are applied to requests that match this
    /// rule.
    ///
    /// The effects of ordering of multiple behaviors are currently unspecified.
    /// This can change in the future based on feedback during the alpha stage.
    ///
    /// Conformance-levels at this level are defined based on the type of
    /// filter:
    ///
    /// - ALL core filters MUST be supported by all implementations.
    /// - Implementers are encouraged to support extended filters.
    /// - Implementation-specific custom filters have no API guarantees across
    ///   implementations.
    ///
    /// Specifying a core filter multiple times has unspecified or custom
    /// conformance.
    ///
    /// Support: Core
    pub filters: Option<Vec<HttpRouteFilter>>,
}

/// HTTPRouteFilter defines processing steps that must be completed during the
/// request or response lifecycle. HTTPRouteFilters are meant as an extension
/// point to express processing that may be done in Gateway implementations.
/// Some examples include request or response modification, implementing
/// authentication strategies, rate-limiting, and traffic shaping. API
/// guarantee/conformance is defined based on the type of the filter.
#[derive(
    Clone, Debug, PartialEq, Eq, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
#[serde(tag = "type", rename_all = "PascalCase")]
pub enum HttpRouteFilter {
    /// RequestHeaderModifier defines a schema for a filter that modifies request
    /// headers.
    ///
    /// Support: Core
    #[serde(rename_all = "camelCase")]
    RequestHeaderModifier {
        request_header_modifier: HttpRequestHeaderFilter,
    },

    /// RequestRedirect defines a schema for a filter that responds to the
    /// request with an HTTP redirection.
    ///
    /// Support: Core
    #[serde(rename_all = "camelCase")]
    RequestRedirect {
        request_redirect: HttpRequestRedirectFilter,
    },
}

/// HTTPRouteStatus defines the observed state of HTTPRoute.
#[derive(Clone, Debug, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
pub struct HttpRouteStatus {
    /// Common route status information.
    #[serde(flatten)]
    pub inner: RouteStatus,
}

pub fn parent_ref_targets_kind<T>(parent_ref: &ParentReference) -> bool
where
    T: kube::Resource,
    T::DynamicType: Default,
{
    let kind = match parent_ref.kind {
        Some(ref kind) => kind,
        None => return false,
    };

    super::targets_kind::<T>(parent_ref.group.as_deref(), kind)
}
