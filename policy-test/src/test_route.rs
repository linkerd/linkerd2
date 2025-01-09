use k8s_gateway_api::{self as gateway, BackendRef, ParentReference};
use k8s_openapi::Resource;
use linkerd2_proxy_api::{meta, meta::Metadata, outbound};
use linkerd_policy_controller_k8s_api::{
    self as k8s, policy, Condition, Resource as _, ResourceExt,
};

use crate::outbound_api::{detect_http_routes, grpc_routes, tcp_routes, tls_routes};

pub trait TestRoute:
    kube::Resource<Scope = kube::core::NamespaceResourceScope, DynamicType: Default>
    + serde::Serialize
    + serde::de::DeserializeOwned
    + Clone
    + std::fmt::Debug
    + Send
    + Sync
    + 'static
{
    type Route;
    type Backend;
    type Filter;

    fn make_route(
        ns: impl ToString,
        parents: Vec<ParentReference>,
        rules: Vec<Vec<BackendRef>>,
    ) -> Self;
    fn routes<F>(config: &outbound::OutboundPolicy, f: F)
    where
        F: Fn(&[Self::Route]);
    fn parents_mut(&mut self) -> Vec<&mut ParentReference>;
    fn extract_meta(route: &Self::Route) -> &Metadata;
    fn backend_filters(backend: &Self::Backend) -> Vec<&Self::Filter>;
    fn rules_first_available(route: &Self::Route) -> Vec<Vec<&Self::Backend>>;
    fn rules_random_available(route: &Self::Route) -> Vec<Vec<&Self::Backend>>;
    fn backend(backend: &Self::Backend) -> &outbound::Backend;
    fn conditions(&self) -> Option<Vec<&Condition>>;
    fn is_failure_filter(filter: &Self::Filter) -> bool;

    fn meta_eq(&self, meta: &Metadata) -> bool {
        let meta = match &meta.kind {
            Some(meta::metadata::Kind::Resource(r)) => r,
            _ => return false,
        };
        let dt = Default::default();
        self.meta().name.as_ref() == Some(&meta.name)
            && self.meta().namespace.as_ref() == Some(&meta.namespace)
            && Self::kind(&dt) == meta.kind
            && Self::group(&dt) == meta.group
    }
}

#[allow(async_fn_in_trait)]
pub trait TestParent:
    kube::Resource<Scope = kube::core::NamespaceResourceScope, DynamicType: Default>
    + serde::Serialize
    + serde::de::DeserializeOwned
    + Clone
    + std::fmt::Debug
    + Send
    + Sync
{
    fn make_parent(ns: impl ToString) -> Self;
    fn make_backend(ns: impl ToString) -> Option<Self>;
    fn conditions(&self) -> Vec<&Condition>;
    fn obj_ref(&self) -> ParentReference;
    fn backend_ref(&self, port: u16) -> gateway::BackendRef {
        let dt = Default::default();
        gateway::BackendRef {
            weight: None,
            inner: gateway::BackendObjectReference {
                group: Some(Self::group(&dt).to_string()),
                kind: Some(Self::kind(&dt).to_string()),
                name: self.name_unchecked(),
                namespace: self.namespace(),
                port: Some(port),
            },
        }
    }
    fn ip(&self) -> &str;
}

impl TestRoute for gateway::HttpRoute {
    type Route = outbound::HttpRoute;
    type Backend = outbound::http_route::RouteBackend;
    type Filter = outbound::http_route::Filter;

    fn make_route(
        ns: impl ToString,
        parents: Vec<ParentReference>,
        rules: Vec<Vec<BackendRef>>,
    ) -> Self {
        let rules = rules
            .into_iter()
            .map(|backends| {
                let backends = backends
                    .into_iter()
                    .map(|backend| gateway::HttpBackendRef {
                        backend_ref: Some(backend),
                        filters: None,
                    })
                    .collect();
                gateway::HttpRouteRule {
                    matches: Some(vec![]),
                    filters: None,
                    backend_refs: Some(backends),
                }
            })
            .collect();
        gateway::HttpRoute {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("foo-route".to_string()),
                ..Default::default()
            },
            spec: gateway::HttpRouteSpec {
                inner: gateway::CommonRouteSpec {
                    parent_refs: Some(parents),
                },
                hostnames: None,
                rules: Some(rules),
            },
            status: None,
        }
    }

    fn routes<F>(config: &outbound::OutboundPolicy, f: F)
    where
        F: Fn(&[outbound::HttpRoute]),
    {
        detect_http_routes(config, f);
    }

    fn extract_meta(route: &outbound::HttpRoute) -> &Metadata {
        route.metadata.as_ref().unwrap()
    }

    fn backend_filters(
        backend: &outbound::http_route::RouteBackend,
    ) -> Vec<&outbound::http_route::Filter> {
        backend.filters.iter().collect()
    }

    fn rules_first_available(
        route: &outbound::HttpRoute,
    ) -> Vec<Vec<&outbound::http_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::http_route::distribution::Kind::FirstAvailable(first_available) => {
                        first_available.backends.iter().collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn rules_random_available(
        route: &outbound::HttpRoute,
    ) -> Vec<Vec<&outbound::http_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::http_route::distribution::Kind::RandomAvailable(random_available) => {
                        random_available
                            .backends
                            .iter()
                            .map(|backend| backend.backend.as_ref().unwrap())
                            .collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn backend(backend: &outbound::http_route::RouteBackend) -> &outbound::Backend {
        backend.backend.as_ref().unwrap()
    }

    fn conditions(&self) -> Option<Vec<&Condition>> {
        self.status.as_ref().map(|status| {
            status
                .inner
                .parents
                .iter()
                .map(|parent_status| &parent_status.conditions)
                .flatten()
                .collect()
        })
    }

    fn is_failure_filter(filter: &outbound::http_route::Filter) -> bool {
        match filter.kind.as_ref().unwrap() {
            outbound::http_route::filter::Kind::FailureInjector(_) => true,
            _ => false,
        }
    }

    fn parents_mut(&mut self) -> Vec<&mut ParentReference> {
        self.spec
            .inner
            .parent_refs
            .as_mut()
            .unwrap()
            .iter_mut()
            .collect()
    }
}

impl TestRoute for policy::HttpRoute {
    type Route = outbound::HttpRoute;
    type Backend = outbound::http_route::RouteBackend;
    type Filter = outbound::http_route::Filter;

    fn make_route(
        ns: impl ToString,
        parents: Vec<ParentReference>,
        rules: Vec<Vec<BackendRef>>,
    ) -> Self {
        let rules = rules
            .into_iter()
            .map(|backends| {
                let backends = backends
                    .into_iter()
                    .map(|backend| gateway::HttpBackendRef {
                        backend_ref: Some(backend),
                        filters: None,
                    })
                    .collect();
                policy::httproute::HttpRouteRule {
                    matches: Some(vec![]),
                    filters: None,
                    timeouts: None,
                    backend_refs: Some(backends),
                }
            })
            .collect();
        policy::HttpRoute {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("foo-route".to_string()),
                ..Default::default()
            },
            spec: policy::HttpRouteSpec {
                inner: gateway::CommonRouteSpec {
                    parent_refs: Some(parents),
                },
                hostnames: None,
                rules: Some(rules),
            },
            status: None,
        }
    }

    fn routes<F>(config: &outbound::OutboundPolicy, f: F)
    where
        F: Fn(&[outbound::HttpRoute]),
    {
        detect_http_routes(config, f);
    }

    fn extract_meta(route: &outbound::HttpRoute) -> &Metadata {
        route.metadata.as_ref().unwrap()
    }

    fn backend_filters(
        backend: &outbound::http_route::RouteBackend,
    ) -> Vec<&outbound::http_route::Filter> {
        backend.filters.iter().collect()
    }

    fn rules_first_available(
        route: &outbound::HttpRoute,
    ) -> Vec<Vec<&outbound::http_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::http_route::distribution::Kind::FirstAvailable(first_available) => {
                        first_available.backends.iter().collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn rules_random_available(
        route: &outbound::HttpRoute,
    ) -> Vec<Vec<&outbound::http_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::http_route::distribution::Kind::RandomAvailable(random_available) => {
                        random_available
                            .backends
                            .iter()
                            .map(|backend| backend.backend.as_ref().unwrap())
                            .collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn backend(backend: &outbound::http_route::RouteBackend) -> &outbound::Backend {
        backend.backend.as_ref().unwrap()
    }

    fn conditions(&self) -> Option<Vec<&Condition>> {
        self.status.as_ref().map(|status| {
            status
                .inner
                .parents
                .iter()
                .map(|parent_status| &parent_status.conditions)
                .flatten()
                .collect()
        })
    }

    fn is_failure_filter(filter: &outbound::http_route::Filter) -> bool {
        match filter.kind.as_ref().unwrap() {
            outbound::http_route::filter::Kind::FailureInjector(_) => true,
            _ => false,
        }
    }

    fn parents_mut(&mut self) -> Vec<&mut ParentReference> {
        self.spec
            .inner
            .parent_refs
            .as_mut()
            .unwrap()
            .iter_mut()
            .collect()
    }
}

impl TestRoute for gateway::GrpcRoute {
    type Route = outbound::GrpcRoute;
    type Backend = outbound::grpc_route::RouteBackend;
    type Filter = outbound::grpc_route::Filter;

    fn make_route(
        ns: impl ToString,
        parents: Vec<ParentReference>,
        rules: Vec<Vec<BackendRef>>,
    ) -> Self {
        let rules = rules
            .into_iter()
            .map(|backends| {
                let backends = backends
                    .into_iter()
                    .map(|backend| gateway::GrpcRouteBackendRef {
                        filters: None,
                        inner: backend.inner,
                        weight: None,
                    })
                    .collect();
                gateway::GrpcRouteRule {
                    matches: Some(vec![]),
                    filters: None,
                    backend_refs: Some(backends),
                }
            })
            .collect();
        gateway::GrpcRoute {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("foo-route".to_string()),
                ..Default::default()
            },
            spec: gateway::GrpcRouteSpec {
                inner: gateway::CommonRouteSpec {
                    parent_refs: Some(parents),
                },
                hostnames: None,
                rules: Some(rules),
            },
            status: None,
        }
    }

    fn routes<F>(config: &outbound::OutboundPolicy, f: F)
    where
        F: Fn(&[outbound::GrpcRoute]),
    {
        f(grpc_routes(config));
    }

    fn extract_meta(route: &outbound::GrpcRoute) -> &Metadata {
        route.metadata.as_ref().unwrap()
    }

    fn backend_filters(
        backend: &outbound::grpc_route::RouteBackend,
    ) -> Vec<&outbound::grpc_route::Filter> {
        backend.filters.iter().collect()
    }

    fn rules_first_available(
        route: &outbound::GrpcRoute,
    ) -> Vec<Vec<&outbound::grpc_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::grpc_route::distribution::Kind::FirstAvailable(first_available) => {
                        first_available.backends.iter().collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn rules_random_available(
        route: &outbound::GrpcRoute,
    ) -> Vec<Vec<&outbound::grpc_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::grpc_route::distribution::Kind::RandomAvailable(random_available) => {
                        random_available
                            .backends
                            .iter()
                            .map(|backend| backend.backend.as_ref().unwrap())
                            .collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn backend(backend: &outbound::grpc_route::RouteBackend) -> &outbound::Backend {
        backend.backend.as_ref().unwrap()
    }

    fn conditions(&self) -> Option<Vec<&Condition>> {
        self.status.as_ref().map(|status| {
            status
                .inner
                .parents
                .iter()
                .map(|parent_status| &parent_status.conditions)
                .flatten()
                .collect()
        })
    }

    fn is_failure_filter(filter: &outbound::grpc_route::Filter) -> bool {
        match filter.kind.as_ref().unwrap() {
            outbound::grpc_route::filter::Kind::FailureInjector(_) => true,
            _ => false,
        }
    }

    fn parents_mut(&mut self) -> Vec<&mut ParentReference> {
        self.spec
            .inner
            .parent_refs
            .as_mut()
            .unwrap()
            .iter_mut()
            .collect()
    }
}

impl TestRoute for gateway::TlsRoute {
    type Route = outbound::TlsRoute;
    type Backend = outbound::tls_route::RouteBackend;
    type Filter = outbound::tls_route::Filter;

    fn make_route(
        ns: impl ToString,
        parents: Vec<ParentReference>,
        rules: Vec<Vec<BackendRef>>,
    ) -> Self {
        let rules = rules
            .into_iter()
            .map(|backends| gateway::TlsRouteRule {
                backend_refs: backends,
            })
            .collect();
        gateway::TlsRoute {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("foo-route".to_string()),
                ..Default::default()
            },
            spec: gateway::TlsRouteSpec {
                inner: gateway::CommonRouteSpec {
                    parent_refs: Some(parents),
                },
                hostnames: None,
                rules,
            },
            status: None,
        }
    }

    fn routes<F>(config: &outbound::OutboundPolicy, f: F)
    where
        F: Fn(&[outbound::TlsRoute]),
    {
        f(tls_routes(config));
    }

    fn extract_meta(route: &outbound::TlsRoute) -> &Metadata {
        route.metadata.as_ref().unwrap()
    }

    fn backend_filters(
        backend: &outbound::tls_route::RouteBackend,
    ) -> Vec<&outbound::tls_route::Filter> {
        backend.filters.iter().collect()
    }

    fn rules_first_available(
        route: &outbound::TlsRoute,
    ) -> Vec<Vec<&outbound::tls_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::tls_route::distribution::Kind::FirstAvailable(first_available) => {
                        first_available.backends.iter().collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn rules_random_available(
        route: &outbound::TlsRoute,
    ) -> Vec<Vec<&outbound::tls_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::tls_route::distribution::Kind::RandomAvailable(random_available) => {
                        random_available
                            .backends
                            .iter()
                            .map(|backend| backend.backend.as_ref().unwrap())
                            .collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn backend(backend: &outbound::tls_route::RouteBackend) -> &outbound::Backend {
        backend.backend.as_ref().unwrap()
    }

    fn conditions(&self) -> Option<Vec<&Condition>> {
        self.status.as_ref().map(|status| {
            status
                .inner
                .parents
                .iter()
                .map(|parent_status| &parent_status.conditions)
                .flatten()
                .collect()
        })
    }

    fn is_failure_filter(filter: &outbound::tls_route::Filter) -> bool {
        match filter.kind.as_ref().unwrap() {
            outbound::tls_route::filter::Kind::Invalid(_) => true,
            _ => false,
        }
    }

    fn parents_mut(&mut self) -> Vec<&mut ParentReference> {
        self.spec
            .inner
            .parent_refs
            .as_mut()
            .unwrap()
            .iter_mut()
            .collect()
    }
}

impl TestRoute for gateway::TcpRoute {
    type Route = outbound::OpaqueRoute;
    type Backend = outbound::opaque_route::RouteBackend;
    type Filter = outbound::opaque_route::Filter;

    fn make_route(
        ns: impl ToString,
        parents: Vec<ParentReference>,
        rules: Vec<Vec<BackendRef>>,
    ) -> Self {
        let rules = rules
            .into_iter()
            .map(|backends| gateway::TcpRouteRule {
                backend_refs: backends,
            })
            .collect();
        gateway::TcpRoute {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("foo-route".to_string()),
                ..Default::default()
            },
            spec: gateway::TcpRouteSpec {
                inner: gateway::CommonRouteSpec {
                    parent_refs: Some(parents),
                },
                rules,
            },
            status: None,
        }
    }

    fn routes<F>(config: &outbound::OutboundPolicy, f: F)
    where
        F: Fn(&[outbound::OpaqueRoute]),
    {
        f(tcp_routes(config));
    }

    fn extract_meta(route: &outbound::OpaqueRoute) -> &Metadata {
        route.metadata.as_ref().unwrap()
    }

    fn backend_filters(
        backend: &outbound::opaque_route::RouteBackend,
    ) -> Vec<&outbound::opaque_route::Filter> {
        backend.filters.iter().collect()
    }

    fn rules_first_available(
        route: &outbound::OpaqueRoute,
    ) -> Vec<Vec<&outbound::opaque_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::opaque_route::distribution::Kind::FirstAvailable(first_available) => {
                        first_available.backends.iter().collect()
                    }
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn rules_random_available(
        route: &outbound::OpaqueRoute,
    ) -> Vec<Vec<&outbound::opaque_route::RouteBackend>> {
        route
            .rules
            .iter()
            .map(
                |rule| match rule.backends.as_ref().unwrap().kind.as_ref().unwrap() {
                    outbound::opaque_route::distribution::Kind::RandomAvailable(
                        random_available,
                    ) => random_available
                        .backends
                        .iter()
                        .map(|backend| backend.backend.as_ref().unwrap())
                        .collect(),
                    _ => panic!("unexpected distribution kind"),
                },
            )
            .collect()
    }

    fn backend(backend: &outbound::opaque_route::RouteBackend) -> &outbound::Backend {
        backend.backend.as_ref().unwrap()
    }

    fn conditions(&self) -> Option<Vec<&Condition>> {
        self.status.as_ref().map(|status| {
            status
                .inner
                .parents
                .iter()
                .map(|parent_status| &parent_status.conditions)
                .flatten()
                .collect()
        })
    }

    fn is_failure_filter(filter: &outbound::opaque_route::Filter) -> bool {
        match filter.kind.as_ref().unwrap() {
            outbound::opaque_route::filter::Kind::Invalid(_) => true,
            _ => false,
        }
    }

    fn parents_mut(&mut self) -> Vec<&mut ParentReference> {
        self.spec
            .inner
            .parent_refs
            .as_mut()
            .unwrap()
            .iter_mut()
            .collect()
    }
}

impl TestParent for k8s::Service {
    fn make_parent(ns: impl ToString) -> Self {
        k8s::Service {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("my-svc".to_string()),
                ..Default::default()
            },
            spec: Some(k8s::ServiceSpec {
                ports: Some(vec![k8s::ServicePort {
                    port: 4191,
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..k8s::Service::default()
        }
    }

    fn make_backend(ns: impl ToString) -> Option<Self> {
        let service = k8s::Service {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("backend".to_string()),
                ..Default::default()
            },
            spec: Some(k8s::ServiceSpec {
                ports: Some(vec![k8s::ServicePort {
                    port: 4191,
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..k8s::Service::default()
        };
        Some(service)
    }

    fn conditions(&self) -> Vec<&Condition> {
        self.status
            .as_ref()
            .unwrap()
            .conditions
            .as_ref()
            .unwrap()
            .iter()
            .collect()
    }

    fn obj_ref(&self) -> ParentReference {
        ParentReference {
            kind: Some(k8s::Service::KIND.to_string()),
            name: self.name_unchecked(),
            namespace: self.namespace(),
            group: Some(k8s::Service::GROUP.to_string()),
            section_name: None,
            port: Some(4191),
        }
    }

    fn ip(&self) -> &str {
        self.spec.as_ref().unwrap().cluster_ip.as_ref().unwrap()
    }
}

impl TestParent for policy::EgressNetwork {
    fn make_parent(ns: impl ToString) -> Self {
        policy::EgressNetwork {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("my-egress".to_string()),
                ..Default::default()
            },
            spec: policy::EgressNetworkSpec {
                networks: None,
                traffic_policy: policy::egress_network::TrafficPolicy::Allow,
            },
            status: None,
        }
    }

    fn make_backend(_ns: impl ToString) -> Option<Self> {
        None
    }

    fn conditions(&self) -> Vec<&Condition> {
        self.status.as_ref().unwrap().conditions.iter().collect()
    }

    fn obj_ref(&self) -> ParentReference {
        ParentReference {
            kind: Some(policy::EgressNetwork::kind(&()).to_string()),
            name: self.name_unchecked(),
            namespace: self.namespace(),
            group: Some(policy::EgressNetwork::group(&()).to_string()),
            section_name: None,
            port: Some(4191),
        }
    }

    fn ip(&self) -> &str {
        // For EgressNetwork, we can just return a non-private
        // IP address as our default cluster setup dictates that
        // all non-private networks are considered egress. Since
        // we do not modify this setting in tests for the time being,
        // returning 1.1.1.1 is fine.
        "1.1.1.1"
    }
}
