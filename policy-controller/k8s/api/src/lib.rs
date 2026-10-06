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

    /// The `TLSRoute` API versions the policy controller knows how to watch,
    /// in order of preference.
    ///
    /// No single version is served by every supported Gateway API bundle:
    /// v1.2--v1.4 serve `v1alpha2` only (and only in the experimental
    /// channel), while v1.5 serves `v1` in both channels and stops serving
    /// `v1alpha2` in the standard channel. The version to use is therefore
    /// negotiated with the API server at startup.
    ///
    /// The CRDs declare no conversion strategy, so all served versions are the
    /// same stored object under a different `apiVersion`.
    #[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
    pub enum TlsRouteApiVersion {
        #[default]
        V1,
        V1Alpha2,
    }

    impl TlsRouteApiVersion {
        /// All supported versions, most preferred first.
        pub const ALL: &'static [Self] = &[Self::V1, Self::V1Alpha2];

        pub fn version(&self) -> &'static str {
            match self {
                Self::V1 => "v1",
                Self::V1Alpha2 => "v1alpha2",
            }
        }

        pub fn api_version(&self) -> &'static str {
            match self {
                Self::V1 => "gateway.networking.k8s.io/v1",
                Self::V1Alpha2 => "gateway.networking.k8s.io/v1alpha2",
            }
        }
    }

    /// A `TLSRoute` bound to `v1alpha2`.
    ///
    /// The `gateway-api` crate binds `TLSRoute` to `v1`, which is only served
    /// by Gateway API v1.5 and later. Clusters running v1.2--v1.4, as well as
    /// clusters using the CRDs vendored by the `linkerd-crds` chart, serve
    /// `v1alpha2` instead, so we keep a second binding for them and pick
    /// between the two at runtime.
    ///
    /// The spec and status types are reused from the `gateway-api` crate, so
    /// this only overrides the group/version/kind the client addresses; the
    /// one schema difference between the versions -- `v1` requires
    /// `spec.hostnames` and `v1alpha2` does not -- is handled by
    /// `deserialize_v1alpha2_spec`.
    #[derive(Clone, Debug, Default, PartialEq, serde::Deserialize)]
    #[serde(rename_all = "camelCase")]
    pub struct TLSRouteV1Alpha2 {
        pub metadata: k8s_openapi::apimachinery::pkg::apis::meta::v1::ObjectMeta,
        #[serde(deserialize_with = "deserialize_v1alpha2_spec")]
        pub spec: TlsRouteSpec,
        pub status: Option<TlsRouteStatus>,
    }

    /// A `TlsRouteSpec` read from a `v1alpha2` payload.
    ///
    /// Admission requests are dispatched on the version they carry, so this
    /// makes `deserialize_v1alpha2_spec`'s leniency available to the admission
    /// controller, which parses specs on their own.
    #[derive(Clone, Debug, PartialEq)]
    pub struct TlsRouteSpecV1Alpha2(pub TlsRouteSpec);

    impl<'de> serde::Deserialize<'de> for TlsRouteSpecV1Alpha2 {
        fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
        where
            D: serde::Deserializer<'de>,
        {
            deserialize_v1alpha2_spec(deserializer).map(Self)
        }
    }

    /// Deserializes a `TlsRouteSpec` from a `v1alpha2` payload, where
    /// `hostnames` is optional.
    ///
    /// The `gateway-api` crate's `TlsRouteSpec` mirrors the `v1` schema, which
    /// requires `hostnames`; deserializing a `v1alpha2` route that omits it
    /// would fail, and the route would be dropped from the index.
    fn deserialize_v1alpha2_spec<'de, D>(deserializer: D) -> Result<TlsRouteSpec, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        use serde::Deserialize;

        #[derive(Deserialize)]
        #[serde(rename_all = "camelCase")]
        struct V1Alpha2Spec {
            #[serde(default)]
            hostnames: Vec<String>,
            parent_refs: Option<Vec<TlsRouteParentRefs>>,
            rules: Vec<TlsRouteRules>,
            use_default_gateways: Option<TlsRouteUseDefaultGateways>,
        }

        let V1Alpha2Spec {
            hostnames,
            parent_refs,
            rules,
            use_default_gateways,
        } = V1Alpha2Spec::deserialize(deserializer)?;
        Ok(TlsRouteSpec {
            hostnames,
            parent_refs,
            rules,
            use_default_gateways,
        })
    }

    impl kube::Resource for TLSRouteV1Alpha2 {
        type DynamicType = ();
        type Scope = kube::core::NamespaceResourceScope;

        fn kind(_: &()) -> std::borrow::Cow<'_, str> {
            "TLSRoute".into()
        }

        fn group(_: &()) -> std::borrow::Cow<'_, str> {
            "gateway.networking.k8s.io".into()
        }

        fn version(_: &()) -> std::borrow::Cow<'_, str> {
            TlsRouteApiVersion::V1Alpha2.version().into()
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
    impl serde::Serialize for TLSRouteV1Alpha2 {
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

    /// The versions are the same object under a different `apiVersion`, so
    /// routes read from a `v1alpha2` watch are indexed as if they had been read
    /// from a `v1` watch.
    impl From<TLSRouteV1Alpha2> for TLSRoute {
        fn from(
            TLSRouteV1Alpha2 {
                metadata,
                spec,
                status,
            }: TLSRouteV1Alpha2,
        ) -> Self {
            Self {
                metadata,
                spec,
                status,
            }
        }
    }

    #[cfg(test)]
    mod tests {
        use super::*;
        use kube::Resource;

        /// `TLSRouteV1Alpha2` must stay on `v1alpha2`; `v1` is not served
        /// before Gateway API v1.5. A regression here fails silently at
        /// runtime: the watch is skipped and TLSRoute policy stops being
        /// applied on older clusters.
        #[test]
        fn tls_route_is_v1alpha2() {
            assert_eq!(
                TLSRouteV1Alpha2::api_version(&()),
                "gateway.networking.k8s.io/v1alpha2"
            );
            assert_eq!(
                TLSRouteV1Alpha2::url_path(&(), Some("ns")),
                "/apis/gateway.networking.k8s.io/v1alpha2/namespaces/ns/tlsroutes"
            );

            // `apiVersion` and `kind` are not struct fields, so they are only
            // emitted by the hand-written `Serialize` impl.
            let json = serde_json::to_value(TLSRouteV1Alpha2::default()).unwrap();
            assert_eq!(json["apiVersion"], "gateway.networking.k8s.io/v1alpha2");
            assert_eq!(json["kind"], "TLSRoute");
        }

        /// The `gateway-api` crate binds `TLSRoute` to `v1`, which is the
        /// version we prefer whenever the cluster serves it.
        #[test]
        fn tls_route_is_v1() {
            assert_eq!(TLSRoute::api_version(&()), "gateway.networking.k8s.io/v1");
            assert_eq!(
                TlsRouteApiVersion::V1.api_version(),
                TLSRoute::api_version(&())
            );
            assert_eq!(
                TlsRouteApiVersion::V1Alpha2.api_version(),
                TLSRouteV1Alpha2::api_version(&())
            );
        }

        /// `hostnames` is required by the `v1` schema but optional in
        /// `v1alpha2`, so routes that omit it must still be readable.
        #[test]
        fn v1alpha2_hostnames_are_optional() {
            let route: TLSRouteV1Alpha2 = serde_json::from_value(serde_json::json!({
                "apiVersion": "gateway.networking.k8s.io/v1alpha2",
                "kind": "TLSRoute",
                "metadata": { "name": "test", "namespace": "ns" },
                "spec": {
                    "parentRefs": [{ "name": "svc", "port": 4143 }],
                    "rules": [{ "backendRefs": [{ "name": "svc", "port": 4143 }] }],
                },
            }))
            .unwrap();
            assert!(route.spec.hostnames.is_empty());

            let route = TLSRoute::from(route);
            assert_eq!(route.spec.rules.len(), 1);
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
