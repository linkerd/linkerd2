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

    /// A `TLSRoute` pinned to `v1alpha2`.
    ///
    /// This deliberately shadows the `TLSRoute` from the glob import above.
    /// The `gateway-api` crate binds `TLSRoute` to `v1`, which is only served
    /// by Gateway API v1.5 and later. `v1alpha2` is served by every release
    /// from v1.1 through v1.5 (deprecated but still served, and the CRD
    /// declares no conversion strategy, so the versions are the same object
    /// under a different `apiVersion`). Using it keeps us compatible with
    /// clusters that predate v1.5.
    ///
    /// The spec and status types are reused from the `gateway-api` crate, so
    /// this only overrides the group/version/kind the client addresses.
    #[derive(Clone, Debug, Default, PartialEq, serde::Deserialize)]
    #[serde(rename_all = "camelCase")]
    pub struct TLSRoute {
        pub metadata: k8s_openapi::apimachinery::pkg::apis::meta::v1::ObjectMeta,
        pub spec: TlsRouteSpec,
        pub status: Option<TlsRouteStatus>,
    }

    impl kube::Resource for TLSRoute {
        type DynamicType = ();
        type Scope = kube::core::NamespaceResourceScope;

        fn kind(_: &()) -> std::borrow::Cow<'_, str> {
            "TLSRoute".into()
        }

        fn group(_: &()) -> std::borrow::Cow<'_, str> {
            "gateway.networking.k8s.io".into()
        }

        fn version(_: &()) -> std::borrow::Cow<'_, str> {
            "v1alpha2".into()
        }

        fn plural(_: &()) -> std::borrow::Cow<'_, str> {
            "tlsroutes".into()
        }

        fn meta(&self) -> &k8s_openapi::apimachinery::pkg::apis::meta::v1::ObjectMeta {
            &self.metadata
        }

        fn meta_mut(&mut self) -> &mut k8s_openapi::apimachinery::pkg::apis::meta::v1::ObjectMeta {
            &mut self.metadata
        }
    }

    // Mirrors what `kube::CustomResource` derives: `apiVersion` and `kind` are
    // not stored on the struct, so they must be written out explicitly.
    impl serde::Serialize for TLSRoute {
        fn serialize<S: serde::Serializer>(&self, ser: S) -> Result<S::Ok, S::Error> {
            use kube::Resource;
            use serde::ser::SerializeStruct;
            let mut obj =
                ser.serialize_struct("TLSRoute", 4 + usize::from(self.status.is_some()))?;
            obj.serialize_field("apiVersion", &Self::api_version(&()))?;
            obj.serialize_field("kind", &Self::kind(&()))?;
            obj.serialize_field("metadata", &self.metadata)?;
            obj.serialize_field("spec", &self.spec)?;
            if let Some(status) = &self.status {
                obj.serialize_field("status", status)?;
            }
            obj.end()
        }
    }

    #[cfg(test)]
    mod tests {
        use super::*;
        use kube::Resource;

        /// `TLSRoute` must stay on `v1alpha2`; `v1` is not served before
        /// Gateway API v1.5. A regression here fails silently at runtime: the
        /// watch is skipped and TLSRoute policy stops being applied.
        #[test]
        fn tls_route_is_v1alpha2() {
            assert_eq!(
                TLSRoute::api_version(&()),
                "gateway.networking.k8s.io/v1alpha2"
            );
            assert_eq!(
                TLSRoute::url_path(&(), Some("ns")),
                "/apis/gateway.networking.k8s.io/v1alpha2/namespaces/ns/tlsroutes"
            );

            // `apiVersion` and `kind` are not struct fields, so they are only
            // emitted by the hand-written `Serialize` impl.
            let json = serde_json::to_value(TLSRoute::default()).unwrap();
            assert_eq!(json["apiVersion"], "gateway.networking.k8s.io/v1alpha2");
            assert_eq!(json["kind"], "TLSRoute");
        }
    }

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
