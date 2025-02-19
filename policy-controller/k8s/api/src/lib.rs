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
    pub use gateway_api::apis::experimental::grpcroutes::*;
    pub use gateway_api::apis::experimental::httproutes::*;
    pub use gateway_api::apis::experimental::tcproutes::*;
    pub use gateway_api::apis::experimental::tlsroutes::*;

    pub mod http_method {
        use gateway_api::apis::experimental::httproutes::HTTPRouteRulesMatchesMethod;

        pub const GET: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Get;
        pub const POST: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Post;
        pub const PUT: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Put;
        pub const DELETE: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Delete;
        pub const PATCH: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Patch;
        pub const HEAD: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Head;
        pub const OPTIONS: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Options;
        pub const CONNECT: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Connect;
        pub const TRACE: HTTPRouteRulesMatchesMethod = HTTPRouteRulesMatchesMethod::Trace;
    }

    pub mod http_scheme {
        use gateway_api::apis::experimental::httproutes::HTTPRouteRulesFiltersRequestRedirectScheme;

        pub const HTTP: HTTPRouteRulesFiltersRequestRedirectScheme =
            HTTPRouteRulesFiltersRequestRedirectScheme::Http;
        pub const HTTPS: HTTPRouteRulesFiltersRequestRedirectScheme =
            HTTPRouteRulesFiltersRequestRedirectScheme::Https;
    }
}
