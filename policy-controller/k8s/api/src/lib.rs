#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod duration;
pub mod external_workload;
pub mod labels;
pub mod policy;

pub use self::labels::Labels;
pub use k8s_openapi::{
    api::{
        self,
        coordination::v1::Lease,
        core::v1::{
            Container, ContainerPort, Endpoints, HTTPGetAction, Namespace, Node, NodeSpec, Pod,
            PodSpec, PodStatus, Probe, Service, ServiceAccount, ServicePort, ServiceSpec,
        },
    },
    apimachinery::{
        self,
        pkg::{
            apis::meta::v1::{Condition, Time},
            util::intstr::IntOrString,
        },
    },
    NamespaceResourceScope,
};
pub use kube::{
    api::{Api, ListParams, ObjectMeta, Patch, PatchParams, Resource, ResourceExt},
    error::ErrorResponse,
    runtime::watcher::Event as WatchEvent,
    Client, Error,
};

pub mod gateway {
    pub use k8s_gateway_api::*;

    pub type HTTPRoute = HttpRoute;
    pub type HTTPRouteSpec = HttpRouteSpec;
    pub type HTTPRouteParentRefs = ParentReference;
    pub type HTTPRouteRules = HttpRouteRule;
    pub type HTTPRouteRulesMatches = HttpRouteMatch;
    pub type HTTPRouteRulesFilters = HttpRouteFilter;
    pub type HTTPRouteRulesBackendRefs = HttpBackendRef;
    pub type HTTPRouteRulesBackendRefsFilters = HttpRouteFilter;
    pub type HTTPRouteStatus = HttpRouteStatus;
    pub type HTTPRouteStatusParents = RouteParentStatus;
    pub type HTTPRouteStatusParentsParentRef = ParentReference;
    pub type HTTPRouteRulesFiltersRequestHeaderModifier = HttpRequestHeaderFilter;
    pub type HTTPRouteRulesFiltersResponseHeaderModifier = HttpRequestHeaderFilter;
    pub type HTTPRouteRulesBackendRefsFiltersRequestHeaderModifier = HttpRequestHeaderFilter;
    pub type HTTPRouteRulesBackendRefsFiltersResponseHeaderModifier = HttpRequestHeaderFilter;
    pub type HTTPRouteRulesFiltersRequestHeaderModifierAdd = HttpHeader;
    pub type HTTPRouteRulesFiltersRequestHeaderModifierSet = HttpHeader;
    pub type HTTPRouteRulesFiltersResponseHeaderModifierAdd = HttpHeader;
    pub type HTTPRouteRulesFiltersResponseHeaderModifierSet = HttpHeader;
    pub type HTTPRouteRulesBackendRefsFiltersRequestHeaderModifierAdd = HttpHeader;
    pub type HTTPRouteRulesBackendRefsFiltersRequestHeaderModifierSet = HttpHeader;
    pub type HTTPRouteRulesBackendRefsFiltersResponseHeaderModifierAdd = HttpHeader;
    pub type HTTPRouteRulesBackendRefsFiltersResponseHeaderModifierSet = HttpHeader;
    pub type HTTPRouteRulesFiltersRequestRedirect = HttpRequestRedirectFilter;
    pub type HTTPRouteRulesBackendRefsFiltersRequestRedirect = HttpRequestRedirectFilter;
    pub type HTTPRouteRulesFiltersRequestRedirectPath = HttpPathModifier;
    pub type HTTPRouteRulesBackendRefsFiltersRequestRedirectPath = HttpPathModifier;

    pub mod http_method {
        pub const GET: &str = "GET";
        pub const POST: &str = "POST";
        pub const PUT: &str = "PUT";
        pub const DELETE: &str = "DELETE";
        pub const PATCH: &str = "PATCH";
        pub const HEAD: &str = "HEAD";
        pub const OPTIONS: &str = "OPTIONS";
        pub const CONNECT: &str = "CONNECT";
        pub const TRACE: &str = "TRACE";
    }

    pub mod http_scheme {
        pub const HTTP: &str = "http";
        pub const HTTPS: &str = "https";
    }

    pub type GRPCRoute = GrpcRoute;
    pub type GRPCRouteSpec = GrpcRouteSpec;
    pub type GRPCRouteParentRefs = ParentReference;
    pub type GRPCRouteRules = GrpcRouteRule;
    pub type GRPCRouteRulesMatches = GrpcRouteMatch;
    pub type GRPCRouteRulesFilters = GrpcRouteFilter;
    pub type GRPCRouteRulesBackendRefs = GrpcRouteBackendRef;
    pub type GRPCRouteRulesBackendRefsFilters = GrpcRouteFilter;
    pub type GRPCRouteStatus = GrpcRouteStatus;
    pub type GRPCRouteStatusParents = RouteParentStatus;
    pub type GRPCRouteStatusParentsParentRef = ParentReference;
    pub type GRPCRouteRulesFiltersRequestHeaderModifier = HttpRequestHeaderFilter;
    pub type GRPCRouteRulesFiltersResponseHeaderModifier = HttpRequestHeaderFilter;
    pub type GRPCRouteRulesBackendRefsFiltersRequestHeaderModifier = HttpRequestHeaderFilter;
    pub type GRPCRouteRulesBackendRefsFiltersResponseHeaderModifier = HttpRequestHeaderFilter;
    pub type GRPCRouteRulesFiltersRequestHeaderModifierAdd = HttpHeader;
    pub type GRPCRouteRulesFiltersRequestHeaderModifierSet = HttpHeader;
    pub type GRPCRouteRulesFiltersResponseHeaderModifierAdd = HttpHeader;
    pub type GRPCRouteRulesFiltersResponseHeaderModifierSet = HttpHeader;
    pub type GRPCRouteRulesBackendRefsFiltersRequestHeaderModifierAdd = HttpHeader;
    pub type GRPCRouteRulesBackendRefsFiltersRequestHeaderModifierSet = HttpHeader;
    pub type GRPCRouteRulesBackendRefsFiltersResponseHeaderModifierAdd = HttpHeader;
    pub type GRPCRouteRulesBackendRefsFiltersResponseHeaderModifierSet = HttpHeader;

    pub type TLSRoute = TlsRoute;
    pub type TLSRouteSpec = TlsRouteSpec;
    pub type TLSRouteParentRefs = ParentReference;
    pub type TLSRouteRules = TlsRouteRule;
    pub type TLSRouteRulesBackendRefs = BackendRef;
    pub type TLSRouteStatus = TlsRouteStatus;
    pub type TLSRouteStatusParents = RouteParentStatus;
    pub type TLSRouteStatusParentsParentRef = ParentReference;

    pub type TCPRoute = TcpRoute;
    pub type TCPRouteSpec = TcpRouteSpec;
    pub type TCPRouteParentRefs = ParentReference;
    pub type TCPRouteRules = TcpRouteRule;
    pub type TCPRouteRulesBackendRefs = BackendRef;
    pub type TCPRouteStatus = TcpRouteStatus;
    pub type TCPRouteStatusParents = RouteParentStatus;
    pub type TCPRouteStatusParentsParentRef = ParentReference;
}
