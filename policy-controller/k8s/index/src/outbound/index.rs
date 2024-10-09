use self::unmeshed_network::UnmeshedNetwork;
use crate::{
    ports::{ports_annotation, PortSet},
    routes::{ExplicitGKN, HttpRouteResource, ImpliedGKN},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, ensure, Result};
use linkerd_policy_controller_core::{
    outbound::{
        Backend, Backoff, FailureAccrual, GrpcRetryCondition, GrpcRoute, HttpRetryCondition,
        HttpRoute, OutboundPolicy, RouteRetry, RouteSet, RouteTimeouts,
    },
    routes::GroupKindNamespaceName,
};
use linkerd_policy_controller_k8s_api::{
    gateway::{self as k8s_gateway_api, ParentReference},
    policy as linkerd_k8s_api, Pod, ResourceExt, Service,
};
use parking_lot::RwLock;
use std::{hash::Hash, net::IpAddr, num::NonZeroU16, str::FromStr, sync::Arc, time};
use tokio::sync::watch;

pub mod grpc;
pub mod http;
pub mod metrics;
mod unmeshed_network;

#[derive(Debug)]
pub struct Index {
    namespaces: NamespaceIndex,
    services_by_ip: HashMap<IpAddr, ResourceRef>,
    service_info: HashMap<ResourceRef, ServiceInfo>,
    unmeshed_networks: HashMap<ResourceRef, UnmeshedNetwork>,
    pods_by_ip: HashMap<IpAddr, ResourceRef>,
}

pub type SharedIndex = Arc<RwLock<Index>>;

#[derive(Debug, Clone, Hash, PartialEq, Eq)]
pub struct ResourceRef {
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
    service_http_routes: HashMap<String, RouteSet<HttpRoute>>,
    service_grpc_routes: HashMap<String, RouteSet<GrpcRoute>>,
    namespace: Arc<String>,
}

#[derive(Debug, Default)]
struct ServiceInfo {
    opaque_ports: PortSet,
    accrual: Option<FailureAccrual>,
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
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
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
}

#[derive(Debug)]
struct RoutesWatch {
    opaque: bool,
    accrual: Option<FailureAccrual>,
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
    http_routes: RouteSet<HttpRoute>,
    grpc_routes: RouteSet<GrpcRoute>,
    watch: watch::Sender<OutboundPolicy>,
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::HttpRoute> for Index {
    fn apply(&mut self, route: linkerd_k8s_api::HttpRoute) {
        self.apply_http(HttpRouteResource::LinkerdHttp(route))
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name
            .gkn::<linkerd_k8s_api::HttpRoute>()
            .namespaced(namespace);
        tracing::debug!(?gknn, "deleting route");
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete_http_route(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::HttpRoute> for Index {
    fn apply(&mut self, route: k8s_gateway_api::HttpRoute) {
        self.apply_http(HttpRouteResource::GatewayHttp(route))
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name
            .gkn::<k8s_gateway_api::HttpRoute>()
            .namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete_http_route(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::GrpcRoute> for Index {
    fn apply(&mut self, route: k8s_gateway_api::GrpcRoute) {
        self.apply_grpc(route)
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name
            .gkn::<k8s_gateway_api::GrpcRoute>()
            .namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete_grpc_route(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<Service> for Index {
    fn apply(&mut self, service: Service) {
        let name = service.name_unchecked();
        let ns = service.namespace().expect("Service must have a namespace");
        tracing::debug!(name, ns, "indexing service");
        let accrual = parse_accrual_config(service.annotations())
            .map_err(|error| tracing::error!(%error, service=name, namespace=ns, "failed to parse accrual config"))
            .unwrap_or_default();
        let opaque_ports =
            ports_annotation(service.annotations(), "config.linkerd.io/opaque-ports")
                .unwrap_or_else(|| self.namespaces.cluster_info.default_opaque_ports.clone());

        let timeouts = parse_timeouts(service.annotations())
            .map_err(|error| tracing::error!(%error, service=name, namespace=ns, "failed to parse timeouts"))
            .unwrap_or_default();

        let http_retry = http::parse_http_retry(service.annotations()).map_err(|error| {
            tracing::error!(%error, service=name, namespace=ns, "failed to parse http retry")
        }).unwrap_or_default();
        let grpc_retry = grpc::parse_grpc_retry(service.annotations()).map_err(|error| {
            tracing::error!(%error, service=name, namespace=ns, "failed to parse grpc retry")
        }).unwrap_or_default();

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
                        let service_ref = ResourceRef {
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
            http_retry,
            grpc_retry,
            timeouts,
        };

        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_http_routes: Default::default(),
                service_grpc_routes: Default::default(),
                service_port_routes: Default::default(),
                namespace: Arc::new(ns),
            })
            .update_service(service.name_unchecked(), &service_info);

        self.service_info.insert(
            ResourceRef {
                name: service.name_unchecked(),
                namespace: service.namespace().expect("Service must have Namespace"),
            },
            service_info,
        );

        self.reindex_services()
    }

    fn delete(&mut self, namespace: String, name: String) {
        tracing::debug!(name, namespace, "deleting service");
        let service_ref = ResourceRef { name, namespace };
        self.service_info.remove(&service_ref);
        self.services_by_ip.retain(|_, v| *v != service_ref);

        self.reindex_services()
    }
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::UnmeshedNetwork> for Index {
    fn apply(&mut self, u: linkerd_k8s_api::UnmeshedNetwork) {
        let um: UnmeshedNetwork = u.into();
        tracing::debug!(um.name, um.namespace, "indexing unmeshed network");

        self.unmeshed_networks.insert(
            ResourceRef {
                name: um.name.clone(),
                namespace: um.namespace.clone(),
            },
            um,
        );
    }

    fn delete(&mut self, namespace: String, name: String) {
        tracing::debug!(name, namespace, "deleting unmeshed networks");
        let um_ref = ResourceRef { name, namespace };
        self.unmeshed_networks.remove(&um_ref);
    }
}

impl kubert::index::IndexNamespacedResource<Pod> for Index {
    fn apply(&mut self, pod: Pod) {
        let ns = pod.namespace().expect("Pod must have a namespace");
        let name = pod.name_unchecked();
        tracing::debug!(name, ns, "indexing pod");

        if let Some(ips) = pod.status.and_then(|s| s.pod_ips) {
            let pod_ref = ResourceRef {
                name,
                namespace: ns,
            };
            for ip in ips.iter() {
                if let Some(ip) = &ip.ip {
                    let pod_ip_addr = match IpAddr::from_str(ip) {
                        Ok(addr) => addr,
                        Err(error) => {
                            tracing::error!(%error, "malformed pod IP: {ip}");
                            continue;
                        }
                    };

                    self.pods_by_ip.insert(pod_ip_addr, pod_ref.clone());
                }
            }
        }
    }

    fn delete(&mut self, namespace: String, name: String) {
        tracing::debug!(name, namespace, "deleting pod");
        let pod_ref = ResourceRef { name, namespace };
        self.pods_by_ip.retain(|_, v| *v != pod_ref);
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
            unmeshed_networks: HashMap::default(),
            pods_by_ip: HashMap::default(),
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
                service_http_routes: Default::default(),
                service_grpc_routes: Default::default(),
                service_port_routes: Default::default(),
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

    pub fn lookup_service(&self, addr: IpAddr) -> Option<ResourceRef> {
        self.services_by_ip.get(&addr).cloned()
    }

    pub fn pod_exists(&self, addr: IpAddr) -> bool {
        self.pods_by_ip.contains_key(&addr)
    }

    pub fn lookup_unmeshed_network(
        &self,
        addr: IpAddr,
        source_namespace: String,
    ) -> Option<ResourceRef> {
        unmeshed_network::resolve_unmeshed_network(
            addr,
            source_namespace,
            self.unmeshed_networks.values(),
        )
    }

    fn apply_http(&mut self, route: HttpRouteResource) {
        tracing::debug!(name = route.name(), "indexing httproute");

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
                    service_http_routes: Default::default(),
                    service_grpc_routes: Default::default(),
                    service_port_routes: Default::default(),
                })
                .apply_http_route(
                    route.clone(),
                    parent_ref,
                    &self.namespaces.cluster_info,
                    &self.service_info,
                );
        }
    }

    fn apply_grpc(&mut self, route: k8s_gateway_api::GrpcRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing grpcroute");

        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            if !is_parent_service(parent_ref) {
                continue;
            }

            if !route_accepted_by_service(route.status.as_ref().map(|s| &s.inner), &parent_ref.name)
            {
                continue;
            }

            let ns = parent_ref
                .namespace
                .clone()
                .unwrap_or_else(|| route.namespace().expect("GrpcRoute must have a namespace"));

            self.namespaces
                .by_ns
                .entry(ns.clone())
                .or_insert_with(|| Namespace {
                    namespace: Arc::new(ns),
                    service_http_routes: Default::default(),
                    service_grpc_routes: Default::default(),
                    service_port_routes: Default::default(),
                })
                .apply_grpc_route(
                    route.clone(),
                    parent_ref,
                    &self.namespaces.cluster_info,
                    &self.service_info,
                );
        }
    }

    fn reindex_services(&mut self) {
        for ns in self.namespaces.by_ns.values_mut() {
            ns.reindex_services(&self.service_info);
        }
    }
}

impl Namespace {
    fn apply_http_route(
        &mut self,
        route: HttpRouteResource,
        parent_ref: &ParentReference,
        cluster_info: &ClusterInfo,
        service_info: &HashMap<ResourceRef, ServiceInfo>,
    ) {
        tracing::debug!(?route);

        let outbound_route =
            match http::convert_route(&self.namespace, route.clone(), cluster_info, service_info) {
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
                "inserting httproute for service"
            );

            let service_routes =
                self.service_routes_or_default(service_port, cluster_info, service_info);

            service_routes.apply_http_route(route.gknn(), outbound_route);
        } else {
            // If the parent_ref doesn't include a port, apply this route
            // to all ServiceRoutes which match the Service name.
            self.service_port_routes.iter_mut().for_each(
                |(ServicePort { service, port: _ }, routes)| {
                    if service == &parent_ref.name {
                        routes.apply_http_route(route.gknn(), outbound_route.clone());
                    }
                },
            );

            // Also add the route to the list of routes that target the
            // Service without specifying a port.
            self.service_http_routes
                .entry(parent_ref.name.clone())
                .or_default()
                .insert(route.gknn(), outbound_route);
        }
    }

    fn apply_grpc_route(
        &mut self,
        route: k8s_gateway_api::GrpcRoute,
        parent_ref: &ParentReference,
        cluster_info: &ClusterInfo,
        service_info: &HashMap<ResourceRef, ServiceInfo>,
    ) {
        tracing::debug!(?route);

        let outbound_route =
            match grpc::convert_route(&self.namespace, route.clone(), cluster_info, service_info) {
                Ok(route) => route,
                Err(error) => {
                    tracing::error!(%error, "failed to convert route");
                    return;
                }
            };

        tracing::debug!(?outbound_route);

        let gknn = route
            .gkn()
            .namespaced(route.namespace().expect("Route must have namespace"));

        let port = parent_ref.port.and_then(NonZeroU16::new);

        if let Some(port) = port {
            let service_port = ServicePort {
                port,
                service: parent_ref.name.clone(),
            };

            tracing::debug!(
                ?service_port,
                route = route.name_unchecked(),
                "inserting grpcroute for service"
            );

            let service_routes =
                self.service_routes_or_default(service_port, cluster_info, service_info);

            service_routes.apply_grpc_route(gknn, outbound_route);
        } else {
            // If the parent_ref doesn't include a port, apply this route
            // to all ServiceRoutes which match the Service name.
            self.service_port_routes.iter_mut().for_each(
                |(ServicePort { service, port: _ }, routes)| {
                    if service == &parent_ref.name {
                        routes.apply_grpc_route(gknn.clone(), outbound_route.clone());
                    }
                },
            );

            // Also add the route to the list of routes that target the
            // Service without specifying a port.
            self.service_grpc_routes
                .entry(parent_ref.name.clone())
                .or_default()
                .insert(gknn, outbound_route);
        }
    }

    fn reindex_services(&mut self, service_info: &HashMap<ResourceRef, ServiceInfo>) {
        let update_service = |backend: &mut Backend| {
            if let Backend::Service(svc) = backend {
                let service_ref = ResourceRef {
                    name: svc.name.clone(),
                    namespace: svc.namespace.clone(),
                };
                svc.exists = service_info.contains_key(&service_ref);
            }
        };

        for routes in self.service_port_routes.values_mut() {
            for watch in routes.watches_by_ns.values_mut() {
                let http_backends = watch
                    .http_routes
                    .values_mut()
                    .flat_map(|route| route.rules.iter_mut())
                    .flat_map(|rule| rule.backends.iter_mut());
                let grpc_backends = watch
                    .grpc_routes
                    .values_mut()
                    .flat_map(|route| route.rules.iter_mut())
                    .flat_map(|rule| rule.backends.iter_mut());

                http_backends.chain(grpc_backends).for_each(update_service);
                watch.send_if_modified();
            }
        }
    }

    fn update_service(&mut self, name: String, service: &ServiceInfo) {
        tracing::debug!(?name, ?service, "updating service");

        for (svc_port, svc_routes) in self.service_port_routes.iter_mut() {
            if svc_port.service != name {
                continue;
            }

            let opaque = service.opaque_ports.contains(&svc_port.port);

            svc_routes.update_service(
                opaque,
                service.accrual,
                service.http_retry.clone(),
                service.grpc_retry.clone(),
                service.timeouts.clone(),
            );
        }
    }

    fn delete_http_route(&mut self, gknn: &GroupKindNamespaceName) {
        for service in self.service_port_routes.values_mut() {
            service.delete_http_route(gknn);
        }

        self.service_http_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn delete_grpc_route(&mut self, gknn: &GroupKindNamespaceName) {
        for service in self.service_port_routes.values_mut() {
            service.delete_grpc_route(gknn);
        }

        self.service_grpc_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn service_routes_or_default(
        &mut self,
        sp: ServicePort,
        cluster: &ClusterInfo,
        service_info: &HashMap<ResourceRef, ServiceInfo>,
    ) -> &mut ServiceRoutes {
        self.service_port_routes
            .entry(sp.clone())
            .or_insert_with(|| {
                let authority =
                    cluster.service_dns_authority(&self.namespace, &sp.service, sp.port);

                let service_ref = ResourceRef {
                    name: sp.service.clone(),
                    namespace: self.namespace.to_string(),
                };

                let mut opaque = false;
                let mut accrual = None;
                let mut http_retry = None;
                let mut grpc_retry = None;
                let mut timeouts = Default::default();
                if let Some(svc) = service_info.get(&service_ref) {
                    opaque = svc.opaque_ports.contains(&sp.port);
                    accrual = svc.accrual;
                    http_retry = svc.http_retry.clone();
                    grpc_retry = svc.grpc_retry.clone();
                    timeouts = svc.timeouts.clone();
                }

                // The routes which target this Service but don't specify
                // a port apply to all ports. Therefore, we include them.
                let http_routes = self
                    .service_http_routes
                    .get(&sp.service)
                    .cloned()
                    .unwrap_or_default();
                let grpc_routes = self
                    .service_grpc_routes
                    .get(&sp.service)
                    .cloned()
                    .unwrap_or_default();

                let mut service_routes = ServiceRoutes {
                    opaque,
                    accrual,
                    http_retry,
                    grpc_retry,
                    timeouts,
                    authority,
                    port: sp.port,
                    name: sp.service,
                    namespace: self.namespace.clone(),
                    watches_by_ns: Default::default(),
                };

                // Producer routes are routes in the same namespace as
                // their parent service. Consumer routes are routes in
                // other namespaces.
                let (producer_http_routes, consumer_http_routes): (Vec<_>, Vec<_>) = http_routes
                    .into_iter()
                    .partition(|(gknn, _)| gknn.namespace == *self.namespace);
                let (producer_grpc_routes, consumer_grpc_routes): (Vec<_>, Vec<_>) = grpc_routes
                    .into_iter()
                    .partition(|(gknn, _)| gknn.namespace == *self.namespace);

                for (consumer_gknn, consumer_route) in consumer_http_routes {
                    // Consumer routes should only apply to watches from the
                    // consumer namespace.
                    let consumer_watch =
                        service_routes.watch_for_ns_or_default(consumer_gknn.namespace.to_string());

                    consumer_watch.insert_http_route(consumer_gknn.clone(), consumer_route.clone());
                }
                for (consumer_gknn, consumer_route) in consumer_grpc_routes {
                    // Consumer routes should only apply to watches from the
                    // consumer namespace.
                    let consumer_watch =
                        service_routes.watch_for_ns_or_default(consumer_gknn.namespace.to_string());

                    consumer_watch.insert_grpc_route(consumer_gknn.clone(), consumer_route.clone());
                }

                for (producer_gknn, producer_route) in producer_http_routes {
                    // Insert the route into the producer namespace.
                    let producer_watch =
                        service_routes.watch_for_ns_or_default(producer_gknn.namespace.to_string());

                    producer_watch.insert_http_route(producer_gknn.clone(), producer_route.clone());

                    // Producer routes apply to clients in all namespaces, so
                    // apply it to watches for all other namespaces too.
                    service_routes
                        .watches_by_ns
                        .iter_mut()
                        .filter(|(namespace, _)| {
                            namespace.as_str() != producer_gknn.namespace.as_ref()
                        })
                        .for_each(|(_, watch)| {
                            watch.insert_http_route(producer_gknn.clone(), producer_route.clone())
                        });
                }

                for (producer_gknn, producer_route) in producer_grpc_routes {
                    // Insert the route into the producer namespace.
                    let producer_watch =
                        service_routes.watch_for_ns_or_default(producer_gknn.namespace.to_string());

                    producer_watch.insert_grpc_route(producer_gknn.clone(), producer_route.clone());

                    // Producer routes apply to clients in all namespaces, so
                    // apply it to watches for all other namespaces too.
                    service_routes
                        .watches_by_ns
                        .iter_mut()
                        .filter(|(namespace, _)| {
                            namespace.as_str() != producer_gknn.namespace.as_ref()
                        })
                        .for_each(|(_, watch)| {
                            watch.insert_grpc_route(producer_gknn.clone(), producer_route.clone())
                        });
                }

                service_routes
            })
    }
}

#[inline]
fn is_service(group: Option<&str>, kind: &str) -> bool {
    // If the group is not specified or empty, assume it's 'core'.
    group
        .map(|g| g.eq_ignore_ascii_case("core") || g.is_empty())
        .unwrap_or(true)
        && kind.eq_ignore_ascii_case("Service")
}

#[inline]
fn is_unmeshed_network(group: Option<&str>, kind: &str) -> bool {
    // If the group is not specified or empty, assume it's 'core'.
    group
        .map(|g| g.eq_ignore_ascii_case("policy.linkerd.io"))
        .unwrap_or(false)
        && kind.eq_ignore_ascii_case("UnmeshedNetwork")
}

#[inline]
pub fn is_parent_service(parent: &ParentReference) -> bool {
    parent
        .kind
        .as_deref()
        .map(|k| is_service(parent.group.as_deref(), k))
        // Parent refs require a `kind`.
        .unwrap_or(false)
}

#[inline]
pub fn is_parent_unmeshed_network(parent: &ParentReference) -> bool {
    parent
        .kind
        .as_deref()
        .map(|k| is_unmeshed_network(parent.group.as_deref(), k))
        // Parent refs require a `kind`.
        .unwrap_or(false)
}

#[inline]
pub fn is_parent_service_or_unmeshed_network(parent: &ParentReference) -> bool {
    is_parent_service(parent) || is_parent_unmeshed_network(parent)
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

impl ServiceRoutes {
    fn watch_for_ns_or_default(&mut self, namespace: String) -> &mut RoutesWatch {
        // The routes from the producer namespace apply to watches in all
        // namespaces, so we copy them.
        let http_routes = self
            .watches_by_ns
            .get(self.namespace.as_ref())
            .map(|watch| watch.http_routes.clone())
            .unwrap_or_default();
        let grpc_routes = self
            .watches_by_ns
            .get(self.namespace.as_ref())
            .map(|watch| watch.grpc_routes.clone())
            .unwrap_or_default();

        self.watches_by_ns.entry(namespace).or_insert_with(|| {
            let (sender, _) = watch::channel(OutboundPolicy {
                port: self.port,
                opaque: self.opaque,
                accrual: self.accrual,
                http_retry: self.http_retry.clone(),
                grpc_retry: self.grpc_retry.clone(),
                timeouts: self.timeouts.clone(),
                http_routes: http_routes.clone(),
                grpc_routes: grpc_routes.clone(),
                name: self.name.to_string(),
                authority: self.authority.clone(),
                namespace: self.namespace.to_string(),
            });

            RoutesWatch {
                http_routes,
                grpc_routes,
                watch: sender,
                opaque: self.opaque,
                accrual: self.accrual,
                http_retry: self.http_retry.clone(),
                grpc_retry: self.grpc_retry.clone(),
                timeouts: self.timeouts.clone(),
            }
        })
    }

    fn apply_http_route(&mut self, gknn: GroupKindNamespaceName, route: HttpRoute) {
        if *gknn.namespace == *self.namespace {
            // This is a producer namespace route.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());

            watch.insert_http_route(gknn.clone(), route.clone());

            // Producer routes apply to clients in all namespaces, so
            // apply it to watches for all other namespaces too.
            for (ns, ns_watch) in self.watches_by_ns.iter_mut() {
                if ns != &gknn.namespace {
                    ns_watch.insert_http_route(gknn.clone(), route.clone());
                }
            }
        } else {
            // This is a consumer namespace route and should only apply to
            // watches from that namespace.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());
            watch.insert_http_route(gknn, route);
        }
    }

    fn apply_grpc_route(&mut self, gknn: GroupKindNamespaceName, route: GrpcRoute) {
        if *gknn.namespace == *self.namespace {
            // This is a producer namespace route.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());

            watch.insert_grpc_route(gknn.clone(), route.clone());

            // Producer routes apply to clients in all namespaces, so
            // apply it to watches for all other namespaces too.
            for (ns, ns_watch) in self.watches_by_ns.iter_mut() {
                if ns != &gknn.namespace {
                    ns_watch.insert_grpc_route(gknn.clone(), route.clone());
                }
            }
        } else {
            // This is a consumer namespace route and should only apply to
            // watches from that namespace.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());
            watch.insert_grpc_route(gknn, route);
        }
    }

    fn update_service(
        &mut self,
        opaque: bool,
        accrual: Option<FailureAccrual>,
        http_retry: Option<RouteRetry<HttpRetryCondition>>,
        grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
        timeouts: RouteTimeouts,
    ) {
        self.opaque = opaque;
        self.accrual = accrual;
        self.http_retry = http_retry.clone();
        self.grpc_retry = grpc_retry.clone();
        self.timeouts = timeouts.clone();
        for watch in self.watches_by_ns.values_mut() {
            watch.opaque = opaque;
            watch.accrual = accrual;
            watch.http_retry = http_retry.clone();
            watch.grpc_retry = grpc_retry.clone();
            watch.timeouts = timeouts.clone();
            watch.send_if_modified();
        }
    }

    fn delete_http_route(&mut self, gknn: &GroupKindNamespaceName) {
        for watch in self.watches_by_ns.values_mut() {
            watch.remove_http_route(gknn);
        }
    }

    fn delete_grpc_route(&mut self, gknn: &GroupKindNamespaceName) {
        for watch in self.watches_by_ns.values_mut() {
            watch.remove_grpc_route(gknn);
        }
    }
}

impl RoutesWatch {
    fn send_if_modified(&mut self) {
        self.watch.send_if_modified(|policy| {
            let mut modified = false;

            if self.http_routes != policy.http_routes {
                policy.http_routes = self.http_routes.clone();
                modified = true;
            }

            if self.grpc_routes != policy.grpc_routes {
                policy.grpc_routes = self.grpc_routes.clone();
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

            if self.http_retry != policy.http_retry {
                policy.http_retry = self.http_retry.clone();
                modified = true;
            }

            if self.grpc_retry != policy.grpc_retry {
                policy.grpc_retry = self.grpc_retry.clone();
                modified = true;
            }

            if self.timeouts != policy.timeouts {
                policy.timeouts = self.timeouts.clone();
                modified = true;
            }

            modified
        });
    }

    fn insert_http_route(&mut self, gknn: GroupKindNamespaceName, route: HttpRoute) {
        self.http_routes.insert(gknn, route);

        self.send_if_modified();
    }

    fn insert_grpc_route(&mut self, gknn: GroupKindNamespaceName, route: GrpcRoute) {
        self.grpc_routes.insert(gknn, route);

        self.send_if_modified();
    }

    fn remove_http_route(&mut self, gknn: &GroupKindNamespaceName) {
        self.http_routes.remove(gknn);
        self.send_if_modified();
    }

    fn remove_grpc_route(&mut self, gknn: &GroupKindNamespaceName) {
        self.grpc_routes.remove(gknn);
        self.send_if_modified();
    }
}

pub fn parse_accrual_config(
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

pub fn parse_timeouts(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<RouteTimeouts> {
    let response = annotations
        .get("timeout.linkerd.io/response")
        .map(|s| parse_duration(s))
        .transpose()?;
    let request = annotations
        .get("timeout.linkerd.io/request")
        .map(|s| parse_duration(s))
        .transpose()?;
    let idle = annotations
        .get("timeout.linkerd.io/idle")
        .map(|s| parse_duration(s))
        .transpose()?;
    Ok(RouteTimeouts {
        response,
        request,
        idle,
    })
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
