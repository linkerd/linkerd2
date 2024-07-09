use crate::{
    ports::{ports_annotation, PortSet},
    routes::{ExplicitGKN, HttpRouteResource, ImpliedGKN},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, ensure, Result};
use linkerd_policy_controller_core::{
    outbound::{
        Backend, Backoff, FailureAccrual, OutboundPolicy, OutboundRoute, RouteSet, TrafficGroup,
        TrafficSubsetRef,
    },
    routes::{GroupKindNamespaceName, GrpcRouteMatch, HttpRouteMatch},
};
use linkerd_policy_controller_k8s_api::{
    gateway::{self as k8s_gateway_api, ParentReference},
    labels::Selector,
    policy as linkerd_k8s_api,
    traffic_group::{self as linkerd_k8s_multicluster},
    Labels, ResourceExt, Service,
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

mod grpc;
mod http;
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
    service_http_routes: HashMap<String, RouteSet<HttpRouteMatch>>,
    service_grpc_routes: HashMap<String, RouteSet<GrpcRouteMatch>>,
    /// Index all traffic groups in a namespace. Stores a handle to a traffic subset for each
    /// service. This applies for subsets that directly reference a service.
    traffic_groups: HashMap<ServiceRef, TrafficSubset>,
    namespace: Arc<String>,
}

#[derive(Debug, Default)]
struct ServiceInfo {
    opaque_ports: PortSet,
    accrual: Option<FailureAccrual>,
    labels: Labels,
}

#[derive(Clone, Debug, Default)]
struct TrafficSubset {
    subsets: Vec<ServiceRef>,
    strategy: Option<String>,
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
    traffic_group: TrafficGroup,
}

#[derive(Debug)]
struct RoutesWatch {
    opaque: bool,
    accrual: Option<FailureAccrual>,
    traffic_group: TrafficGroup,
    http_routes: RouteSet<HttpRouteMatch>,
    grpc_routes: RouteSet<GrpcRouteMatch>,
    watch: watch::Sender<OutboundPolicy>,
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_multicluster::TrafficGroup> for Index {
    fn apply(&mut self, group: linkerd_k8s_multicluster::TrafficGroup) {
        let name = group.name_unchecked();
        let ns = group
            .namespace()
            .expect("TrafficGroup must have a namespace");

        tracing::info!(%name, %ns, "index traffic group");
        // Parse label selector from each subset
        let selectors = group
            .spec
            .subsets
            .into_iter()
            .map(|subset| (subset.name, Selector::from_map(subset.labels)))
            .collect::<HashMap<String, Selector>>();
        let strategy = group.spec.strategy;
        // Find all backends that apply. NOTE: if we do this, it means whenever a new service is
        // added, we have to check if it matches a backend somewhere, i.e. 2 way relationship
        let mut backends = Vec::new();
        for (backend_name, selector) in &selectors {
            tracing::info!(%backend_name, "adding backend to subsets");
            // For now, take just one. We have to be careful since a bad label selector may select
            // more than one service.
            let svc_match = self
                .service_info
                .iter()
                .filter(|(svc_ref, _)| &svc_ref.namespace == &ns)
                .filter(|(_svc_ref, svc_info)| selector.matches(&svc_info.labels))
                .nth(0)
                .map(|(svc_ref, _svc_info)| svc_ref);
            if let Some(svc_ref) = svc_match {
                backends.push(svc_ref.clone());
            }
        }

        let traffic_subsets = TrafficSubset {
            subsets: backends,
            strategy,
        };

        let ns_entry = self
            .namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                namespace: Arc::new(ns.clone()),
                traffic_groups: HashMap::new(),
                service_port_routes: HashMap::new(),
                service_http_routes: Default::default(),
                service_grpc_routes: Default::default(),
            });

        // Index a TrafficGroup resource
        for parent_ref in group.spec.parent_refs {
            if !is_parent_service(&parent_ref) {
                continue;
            }
            // Force parentRef to be in the same namespace as the traffic group
            // for now.
            let svc_ref = ServiceRef {
                name: parent_ref.name,
                namespace: ns.clone(),
            };
            ns_entry
                .traffic_groups
                .insert(svc_ref.clone(), traffic_subsets.clone());
            if let Some(svc) = self.service_info.get(&svc_ref) {
                tracing::info!("triggering a service update");
                ns_entry.update_service(svc_ref.name.clone(), svc, &self.namespaces.cluster_info);
            }
        }

        tracing::info!("triggering a re-index");
        self.reindex_services()
    }

    fn delete(&mut self, _: String, _: String) {
        tracing::info!("delete not yet implemented for TrafficGroup");
    }
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

        let entry = self
            .namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_http_routes: Default::default(),
                service_grpc_routes: Default::default(),
                service_port_routes: Default::default(),
                traffic_groups: Default::default(),
                namespace: Arc::new(ns.clone()),
            });

        let service_info = ServiceInfo {
            opaque_ports,
            accrual,
            labels: Labels::from(service.labels().clone()),
        };

        tracing::info!("service {}/{} updated; triggering update", &name, &ns);
        entry.update_service(
            service.name_unchecked(),
            &service_info,
            &self.namespaces.cluster_info,
        );

        self.service_info.insert(
            ServiceRef {
                name: service.name_unchecked(),
                namespace: service.namespace().expect("Service must have Namespace"),
            },
            service_info,
        );

        self.reindex_services()
    }

    fn delete(&mut self, namespace: String, name: String) {
        tracing::debug!(name, namespace, "deleting service");
        let service_ref = ServiceRef { name, namespace };
        self.service_info.remove(&service_ref);
        self.services_by_ip.retain(|_, v| *v != service_ref);

        self.reindex_services()
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
                traffic_groups: Default::default(),
                service_http_routes: Default::default(),
                service_grpc_routes: Default::default(),
                service_port_routes: Default::default(),
            });

        let key = ServicePort {
            service: service_name,
            port: service_port,
        };

        tracing::debug!(?key, "subscribing to service port");

        //
        let routes =
            ns.service_routes_or_default(key, &self.namespaces.cluster_info, &self.service_info);

        let watch = routes.watch_for_ns_or_default(source_namespace);

        Ok(watch.watch.subscribe())
    }

    pub fn lookup_service(&self, addr: IpAddr) -> Option<ServiceRef> {
        self.services_by_ip.get(&addr).cloned()
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
                    traffic_groups: Default::default(),
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
                    traffic_groups: Default::default(),
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
        service_info: &HashMap<ServiceRef, ServiceInfo>,
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
        service_info: &HashMap<ServiceRef, ServiceInfo>,
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

    fn reindex_services(&mut self, service_info: &HashMap<ServiceRef, ServiceInfo>) {
        let update_service = |backend: &mut Backend| {
            if let Backend::Service(svc) = backend {
                let service_ref = ServiceRef {
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

    fn update_service(&mut self, name: String, service: &ServiceInfo, cluster_info: &ClusterInfo) {
        tracing::debug!(?name, ?service, "updating service");

        let svc_ref = ServiceRef {
            name: name.clone(),
            namespace: self.namespace.to_string(),
        };
        for (svc_port, svc_routes) in self.service_port_routes.iter_mut() {
            if svc_port.service != name {
                continue;
            }

            let opaque = service.opaque_ports.contains(&svc_port.port);

            // Construct a subset ref from each ServiceRef
            // ignore accrual for now
            let traffic_group = self
                .traffic_groups
                .get(&svc_ref)
                .map(|v| v.to_owned())
                .unwrap_or_default();
            let subsets = traffic_group
                .subsets
                .into_iter()
                .map(|svc_ref| TrafficSubsetRef {
                    name: svc_ref.name.clone(),
                    namespace: svc_ref.namespace.clone(),
                    authority: cluster_info.service_dns_authority(
                        &svc_ref.namespace,
                        &svc_ref.name,
                        svc_port.port,
                    ),
                    port: svc_port.port,
                    failure_accrual: None,
                })
                .collect::<Vec<_>>();
            tracing::info!(
                "updating service with {} new traffic subsets",
                subsets.len()
            );
            let traffic_group = TrafficGroup {
                subsets,
                strategy: traffic_group.strategy.clone(),
            };
            svc_routes.update_service(opaque, service.accrual, traffic_group);
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

                // Construct a subset ref from each ServiceRef
                // ignore accrual for now
                tracing::info!(
                    "establishing watch, got {} traffic groups indexed",
                    self.traffic_groups.len()
                );
                let traffic_subsets = self
                    .traffic_groups
                    .get(&service_ref)
                    .map(|v| v.to_owned())
                    .unwrap_or_default();
                let subsets = traffic_subsets
                    .subsets
                    .into_iter()
                    .map(|svc_ref| TrafficSubsetRef {
                        name: svc_ref.name.clone(),
                        namespace: svc_ref.namespace.clone(),
                        authority: cluster.service_dns_authority(
                            &svc_ref.namespace,
                            &svc_ref.name,
                            sp.port,
                        ),
                        port: sp.port,
                        failure_accrual: None,
                    })
                    .collect::<Vec<_>>();
                tracing::info!("establishing watch, got {} subsets", subsets.len());
                let traffic_group = TrafficGroup {
                    subsets,
                    strategy: traffic_subsets.strategy,
                };
                let mut service_routes = ServiceRoutes {
                    opaque,
                    accrual,
                    authority,
                    traffic_group,
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
                http_routes: http_routes.clone(),
                grpc_routes: grpc_routes.clone(),
                name: self.name.to_string(),
                authority: self.authority.clone(),
                namespace: self.namespace.to_string(),
                traffic_group: self.traffic_group.clone(),
            });

            RoutesWatch {
                http_routes,
                grpc_routes,
                watch: sender,
                opaque: self.opaque,
                accrual: self.accrual,
                traffic_group: self.traffic_group.clone(),
            }
        })
    }

    fn apply_http_route(
        &mut self,
        gknn: GroupKindNamespaceName,
        route: OutboundRoute<HttpRouteMatch>,
    ) {
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

    fn apply_grpc_route(
        &mut self,
        gknn: GroupKindNamespaceName,
        route: OutboundRoute<GrpcRouteMatch>,
    ) {
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
        traffic_group: TrafficGroup,
    ) {
        self.opaque = opaque;
        self.accrual = accrual;
        self.traffic_group = traffic_group.clone();
        for watch in self.watches_by_ns.values_mut() {
            watch.opaque = opaque;
            watch.accrual = accrual;
            watch.traffic_group = traffic_group.clone();
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

            if self.traffic_group != policy.traffic_group {
                policy.traffic_group = self.traffic_group.clone();
                modified = true;
            }

            modified
        });
    }

    fn insert_http_route(
        &mut self,
        gknn: GroupKindNamespaceName,
        route: OutboundRoute<HttpRouteMatch>,
    ) {
        self.http_routes.insert(gknn, route);

        self.send_if_modified();
    }

    fn insert_grpc_route(
        &mut self,
        gknn: GroupKindNamespaceName,
        route: OutboundRoute<GrpcRouteMatch>,
    ) {
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
