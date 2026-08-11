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
    core::Status,
    runtime::watcher::Event as WatchEvent,
    Client, Error,
};

pub mod gateway {
    pub use gateway_api::apis::experimental::grpcroutes::*;
    pub use gateway_api::apis::experimental::httproutes::*;
    pub use gateway_api::apis::experimental::tcproutes::*;
    pub use gateway_api::apis::experimental::tlsroutes::*;

    pub mod http_method {
        use gateway_api::apis::experimental::httproutes::HttpRouteRulesMatchesMethod;

        pub const GET: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Get;
        pub const POST: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Post;
        pub const PUT: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Put;
        pub const DELETE: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Delete;
        pub const PATCH: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Patch;
        pub const HEAD: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Head;
        pub const OPTIONS: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Options;
        pub const CONNECT: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Connect;
        pub const TRACE: HttpRouteRulesMatchesMethod = HttpRouteRulesMatchesMethod::Trace;
    }

    pub mod http_scheme {
        use gateway_api::apis::experimental::httproutes::HttpRouteRulesFiltersRequestRedirectScheme;

        pub const HTTP: HttpRouteRulesFiltersRequestRedirectScheme =
            HttpRouteRulesFiltersRequestRedirectScheme::Http;
        pub const HTTPS: HttpRouteRulesFiltersRequestRedirectScheme =
            HttpRouteRulesFiltersRequestRedirectScheme::Https;
    }
}
