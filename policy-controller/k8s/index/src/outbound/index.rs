use crate::{
    ports::{ports_annotation, PortSet},
    routes::{self, ExplicitGKN, RouteResource},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, ensure, Result};
use linkerd_policy_controller_core::{
    outbound::{
        Backend, Backoff, FailureAccrual, Filter, OutboundPolicy, OutboundRoute,
        OutboundRouteCollection, OutboundRouteRule, TypedOutboundRoute, WeightedService,
    },
    routes::{GroupKindNamespaceName, GrpcRouteMatch, HttpRouteMatch},
};
use linkerd_policy_controller_k8s_api::{
    gateway::{self as k8s_gateway_api, BackendObjectReference, HttpBackendRef, ParentReference},
    policy as linkerd_k8s_api, ResourceExt, Service, Time,
};
use parking_lot::RwLock;
use std::{hash::Hash, net::IpAddr, num::NonZeroU16, sync::Arc, time};
use tokio::sync::watch;

#[derive(Debug)]
pub struct Index {
    namespaces: NamespaceIndex,
    services_by_ip: HashMap<IpAddr, ServiceRef>,
    service_info: HashMap<ServiceRef, ServiceInfo>,
}

pub mod metrics;

pub type SharedIndex = Arc<RwLock<Index>>;

#[derive(Debug, Clone, Hash, PartialEq, Eq)]
pub struct ServiceRef {
    pub name: String,
    pub namespace: String,
}

/// Holds all `Pod`, `Server`, and `ServerAuthorization` indices by-namespace.
#[derive(Debug)]
struct NamespaceIndex {
    cluster_info: Arc<ClusterInfo>,
    by_ns: HashMap<String, Namespace>,
}

#[derive(Debug)]
struct Namespace {
    /// Stores an observable handle for each known service:port,
    /// as well as any route resources in the cluster that specify
    /// a port.
    service_port_routes: HashMap<ServicePort, ServiceRoutes>,
    /// Stores the route resources (by service name) that do not
    /// explicitly target a port.
    service_routes: HashMap<String, Option<OutboundRouteCollection>>,
    namespace: Arc<String>,
}

#[derive(Debug, Default)]
struct ServiceInfo {
    opaque_ports: PortSet,
    accrual: Option<FailureAccrual>,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
struct ServicePort {
    service: String,
    port: NonZeroU16,
}

#[derive(Debug)]
struct ServiceRoutes {
    namespace: Arc<String>,
    name: String,
    port: NonZeroU16,
    authority: String,
    watches_by_ns: HashMap<String, RoutesWatch>,
    opaque: bool,
    accrual: Option<FailureAccrual>,
}

#[derive(Debug)]
struct RoutesWatch {
    opaque: bool,
    accrual: Option<FailureAccrual>,
    routes: Option<OutboundRouteCollection>,
    watch: watch::Sender<OutboundPolicy>,
}

#[inline]
fn default_collection_for(route: &TypedOutboundRoute) -> OutboundRouteCollection {
    match route {
        TypedOutboundRoute::Grpc(_) => OutboundRouteCollection::Grpc(Default::default()),
        TypedOutboundRoute::Http(_) => OutboundRouteCollection::Http(Default::default()),
    }
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::HttpRoute> for Index {
    fn apply(&mut self, route: linkerd_k8s_api::HttpRoute) {
        self.apply(RouteResource::LinkerdHttp(route))
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name
            .gkn::<linkerd_k8s_api::HttpRoute>()
            .namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::HttpRoute> for Index {
    fn apply(&mut self, route: k8s_gateway_api::HttpRoute) {
        self.apply(RouteResource::GatewayHttp(route))
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name
            .gkn::<k8s_gateway_api::HttpRoute>()
            .namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::GrpcRoute> for Index {
    fn apply(&mut self, route: k8s_gateway_api::GrpcRoute) {
        self.apply(RouteResource::GatewayGrpc(route))
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name
            .gkn::<k8s_gateway_api::GrpcRoute>()
            .namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<Service> for Index {
    fn apply(&mut self, service: Service) {
        let name = service.name_unchecked();
        let ns = service.namespace().expect("Service must have a namespace");
        let accrual = parse_accrual_config(service.annotations())
            .map_err(|error| tracing::error!(%error, service=name, namespace=ns, "failed to parse accrual config"))
            .unwrap_or_default();
        let opaque_ports =
            ports_annotation(service.annotations(), "config.linkerd.io/opaque-ports")
                .unwrap_or_else(|| self.namespaces.cluster_info.default_opaque_ports.clone());

        if let Some(cluster_ips) = service
            .spec
            .as_ref()
            .and_then(|spec| spec.cluster_ips.as_deref())
        {
            for cluster_ip in cluster_ips {
                if cluster_ip == "None" {
                    continue;
                }
                match cluster_ip.parse() {
                    Ok(addr) => {
                        let service_ref = ServiceRef {
                            name: name.clone(),
                            namespace: ns.clone(),
                        };
                        self.services_by_ip.insert(addr, service_ref);
                    }
                    Err(error) => {
                        tracing::error!(%error, service=name, cluster_ip, "invalid cluster ip");
                    }
                }
            }
        }

        let service_info = ServiceInfo {
            opaque_ports,
            accrual,
        };

        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_routes: Default::default(),
                service_port_routes: Default::default(),
                namespace: Arc::new(ns),
            })
            .update_service(service.name_unchecked(), &service_info);

        self.service_info.insert(
            ServiceRef {
                name: service.name_unchecked(),
                namespace: service.namespace().expect("Service must have Namespace"),
            },
            service_info,
        );
    }

    fn delete(&mut self, namespace: String, name: String) {
        let service_ref = ServiceRef { name, namespace };
        self.service_info.remove(&service_ref);
        self.services_by_ip.retain(|_, v| *v != service_ref);
    }
}

impl Index {
    pub fn shared(cluster_info: Arc<ClusterInfo>) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            namespaces: NamespaceIndex {
                by_ns: HashMap::default(),
                cluster_info,
            },
            services_by_ip: HashMap::default(),
            service_info: HashMap::default(),
        }))
    }

    pub fn outbound_policy_rx(
        &mut self,
        service_name: String,
        service_namespace: String,
        service_port: NonZeroU16,
        source_namespace: String,
    ) -> Result<watch::Receiver<OutboundPolicy>> {
        let ns = self
            .namespaces
            .by_ns
            .entry(service_namespace.clone())
            .or_insert_with(|| Namespace {
                namespace: Arc::new(service_namespace.to_string()),
                ..Default::default()
            });

        let key = ServicePort {
            service: service_name,
            port: service_port,
        };

        tracing::debug!(?key, "subscribing to service port");

        let routes =
            ns.service_routes_or_default(key, &self.namespaces.cluster_info, &self.service_info);

        let watch = routes.watch_for_ns_or_default(source_namespace);

        Ok(watch.watch.subscribe())
    }

    pub fn lookup_service(&self, addr: IpAddr) -> Option<ServiceRef> {
        self.services_by_ip.get(&addr).cloned()
    }

    fn apply(&mut self, route: RouteResource) {
        tracing::debug!(name = route.name(), "indexing route");

        for parent_ref in route.inner().parent_refs.iter().flatten() {
            if !is_parent_service(parent_ref) {
                continue;
            }

            if !route_accepted_by_service(route.status(), &parent_ref.name) {
                continue;
            }

            let ns = parent_ref
                .namespace
                .clone()
                .unwrap_or_else(|| route.namespace());

            self.namespaces
                .by_ns
                .entry(ns.clone())
                .or_insert_with(|| Namespace {
                    namespace: Arc::new(ns),
                    ..Default::default()
                })
                .apply(
                    route.clone(),
                    parent_ref,
                    &self.namespaces.cluster_info,
                    &self.service_info,
                );
        }
    }
}

impl Default for Namespace {
    fn default() -> Self {
        Self {
            service_routes: HashMap::new(),
            service_port_routes: HashMap::new(),
            namespace: Arc::new("default".to_string()),
        }
    }
}

impl Namespace {
    fn apply(
        &mut self,
        route: RouteResource,
        parent_ref: &ParentReference,
        cluster_info: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) {
        tracing::debug!(?route);

        let outbound_route = match self.convert_route(route.clone(), cluster_info, service_info) {
            Ok(route) => route,
            Err(error) => {
                tracing::error!(%error, "failed to convert route");
                return;
            }
        };

        tracing::debug!(?outbound_route);

        let port = parent_ref.port.and_then(NonZeroU16::new);

        if let Some(port) = port {
            let service_port = ServicePort {
                port,
                service: parent_ref.name.clone(),
            };

            tracing::debug!(
                ?service_port,
                route = route.name(),
                "inserting route for service"
            );

            let service_routes =
                self.service_routes_or_default(service_port, cluster_info, service_info);

            service_routes.apply(route.gknn(), outbound_route);
        } else {
            // If the parent_ref doesn't include a port, apply this route
            // to all ServiceRoutes which match the Service name.
            self.service_port_routes.iter_mut().for_each(
                |(ServicePort { service, port: _ }, routes)| {
                    if service == &parent_ref.name {
                        routes.apply(route.gknn(), outbound_route.clone());
                    }
                },
            );

            // Also add the route to the list of routes that target the
            // Service without specifying a port.
            match self
                .service_routes
                .entry(parent_ref.name.clone())
                .or_insert_with(|| Some(default_collection_for(&outbound_route)))
            {
                Some(routes) => {
                    routes
                        .insert(route.gknn(), outbound_route)
                        .map_err(|error| tracing::warn!(?error))
                        .transpose();
                }
                None => {
                    let mut routes = default_collection_for(&outbound_route);
                    routes
                        .insert(route.gknn(), outbound_route)
                        .map_err(|error| tracing::warn!(?error))
                        .transpose();
                    self.service_routes
                        .insert(parent_ref.name.clone(), Some(routes));
                }
            };
        }
    }

    fn update_service(&mut self, name: String, service: &ServiceInfo) {
        tracing::debug!(?name, ?service, "updating service");

        for (svc_port, svc_routes) in self.service_port_routes.iter_mut() {
            if svc_port.service != name {
                continue;
            }

            let opaque = service.opaque_ports.contains(&svc_port.port);

            svc_routes.update_service(opaque, service.accrual);
        }
    }

    fn delete(&mut self, gknn: &GroupKindNamespaceName) {
        for service in self.service_port_routes.values_mut() {
            service.delete(gknn);
        }

        for entry in self.service_routes.values_mut() {
            if let Some(routes) = entry.as_mut() {
                routes.remove(gknn);

                if routes.is_empty() {
                    *entry = None;
                }
            }
        }
    }

    fn service_routes_or_default(
        &mut self,
        sp: ServicePort,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) -> &mut ServiceRoutes {
        self.service_port_routes
            .entry(sp.clone())
            .or_insert_with(|| {
                let authority =
                    cluster.service_dns_authority(&self.namespace, &sp.service, sp.port);

                let service_ref = ServiceRef {
                    name: sp.service.clone(),
                    namespace: self.namespace.to_string(),
                };

                let (opaque, accrual) = match service_info.get(&service_ref) {
                    Some(svc) => (svc.opaque_ports.contains(&sp.port), svc.accrual),
                    None => (false, None),
                };

                // The routes which target this Service but don't specify
                // a port apply to all ports. Therefore, we include them.
                let routes = self
                    .service_routes
                    .get(&sp.service)
                    .cloned()
                    .unwrap_or_else(|| None);

                let mut service_routes = ServiceRoutes {
                    opaque,
                    accrual,
                    authority,
                    port: sp.port,
                    name: sp.service,
                    namespace: self.namespace.clone(),
                    watches_by_ns: Default::default(),
                };

                match routes {
                    None => {}
                    Some(OutboundRouteCollection::Grpc(routes)) => {
                        service_routes
                            .insert_producer_and_consumer_routes(routes, self.namespace.as_str());
                    }
                    Some(OutboundRouteCollection::Http(routes)) => {
                        service_routes
                            .insert_producer_and_consumer_routes(routes, self.namespace.as_str());
                    }
                };

                service_routes
            })
    }

    fn convert_route(
        &self,
        route: RouteResource,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) -> Result<TypedOutboundRoute> {
        match route {
            RouteResource::LinkerdHttp(route) => {
                let hostnames = route
                    .spec
                    .hostnames
                    .into_iter()
                    .flatten()
                    .map(routes::http::host_match)
                    .collect();

                let rules = route
                    .spec
                    .rules
                    .into_iter()
                    .flatten()
                    .map(|r| self.convert_linkerd_rule(r, cluster, service_info))
                    .collect::<Result<_>>()?;

                let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

                Ok(TypedOutboundRoute::Http(OutboundRoute {
                    hostnames,
                    rules,
                    creation_timestamp,
                }))
            }
            RouteResource::GatewayHttp(route) => {
                let hostnames = route
                    .spec
                    .hostnames
                    .into_iter()
                    .flatten()
                    .map(routes::http::host_match)
                    .collect();

                let rules = route
                    .spec
                    .rules
                    .into_iter()
                    .flatten()
                    .map(|r| self.convert_gateway_http_rule(r, cluster, service_info))
                    .collect::<Result<_>>()?;

                let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

                Ok(TypedOutboundRoute::Http(OutboundRoute {
                    hostnames,
                    rules,
                    creation_timestamp,
                }))
            }
            RouteResource::GatewayGrpc(route) => {
                let hostnames = route
                    .spec
                    .hostnames
                    .into_iter()
                    .flatten()
                    .map(routes::http::host_match)
                    .collect();

                let rules = route
                    .spec
                    .rules
                    .into_iter()
                    .flatten()
                    .map(|r| self.convert_gateway_grpc_rule(r, cluster, service_info))
                    .collect::<Result<_>>()?;

                let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

                Ok(TypedOutboundRoute::Grpc(OutboundRoute {
                    hostnames,
                    rules,
                    creation_timestamp,
                }))
            }
        }
    }

    fn convert_linkerd_rule(
        &self,
        rule: linkerd_k8s_api::httproute::HttpRouteRule,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) -> Result<OutboundRouteRule<HttpRouteMatch>> {
        let matches = rule
            .matches
            .into_iter()
            .flatten()
            .map(routes::http::try_match)
            .collect::<Result<_>>()?;

        let backends = rule
            .backend_refs
            .into_iter()
            .flatten()
            .filter_map(|b| convert_backend(&self.namespace, b, cluster, service_info))
            .collect();

        let filters = rule
            .filters
            .into_iter()
            .flatten()
            .map(convert_linkerd_filter)
            .collect::<Result<_>>()?;

        let request_timeout = rule.timeouts.as_ref().and_then(|timeouts| {
            let timeout = time::Duration::from(timeouts.request?);

            // zero means "no timeout", per GEP-1742
            if timeout == time::Duration::from_nanos(0) {
                return None;
            }

            Some(timeout)
        });

        let backend_request_timeout = rule.timeouts.as_ref().and_then(
            |timeouts: &linkerd_k8s_api::httproute::HttpRouteTimeouts| {
                let timeout = time::Duration::from(timeouts.backend_request?);

                // zero means "no timeout", per GEP-1742
                if timeout == time::Duration::from_nanos(0) {
                    return None;
                }

                Some(timeout)
            },
        );

        Ok(OutboundRouteRule {
            matches,
            backends,
            request_timeout,
            backend_request_timeout,
            filters,
        })
    }

    fn convert_gateway_http_rule(
        &self,
        rule: k8s_gateway_api::HttpRouteRule,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) -> Result<OutboundRouteRule<HttpRouteMatch>> {
        let matches = rule
            .matches
            .into_iter()
            .flatten()
            .map(routes::http::try_match)
            .collect::<Result<_>>()?;

        let backends = rule
            .backend_refs
            .into_iter()
            .flatten()
            .filter_map(|b| convert_backend(&self.namespace, b, cluster, service_info))
            .collect();

        let filters = rule
            .filters
            .into_iter()
            .flatten()
            .map(convert_gateway_filter)
            .collect::<Result<_>>()?;

        Ok(OutboundRouteRule {
            matches,
            backends,
            request_timeout: None,
            backend_request_timeout: None,
            filters,
        })
    }

    fn convert_gateway_grpc_rule(
        &self,
        rule: k8s_gateway_api::GrpcRouteRule,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) -> Result<OutboundRouteRule<GrpcRouteMatch>> {
        let matches = rule
            .matches
            .into_iter()
            .flatten()
            .map(routes::grpc::try_match)
            .collect::<Result<_>>()?;

        let backends = rule
            .backend_refs
            .into_iter()
            .flatten()
            .filter_map(|b| convert_backend(&self.namespace, b, cluster, service_info))
            .collect();

        let filters = rule
            .filters
            .into_iter()
            .flatten()
            .map(convert_gateway_filter)
            .collect::<Result<_>>()?;

        Ok(OutboundRouteRule {
            matches,
            backends,
            request_timeout: None,
            backend_request_timeout: None,
            filters,
        })
    }
}

fn convert_backend<BackendRef: Into<HttpBackendRef>>(
    ns: &str,
    backend: BackendRef,
    cluster: &ClusterInfo,
    services: &HashMap<ServiceRef, ServiceInfo>,
) -> Option<Backend> {
    let backend = backend.into();
    let filters = backend.filters;
    let backend = backend.backend_ref?;
    if !is_backend_service(&backend.inner) {
        return Some(Backend::Invalid {
            weight: backend.weight.unwrap_or(1).into(),
            message: format!(
                "unsupported backend type {group} {kind}",
                group = backend.inner.group.as_deref().unwrap_or("core"),
                kind = backend.inner.kind.as_deref().unwrap_or("<empty>"),
            ),
        });
    }

    let name = backend.inner.name;
    let weight = backend.weight.unwrap_or(1);

    // The gateway API dictates:
    //
    // Port is required when the referent is a Kubernetes Service.
    let port = match backend
        .inner
        .port
        .and_then(|p| NonZeroU16::try_from(p).ok())
    {
        Some(port) => port,
        None => {
            return Some(Backend::Invalid {
                weight: weight.into(),
                message: format!("missing port for backend Service {name}"),
            })
        }
    };
    let service_ref = ServiceRef {
        name: name.clone(),
        namespace: backend.inner.namespace.unwrap_or_else(|| ns.to_string()),
    };
    if !services.contains_key(&service_ref) {
        return Some(Backend::Invalid {
            weight: weight.into(),
            message: format!("Service not found {name}"),
        });
    }

    let filters = match filters
        .into_iter()
        .flatten()
        .map(convert_gateway_filter)
        .collect::<Result<_>>()
    {
        Ok(filters) => filters,
        Err(error) => {
            return Some(Backend::Invalid {
                weight: backend.weight.unwrap_or(1).into(),
                message: format!("unsupported backend filter: {error}", error = error),
            });
        }
    };

    Some(Backend::Service(WeightedService {
        weight: weight.into(),
        authority: cluster.service_dns_authority(&service_ref.namespace, &name, port),
        name,
        namespace: service_ref.namespace.to_string(),
        port,
        filters,
    }))
}

fn convert_linkerd_filter(filter: linkerd_k8s_api::httproute::HttpRouteFilter) -> Result<Filter> {
    let filter = match filter {
        linkerd_k8s_api::httproute::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = routes::http::header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        linkerd_k8s_api::httproute::HttpRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = routes::http::header_modifier(response_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        linkerd_k8s_api::httproute::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
    };
    Ok(filter)
}

fn convert_gateway_filter<RouteFilter: Into<k8s_gateway_api::HttpRouteFilter>>(
    filter: RouteFilter,
) -> Result<Filter> {
    let filter = filter.into();
    let filter = match filter {
        k8s_gateway_api::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = routes::http::header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        k8s_gateway_api::HttpRouteFilter::ResponseHeaderModifier {
            response_header_modifier,
        } => {
            let filter = routes::http::header_modifier(response_header_modifier)?;
            Filter::ResponseHeaderModifier(filter)
        }

        k8s_gateway_api::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = routes::http::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
        k8s_gateway_api::HttpRouteFilter::RequestMirror { .. } => {
            bail!("RequestMirror filter is not supported")
        }
        k8s_gateway_api::HttpRouteFilter::URLRewrite { .. } => {
            bail!("URLRewrite filter is not supported")
        }
        k8s_gateway_api::HttpRouteFilter::ExtensionRef { .. } => {
            bail!("ExtensionRef filter is not supported")
        }
    };
    Ok(filter)
}

#[inline]
fn is_parent_service(parent: &ParentReference) -> bool {
    parent
        .kind
        .as_deref()
        .map(|k| is_service(parent.group.as_deref(), k))
        // Parent refs require a `kind`.
        .unwrap_or(false)
}

#[inline]
fn route_accepted_by_service(
    route_status: Option<&k8s_gateway_api::RouteStatus>,
    service: &str,
) -> bool {
    route_status
        .as_ref()
        .map(|status| status.parents.as_slice())
        .unwrap_or_default()
        .iter()
        .any(|parent_status| {
            parent_status.parent_ref.name == service
                && parent_status
                    .conditions
                    .iter()
                    .any(|condition| condition.type_ == "Accepted" && condition.status == "True")
        })
}

#[inline]
fn is_backend_service(backend: &BackendObjectReference) -> bool {
    is_service(
        backend.group.as_deref(),
        // Backends default to `Service` if no kind is specified.
        backend.kind.as_deref().unwrap_or("Service"),
    )
}

#[inline]
fn is_service(group: Option<&str>, kind: &str) -> bool {
    // If the group is not specified or empty, assume it's 'core'.
    group
        .map(|g| g.eq_ignore_ascii_case("core") || g.is_empty())
        .unwrap_or(true)
        && kind.eq_ignore_ascii_case("Service")
}

impl ServiceRoutes {
    fn watch_for_ns_or_default(&mut self, namespace: String) -> &mut RoutesWatch {
        // The routes from the producer namespace apply to watches in all
        // namespaces, so we copy them.
        let routes = self
            .watches_by_ns
            .get(self.namespace.as_ref())
            .map(|watch| watch.routes.clone())
            .unwrap_or_else(|| None);

        self.watches_by_ns.entry(namespace).or_insert_with(|| {
            let (sender, _) = watch::channel(OutboundPolicy {
                port: self.port,
                opaque: self.opaque,
                accrual: self.accrual,
                routes: routes.clone(),
                name: self.name.to_string(),
                authority: self.authority.clone(),
                namespace: self.namespace.to_string(),
            });

            RoutesWatch {
                routes,
                watch: sender,
                opaque: self.opaque,
                accrual: self.accrual,
            }
        })
    }

    fn apply(&mut self, gknn: GroupKindNamespaceName, route: TypedOutboundRoute) {
        if *gknn.namespace == *self.namespace {
            // This is a producer namespace route.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());

            watch.insert_route(gknn.clone(), route.clone());

            // Producer routes apply to clients in all namespaces, so
            // apply it to watches for all other namespaces too.
            self.watches_by_ns
                .iter_mut()
                .filter(|(ns, _)| ns != &gknn.namespace.as_ref())
                .for_each(|(_, ns_watch)| {
                    ns_watch.insert_route(gknn.clone(), route.clone());
                });
        } else {
            // This is a consumer namespace route and should only apply to
            // watches from that namespace.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());
            watch.insert_route(gknn, route);
        }
    }

    fn update_service(&mut self, opaque: bool, accrual: Option<FailureAccrual>) {
        self.opaque = opaque;
        self.accrual = accrual;
        for watch in self.watches_by_ns.values_mut() {
            watch.opaque = opaque;
            watch.accrual = accrual;
            watch.send_if_modified();
        }
    }

    fn delete(&mut self, gknn: &GroupKindNamespaceName) {
        self.watches_by_ns.values_mut().for_each(|watch| {
            watch.remove_route(gknn);
        });
    }

    fn insert_producer_and_consumer_routes<Route: Clone + Into<TypedOutboundRoute>>(
        &mut self,
        routes: HashMap<GroupKindNamespaceName, Route>,
        namespace: &str,
    ) {
        // Producer routes are routes in the same namespace as
        // their parent service. Consumer routes are routes in
        // other namespaces.
        let (producer_routes, consumer_routes): (Vec<_>, Vec<_>) = routes
            .into_iter()
            .partition(|(gknn, _)| gknn.namespace.as_ref() == namespace);

        for (consumer_gknn, consumer_route) in consumer_routes {
            // Consumer routes should only apply to watches from the
            // consumer namespace.
            let consumer_watch = self.watch_for_ns_or_default(consumer_gknn.namespace.to_string());

            consumer_watch.insert_route(consumer_gknn.clone(), consumer_route.clone());
        }

        for (producer_gknn, producer_route) in producer_routes {
            // Insert the route into the producer namespace.
            let producer_watch = self.watch_for_ns_or_default(producer_gknn.namespace.to_string());

            producer_watch.insert_route(producer_gknn.clone(), producer_route.clone());

            // Producer routes apply to clients in all namespaces, so
            // apply it to watches for all other namespaces too.
            self.watches_by_ns
                .iter_mut()
                .filter(|(namespace, _)| namespace.as_str() != producer_gknn.namespace.as_ref())
                .for_each(|(_, watch)| {
                    watch.insert_route(producer_gknn.clone(), producer_route.clone())
                });
        }
    }
}

impl RoutesWatch {
    fn send_if_modified(&mut self) {
        self.watch.send_if_modified(|policy| {
            let mut modified = false;

            if self.routes != policy.routes {
                policy.routes = self.routes.clone();
                modified = true;
            }

            if self.opaque != policy.opaque {
                policy.opaque = self.opaque;
                modified = true;
            }

            if self.accrual != policy.accrual {
                policy.accrual = self.accrual;
                modified = true;
            }

            modified
        });
    }

    fn insert_route<Route: Into<TypedOutboundRoute>>(
        &mut self,
        gknn: GroupKindNamespaceName,
        route: Route,
    ) {
        match &mut self.routes {
            Some(routes) => {
                routes
                    .insert(gknn, route)
                    .map_err(|error| tracing::warn!(?error))
                    .transpose();
            }
            None => {
                let route = route.into();
                let mut routes = default_collection_for(&route);
                routes
                    .insert(gknn, route)
                    .map_err(|error| tracing::warn!(?error))
                    .transpose();
                self.routes = Some(routes);
            }
        };

        self.send_if_modified();
    }

    fn remove_route(&mut self, gknn: &GroupKindNamespaceName) {
        if let Some(routes) = &mut self.routes {
            routes.remove(gknn);

            if routes.is_empty() {
                self.routes = None;
            }
        }

        self.send_if_modified();
    }
}

fn parse_accrual_config(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<FailureAccrual>> {
    annotations
        .get("balancer.linkerd.io/failure-accrual")
        .map(|mode| {
            if mode == "consecutive" {
                let max_failures = annotations
                    .get("balancer.linkerd.io/failure-accrual-consecutive-max-failures")
                    .map(|s| s.parse::<u32>())
                    .transpose()?
                    .unwrap_or(7);

                let max_penalty = annotations
                    .get("balancer.linkerd.io/failure-accrual-consecutive-max-penalty")
                    .map(|s| parse_duration(s))
                    .transpose()?
                    .unwrap_or_else(|| time::Duration::from_secs(60));

                let min_penalty = annotations
                    .get("balancer.linkerd.io/failure-accrual-consecutive-min-penalty")
                    .map(|s| parse_duration(s))
                    .transpose()?
                    .unwrap_or_else(|| time::Duration::from_secs(1));
                let jitter = annotations
                    .get("balancer.linkerd.io/failure-accrual-consecutive-jitter-ratio")
                    .map(|s| s.parse::<f32>())
                    .transpose()?
                    .unwrap_or(0.5);
                ensure!(
                    min_penalty <= max_penalty,
                    "min_penalty ({min_penalty:?}) cannot exceed max_penalty ({max_penalty:?})"
                );
                ensure!(
                    max_penalty > time::Duration::from_millis(0),
                    "max_penalty cannot be zero"
                );
                ensure!(jitter >= 0.0, "jitter cannot be negative");
                ensure!(jitter <= 100.0, "jitter cannot be greater than 100");

                Ok(FailureAccrual::Consecutive {
                    max_failures,
                    backoff: Backoff {
                        min_penalty,
                        max_penalty,
                        jitter,
                    },
                })
            } else {
                bail!("unsupported failure accrual mode: {mode}");
            }
        })
        .transpose()
}

fn parse_duration(s: &str) -> Result<time::Duration> {
    let s = s.trim();
    let offset = s
        .rfind(|c: char| c.is_ascii_digit())
        .ok_or_else(|| anyhow::anyhow!("{} does not contain a timeout duration value", s))?;
    let (magnitude, unit) = s.split_at(offset + 1);
    let magnitude = magnitude.parse::<u64>()?;

    let mul = match unit {
        "" if magnitude == 0 => 0,
        "ms" => 1,
        "s" => 1000,
        "m" => 1000 * 60,
        "h" => 1000 * 60 * 60,
        "d" => 1000 * 60 * 60 * 24,
        _ => bail!(
            "invalid duration unit {} (expected one of 'ms', 's', 'm', 'h', or 'd')",
            unit
        ),
    };

    let ms = magnitude
        .checked_mul(mul)
        .ok_or_else(|| anyhow::anyhow!("Timeout value {} overflows when converted to 'ms'", s))?;
    Ok(time::Duration::from_millis(ms))
}
