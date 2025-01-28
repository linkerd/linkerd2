use crate::{
    ports::ports_annotation,
    routes::{ExplicitGKN, HttpRouteResource, ImpliedGKN},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, ensure, Result};
use egress_network::EgressNetwork;
use kube::Resource;
use linkerd_policy_controller_core::{
    outbound::{
        Backend, Backoff, FailureAccrual, GrpcRetryCondition, GrpcRoute, HttpRetryCondition,
        HttpRoute, OutboundPolicy, Parent, ParentKind, ParentPort, ResourceTarget, RouteRetry,
        RouteSet, RouteTimeouts, TargetKind, TcpRoute, TlsRoute, TrafficPolicy,
    },
    routes::GroupKindNamespaceName,
};
use linkerd_policy_controller_k8s_api::{
    gateway::{self as k8s_gateway_api, ParentReference},
    policy::{self as linkerd_k8s_api, Cidr},
    ResourceExt, Service,
};
use parking_lot::RwLock;
use std::{hash::Hash, net::IpAddr, num::NonZeroU16, sync::Arc, time};
use tokio::sync::watch;

#[derive(Debug)]
pub struct Index {
    namespaces: NamespaceIndex,
    services_by_ip: HashMap<IpAddr, ParentRef>,
    egress_networks_by_ref: HashMap<ParentRef, EgressNetwork>,
    // holds information about resources. currently EgressNetworks and Services
    parents_by_ref: HashMap<ParentRef, ParentState>,

    cluster_networks: Vec<linkerd_k8s_api::Cidr>,
    global_egress_network_namespace: Arc<String>,

    // holds a no-op sender to which all clients that have been returned
    // a Fallback policy are subsribed. It is used to force these clients
    // to reconnect an obtain new policy once the current one may no longer
    // be valid
    fallback_polcy_tx: watch::Sender<()>,
}

pub mod egress_network;
pub mod grpc;
pub mod http;
pub mod metrics;
pub mod tcp;
pub(crate) mod tls;

pub type SharedIndex = Arc<RwLock<Index>>;

#[derive(Debug, Clone, Hash, PartialEq, Eq)]
pub enum ParentFlavor {
    EgressNetwork,
    Service,
}

#[derive(Debug, Clone, Hash, PartialEq, Eq)]
pub struct ParentRef {
    pub kind: ParentFlavor,
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
    /// Stores an observable handle for each known resource:port,
    /// as well as any route resources in the cluster that specify
    /// a port.
    routes_by_parent_port: HashMap<ParentPortKey, Routes>,
    /// Stores the route resources (by service name) that do not
    /// explicitly target a port. These are only valid for Service
    /// as EgressNetworks cannot be parents without an explicit
    /// port declaration
    service_http_routes: HashMap<String, RouteSet<HttpRoute>>,
    service_grpc_routes: HashMap<String, RouteSet<GrpcRoute>>,
    service_tls_routes: HashMap<String, RouteSet<TlsRoute>>,
    service_tcp_routes: HashMap<String, RouteSet<TcpRoute>>,
    namespace: Arc<String>,
}

#[derive(Debug)]
struct ParentState {
    ports: HashMap<NonZeroU16, ParentPortSpec>,
    accrual: Option<FailureAccrual>,
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
    traffic_policy: Option<TrafficPolicy>,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
struct ParentPortKey {
    kind: ParentFlavor,
    parent_name: String,
    port: NonZeroU16,
}

#[derive(Debug)]
#[allow(dead_code)]
struct ParentPortSpec {
    number: NonZeroU16,
    name: Option<String>,
    app_protocol: Option<String>,
}

#[derive(Debug)]
struct Routes {
    parent: Parent,
    namespace: Arc<String>,
    watches_by_ns: HashMap<String, RoutesWatch>,
    accrual: Option<FailureAccrual>,
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
    allow_l5d_request_headers: bool,
}

#[derive(Debug)]
struct RoutesWatch {
    parent: Parent,
    accrual: Option<FailureAccrual>,
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
    http_routes: RouteSet<HttpRoute>,
    grpc_routes: RouteSet<GrpcRoute>,
    tls_routes: RouteSet<TlsRoute>,
    tcp_routes: RouteSet<TcpRoute>,
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

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::TlsRoute> for Index {
    fn apply(&mut self, route: k8s_gateway_api::TlsRoute) {
        self.apply_tls(route)
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name
            .gkn::<k8s_gateway_api::TlsRoute>()
            .namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete_tls_route(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::TcpRoute> for Index {
    fn apply(&mut self, route: k8s_gateway_api::TcpRoute) {
        self.apply_tcp(route)
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name
            .gkn::<k8s_gateway_api::TcpRoute>()
            .namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete_tcp_route(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<Service> for Index {
    fn apply(&mut self, service: Service) {
        let name = service.name_unchecked();
        let ns = service.namespace().expect("Service must have a namespace");
        tracing::debug!(name, ns, "indexing service");
        let accrual = parse_accrual_config(service.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse accrual config"))
            .unwrap_or_default();

        let timeouts = parse_timeouts(service.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse timeouts"))
            .unwrap_or_default();

        let http_retry = http::parse_http_retry(service.annotations()).map_err(|error| {
            tracing::warn!(%error, service=name, namespace=ns, "Failed to parse http retry")
        }).unwrap_or_default();
        let grpc_retry = grpc::parse_grpc_retry(service.annotations()).map_err(|error| {
            tracing::warn!(%error, service=name, namespace=ns, "Failed to parse grpc retry")
        }).unwrap_or_default();

        for cluster_ip in service
            .spec
            .as_ref()
            .iter()
            .flat_map(|spec| spec.cluster_ips.iter().flatten())
        {
            if cluster_ip.eq_ignore_ascii_case("None") {
                continue;
            }
            let addr = match cluster_ip.parse() {
                Ok(addr) => addr,
                Err(error) => {
                    tracing::warn!(%error, service=name, cluster_ip, "Invalid cluster ip");
                    continue;
                }
            };
            let parent = ParentRef {
                kind: ParentFlavor::Service,
                name: name.clone(),
                namespace: ns.clone(),
            };
            self.services_by_ip.insert(addr, parent);
        }

        let opaque_ports =
            ports_annotation(service.annotations(), "config.linkerd.io/opaque-ports")
                .unwrap_or_else(|| self.namespaces.cluster_info.default_opaque_ports.clone());
        let mut ports = HashMap::new();
        // TODO(ver): Enumerate service ports, tracking app protocols.
        /* for sp in service.spec.iter().flat_map(|s| s.ports.iter().flatten()) {
            if !matches!(sp.protocol.as_deref(), Some("TCP") | None) {
                continue;
            }
            let Some(number) = u16::try_from(sp.port).ok().and_then(NonZeroU16::new) else {
                continue;
            };
            let app_protocol = sp.app_protocol.clone().or_else(|| {
                if opaque_ports.contains(&number) {
                    return Some("linkerd.io/opaque".to_string());
                }
                None
            });
            let spec = PortSpec {
                number,
                name: sp.name.clone(),
                app_protocol,
            };
            ports.insert(number, spec);
        } */
        for opaque_port in opaque_ports {
            if ports.contains_key(&opaque_port) {
                continue;
            }
            let spec = ParentPortSpec {
                number: opaque_port,
                name: None,
                // We specify an app protocol that is not in the 'known' set
                // so that the traffic is handled opaquely.
                app_protocol: Some("linkerd.io/opaque".to_string()),
            };
            ports.insert(opaque_port, spec);
        }

        let parent = ParentState {
            ports,
            accrual,
            http_retry,
            grpc_retry,
            timeouts,
            traffic_policy: None,
        };

        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace::new(ns.clone()))
            .update_resource(service.name_unchecked(), ParentFlavor::Service, &parent);

        let key = ParentRef {
            kind: ParentFlavor::Service,
            name: service.name_unchecked(),
            namespace: service.namespace().expect("Service must have Namespace"),
        };
        self.parents_by_ref.insert(key, parent);

        self.reindex_resources();
    }

    fn delete(&mut self, namespace: String, name: String) {
        tracing::debug!(name, namespace, "deleting service");
        let service_ref = ParentRef {
            kind: ParentFlavor::Service,
            name,
            namespace,
        };
        self.parents_by_ref.remove(&service_ref);
        self.services_by_ip.retain(|_, v| *v != service_ref);

        self.reindex_resources();
    }
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::EgressNetwork> for Index {
    fn apply(&mut self, egress_network: linkerd_k8s_api::EgressNetwork) {
        let name = egress_network.name_unchecked();
        let ns = egress_network
            .namespace()
            .expect("EgressNetwork must have a namespace");
        tracing::debug!(name, ns, "indexing EgressNetwork");
        let accrual = parse_accrual_config(egress_network.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse accrual config"))
            .unwrap_or_default();

        let timeouts = parse_timeouts(egress_network.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse timeouts"))
            .unwrap_or_default();

        let http_retry = http::parse_http_retry(egress_network.annotations()).map_err(|error| {
            tracing::warn!(%error, service=name, namespace=ns, "Failed to parse http retry")
        }).unwrap_or_default();
        let grpc_retry = grpc::parse_grpc_retry(egress_network.annotations()).map_err(|error| {
            tracing::warn!(%error, service=name, namespace=ns, "Failed to parse grpc retry")
        }).unwrap_or_default();

        let egress_net_ref = ParentRef {
            kind: ParentFlavor::EgressNetwork,
            name: name.clone(),
            namespace: ns.clone(),
        };

        let egress_net =
            EgressNetwork::from_resource(&egress_network, self.cluster_networks.clone());

        let traffic_policy = Some(match egress_net.traffic_policy {
            linkerd_k8s_api::TrafficPolicy::Allow => TrafficPolicy::Allow,
            linkerd_k8s_api::TrafficPolicy::Deny => TrafficPolicy::Deny,
        });

        self.egress_networks_by_ref
            .insert(egress_net_ref.clone(), egress_net);

        let opaque_ports = ports_annotation(
            egress_network.annotations(),
            "config.linkerd.io/opaque-ports",
        )
        .unwrap_or_else(|| self.namespaces.cluster_info.default_opaque_ports.clone());
        let mut ports = HashMap::new();
        for port in opaque_ports {
            if !ports.contains_key(&port) {
                ports.insert(
                    port,
                    ParentPortSpec {
                        number: port,
                        name: None,
                        app_protocol: Some("linkerd.io/opaque".to_string()),
                    },
                );
            }
        }

        let egress_network_info = ParentState {
            ports,
            accrual,
            http_retry,
            grpc_retry,
            timeouts,
            traffic_policy,
        };

        let ns = Arc::new(ns);
        self.namespaces
            .by_ns
            .entry(ns.to_string())
            .or_insert_with(|| Namespace::new(ns.clone()))
            .update_resource(
                egress_network.name_unchecked(),
                ParentFlavor::EgressNetwork,
                &egress_network_info,
            );

        self.parents_by_ref
            .insert(egress_net_ref, egress_network_info);

        self.reindex_resources();
        self.reinitialize_egress_watches(&ns);
        self.reinitialize_fallback_watches()
    }

    fn delete(&mut self, namespace: String, name: String) {
        tracing::debug!(name, namespace, "deleting EgressNetwork");
        let egress_net_ref = ParentRef {
            kind: ParentFlavor::EgressNetwork,
            name,
            namespace,
        };
        self.egress_networks_by_ref.remove(&egress_net_ref);

        self.reindex_resources();
        self.reinitialize_egress_watches(&egress_net_ref.namespace);
        self.reinitialize_fallback_watches()
    }
}

impl Index {
    pub fn shared(cluster_info: Arc<ClusterInfo>) -> SharedIndex {
        let cluster_networks = cluster_info.networks.clone();
        let global_egress_network_namespace = cluster_info.global_egress_network_namespace.clone();

        let (fallback_polcy_tx, _) = watch::channel(());
        Arc::new(RwLock::new(Self {
            namespaces: NamespaceIndex {
                by_ns: HashMap::default(),
                cluster_info,
            },
            services_by_ip: HashMap::default(),
            egress_networks_by_ref: HashMap::default(),
            parents_by_ref: HashMap::default(),
            cluster_networks: cluster_networks.into_iter().map(Cidr::from).collect(),
            fallback_polcy_tx,
            global_egress_network_namespace,
        }))
    }

    pub fn is_address_in_cluster(&self, addr: IpAddr) -> bool {
        self.cluster_networks
            .iter()
            .any(|net| net.contains(&addr.into()))
    }

    pub fn fallback_policy_rx(&self) -> watch::Receiver<()> {
        self.fallback_polcy_tx.subscribe()
    }

    fn reinitialize_fallback_watches(&mut self) {
        let (new_fallback_tx, _) = watch::channel(());
        self.fallback_polcy_tx = new_fallback_tx;
    }

    pub fn outbound_policy_rx(
        &mut self,
        target: ResourceTarget,
    ) -> Result<watch::Receiver<OutboundPolicy>> {
        let ResourceTarget {
            name,
            namespace,
            port,
            source_namespace,
            kind,
        } = target;

        let kind = match kind {
            TargetKind::EgressNetwork { .. } => ParentFlavor::EgressNetwork,
            TargetKind::Service { .. } => ParentFlavor::Service,
        };

        let ns = self
            .namespaces
            .by_ns
            .entry(namespace.clone())
            .or_insert_with(|| Namespace::new(namespace.clone()));

        let port = ParentPortKey {
            kind,
            parent_name: name,
            port,
        };
        tracing::debug!(?port, "Subscribing");
        let rx = ns
            .resource_routes_or_default(port, &self.namespaces.cluster_info, &self.parents_by_ref)
            .watch_for_ns_or_default(source_namespace)
            .watch
            .subscribe();
        Ok(rx)
    }

    pub fn lookup_service(&self, addr: IpAddr) -> Option<(String, String)> {
        self.services_by_ip
            .get(&addr)
            .cloned()
            .map(|r| (r.namespace, r.name))
    }

    pub fn lookup_egress_network(
        &self,
        addr: IpAddr,
        source_namespace: String,
    ) -> Option<(String, String)> {
        egress_network::resolve_egress_network(
            addr,
            source_namespace,
            &self.global_egress_network_namespace,
            self.egress_networks_by_ref.values(),
        )
        .map(|r| (r.namespace, r.name))
    }

    fn apply_http(&mut self, route: HttpRouteResource) {
        tracing::debug!(name = route.name(), "indexing httproute");

        // For each parent_ref, create a namespace index for it if it doesn't
        // already exist.
        for parent_ref in route.inner().parent_refs.iter().flatten() {
            let ns = parent_ref
                .namespace
                .clone()
                .unwrap_or_else(|| route.namespace());

            self.namespaces
                .by_ns
                .entry(ns.clone())
                .or_insert_with(|| Namespace::new(ns));
        }

        // We must send the route update to all namespace indexes in case this
        // route's parent_refs have changed and this route must be removed by
        // any of them.
        self.namespaces.by_ns.values_mut().for_each(|ns| {
            ns.apply_http_route(
                route.clone(),
                &self.namespaces.cluster_info,
                &self.parents_by_ref,
            );
        });
    }

    fn apply_grpc(&mut self, route: k8s_gateway_api::GrpcRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing grpcroute");

        // For each parent_ref, create a namespace index for it if it doesn't
        // already exist.
        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            let ns = parent_ref
                .namespace
                .clone()
                .unwrap_or_else(|| route.namespace().expect("GrpcRoute must have a namespace"));

            self.namespaces
                .by_ns
                .entry(ns.clone())
                .or_insert_with(|| Namespace::new(ns));
        }

        // We must send the route update to all namespace indexes in case this
        // route's parent_refs have changed and this route must be removed by
        // any of them.
        for ns in self.namespaces.by_ns.values_mut() {
            ns.apply_grpc_route(
                route.clone(),
                &self.namespaces.cluster_info,
                &self.parents_by_ref,
            );
        }
    }

    fn apply_tls(&mut self, route: k8s_gateway_api::TlsRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing tlsroute");

        // For each parent_ref, create a namespace index for it if it doesn't
        // already exist.
        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            let ns = parent_ref
                .namespace
                .clone()
                .unwrap_or_else(|| route.namespace().expect("TlsRoute must have a namespace"));

            self.namespaces
                .by_ns
                .entry(ns.clone())
                .or_insert_with(|| Namespace::new(ns));
        }

        // We must send the route update to all namespace indexes in case this
        // route's parent_refs have changed and this route must be removed by
        // any of them.
        for ns in self.namespaces.by_ns.values_mut() {
            ns.apply_tls_route(
                route.clone(),
                &self.namespaces.cluster_info,
                &self.parents_by_ref,
            );
        }
    }

    fn apply_tcp(&mut self, route: k8s_gateway_api::TcpRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing tcproute");

        // For each parent_ref, create a namespace index for it if it doesn't
        // already exist.
        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            let ns = parent_ref
                .namespace
                .clone()
                .unwrap_or_else(|| route.namespace().expect("TcpRoute must have a namespace"));

            self.namespaces
                .by_ns
                .entry(ns.clone())
                .or_insert_with(|| Namespace::new(ns));
        }

        // We must send the route update to all namespace indexes in case this
        // route's parent_refs have changed and this route must be removed by
        // any of them.
        for ns in self.namespaces.by_ns.values_mut() {
            ns.apply_tcp_route(
                route.clone(),
                &self.namespaces.cluster_info,
                &self.parents_by_ref,
            );
        }
    }

    fn reindex_resources(&mut self) {
        for ns in self.namespaces.by_ns.values_mut() {
            ns.reindex_resources(&self.parents_by_ref);
        }
    }

    fn reinitialize_egress_watches(&mut self, namespace: &str) {
        for ns in self.namespaces.by_ns.values_mut() {
            if namespace == *self.global_egress_network_namespace || namespace == *ns.namespace {
                ns.reinitialize_egress_watches()
            }
        }
    }
}

impl Namespace {
    fn new(ns: impl Into<Arc<String>>) -> Self {
        Self {
            namespace: ns.into(),
            service_http_routes: Default::default(),
            service_grpc_routes: Default::default(),
            service_tls_routes: Default::default(),
            service_tcp_routes: Default::default(),
            routes_by_parent_port: Default::default(),
        }
    }

    fn apply_http_route(
        &mut self,
        route: HttpRouteResource,
        cluster_info: &ClusterInfo,
        resource_info: &HashMap<ParentRef, ParentState>,
    ) {
        tracing::debug!(?route);

        let outbound_route = match http::convert_route(
            &self.namespace,
            route.clone(),
            cluster_info,
            resource_info,
        ) {
            Ok(route) => route,
            Err(error) => {
                tracing::warn!(%error, "Failed to convert route");
                return;
            }
        };

        tracing::debug!(?outbound_route);

        for parent_ref in route.inner().parent_refs.iter().flatten() {
            let parent_kind = if is_parent_service(parent_ref) {
                ParentFlavor::Service
            } else if is_parent_egress_network(parent_ref) {
                ParentFlavor::EgressNetwork
            } else {
                continue;
            };
            let route_namespace = route.namespace();
            let parent_namespace = parent_ref.namespace.as_ref().unwrap_or(&route_namespace);
            if *parent_namespace != *self.namespace {
                continue;
            }

            if let Some(port) = parent_ref.port.and_then(NonZeroU16::new) {
                let port = ParentPortKey {
                    port,
                    kind: parent_kind,
                    parent_name: parent_ref.name.clone(),
                };
                if route_accepted_by_resource_port(route.status(), &port) {
                    tracing::debug!(?port, route = route.name(), "Attaching HTTPRoute");
                    self.resource_routes_or_default(port, cluster_info, resource_info)
                        .apply_http_route(route.gknn(), outbound_route.clone());
                }
            } else if route_accepted_by_service(route.status(), &parent_ref.name) {
                // If the parent_ref doesn't include a port, apply this route
                // to all ResourceRoutes which match the resource name.
                for (
                    ParentPortKey {
                        parent_name: name, ..
                    },
                    routes,
                ) in self.routes_by_parent_port.iter_mut()
                {
                    if name == &parent_ref.name {
                        tracing::debug!(route = route.name(), "Attaching HTTPRoute");
                        routes.apply_http_route(route.gknn(), outbound_route.clone());
                    }
                }
                // Also add the route to the list of routes that target the
                // resource without specifying a port.
                self.service_http_routes
                    .entry(parent_ref.name.clone())
                    .or_default()
                    .insert(route.gknn(), outbound_route.clone());
            }
        }

        // Remove the route from all parents that are not in the route's parent_refs.
        for (resource_port, resource_routes) in self.routes_by_parent_port.iter_mut() {
            if !route_accepted_by_resource_port(route.status(), resource_port) {
                resource_routes.delete_http_route(&route.gknn());
            }
        }
        for (parent_name, routes) in self.service_http_routes.iter_mut() {
            if !route_accepted_by_service(route.status(), parent_name) {
                routes.remove(&route.gknn());
            }
        }
    }

    fn apply_grpc_route(
        &mut self,
        route: k8s_gateway_api::GrpcRoute,
        cluster_info: &ClusterInfo,
        resource_info: &HashMap<ParentRef, ParentState>,
    ) {
        tracing::debug!(?route);
        let outbound_route = match grpc::convert_route(
            &self.namespace,
            route.clone(),
            cluster_info,
            resource_info,
        ) {
            Ok(route) => route,
            Err(error) => {
                tracing::warn!(%error, "Failed to convert route");
                return;
            }
        };
        let gknn = route
            .gkn()
            .namespaced(route.namespace().expect("Route must have namespace"));
        let status = route.status.as_ref().map(|s| &s.inner);

        tracing::debug!(?outbound_route);

        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            let parent_kind = if is_parent_service(parent_ref) {
                ParentFlavor::Service
            } else if is_parent_egress_network(parent_ref) {
                ParentFlavor::EgressNetwork
            } else {
                continue;
            };
            let route_namespace = route.namespace().expect("GrpcRoute must have a namespace");
            let parent_namespace = parent_ref.namespace.as_ref().unwrap_or(&route_namespace);
            if *parent_namespace != *self.namespace {
                continue;
            }

            let port = parent_ref.port.and_then(NonZeroU16::new);

            if let Some(port) = port {
                let port = ParentPortKey {
                    kind: parent_kind,
                    port,
                    parent_name: parent_ref.name.clone(),
                };

                if !route_accepted_by_resource_port(status, &port) {
                    continue;
                }

                tracing::debug!(
                    ?port,
                    route = route.name_unchecked(),
                    "Attaching grpcroute for resource"
                );

                let service_routes =
                    self.resource_routes_or_default(port, cluster_info, resource_info);

                service_routes.apply_grpc_route(gknn.clone(), outbound_route.clone());
            } else {
                if !route_accepted_by_service(status, &parent_ref.name) {
                    continue;
                }
                // If the parent_ref doesn't include a port, apply this route
                // to all ResourceRoutes which match the resource name.
                self.routes_by_parent_port.iter_mut().for_each(
                    |(
                        ParentPortKey {
                            parent_name: name,
                            port: _,
                            ..
                        },
                        routes,
                    )| {
                        if name == &parent_ref.name {
                            routes.apply_grpc_route(gknn.clone(), outbound_route.clone());
                        }
                    },
                );

                // Also add the route to the list of routes that target the
                // resource without specifying a port.
                self.service_grpc_routes
                    .entry(parent_ref.name.clone())
                    .or_default()
                    .insert(gknn.clone(), outbound_route.clone());
            }
        }

        // Remove the route from all parents that are not in the route's parent_refs.
        for (resource_port, resource_routes) in self.routes_by_parent_port.iter_mut() {
            if !route_accepted_by_resource_port(status, resource_port) {
                resource_routes.delete_grpc_route(&gknn);
            }
        }
        for (parent_name, routes) in self.service_grpc_routes.iter_mut() {
            if !route_accepted_by_service(status, parent_name) {
                routes.remove(&gknn);
            }
        }
    }

    fn apply_tls_route(
        &mut self,
        route: k8s_gateway_api::TlsRoute,
        cluster_info: &ClusterInfo,
        resource_info: &HashMap<ParentRef, ParentState>,
    ) {
        tracing::debug!(?route);
        let outbound_route =
            match tls::convert_route(&self.namespace, route.clone(), cluster_info, resource_info) {
                Ok(route) => route,
                Err(error) => {
                    tracing::warn!(%error, "Failed to convert route");
                    return;
                }
            };

        tracing::debug!(?outbound_route);

        let gknn = route
            .gkn()
            .namespaced(route.namespace().expect("Route must have namespace"));
        let status = route.status.as_ref().map(|s| &s.inner);

        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            let parent_kind = if is_parent_service(parent_ref) {
                ParentFlavor::Service
            } else if is_parent_egress_network(parent_ref) {
                ParentFlavor::EgressNetwork
            } else {
                continue;
            };
            let route_namespace = route.namespace().expect("GrpcRoute must have a namespace");
            let parent_namespace = parent_ref.namespace.as_ref().unwrap_or(&route_namespace);
            if *parent_namespace != *self.namespace {
                continue;
            }

            let port = parent_ref.port.and_then(NonZeroU16::new);

            if let Some(port) = port {
                let port = ParentPortKey {
                    kind: parent_kind,
                    port,
                    parent_name: parent_ref.name.clone(),
                };

                if !route_accepted_by_resource_port(status, &port) {
                    continue;
                }

                tracing::debug!(
                    ?port,
                    route = route.name_unchecked(),
                    "Attaching tlsroute for resource"
                );

                let resource_routes =
                    self.resource_routes_or_default(port, cluster_info, resource_info);

                resource_routes.apply_tls_route(gknn.clone(), outbound_route.clone());
            } else {
                if !route_accepted_by_service(status, &parent_ref.name) {
                    continue;
                }
                // If the parent_ref doesn't include a port, apply this route
                // to all ResourceRoutes which match the resource name.
                self.routes_by_parent_port.iter_mut().for_each(
                    |(
                        ParentPortKey {
                            parent_name: name,
                            port: _,
                            ..
                        },
                        routes,
                    )| {
                        if name == &parent_ref.name {
                            routes.apply_tls_route(gknn.clone(), outbound_route.clone());
                        }
                    },
                );

                // Also add the route to the list of routes that target the
                // resource without specifying a port.
                self.service_tls_routes
                    .entry(parent_ref.name.clone())
                    .or_default()
                    .insert(gknn.clone(), outbound_route.clone());
            }
        }

        // Remove the route from all parents that are not in the route's parent_refs.
        for (resource_port, resource_routes) in self.routes_by_parent_port.iter_mut() {
            if !route_accepted_by_resource_port(status, resource_port) {
                resource_routes.delete_tls_route(&gknn);
            }
        }
        for (parent_name, routes) in self.service_tls_routes.iter_mut() {
            if !route_accepted_by_service(status, parent_name) {
                routes.remove(&gknn);
            }
        }
    }

    fn apply_tcp_route(
        &mut self,
        route: k8s_gateway_api::TcpRoute,
        cluster_info: &ClusterInfo,
        resource_info: &HashMap<ParentRef, ParentState>,
    ) {
        tracing::debug!(?route);
        let outbound_route =
            match tcp::convert_route(&self.namespace, route.clone(), cluster_info, resource_info) {
                Ok(route) => route,
                Err(error) => {
                    tracing::warn!(%error, "Failed to convert route");
                    return;
                }
            };

        tracing::debug!(?outbound_route);

        let gknn = route
            .gkn()
            .namespaced(route.namespace().expect("Route must have namespace"));
        let status = route.status.as_ref().map(|s| &s.inner);

        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            let parent_kind = if is_parent_service(parent_ref) {
                ParentFlavor::Service
            } else if is_parent_egress_network(parent_ref) {
                ParentFlavor::EgressNetwork
            } else {
                continue;
            };
            let route_namespace = route.namespace().expect("GrpcRoute must have a namespace");
            let parent_namespace = parent_ref.namespace.as_ref().unwrap_or(&route_namespace);
            if *parent_namespace != *self.namespace {
                continue;
            }

            let port = parent_ref.port.and_then(NonZeroU16::new);

            if let Some(port) = port {
                let port = ParentPortKey {
                    kind: parent_kind,
                    port,
                    parent_name: parent_ref.name.clone(),
                };
                if route_accepted_by_resource_port(status, &port) {
                    tracing::debug!(
                        parent = %port.parent_name,
                        port = %port.port,
                        route = route.name_unchecked(),
                        "Attaching TCPRoute"
                    );
                    self.resource_routes_or_default(port, cluster_info, resource_info)
                        .apply_tcp_route(gknn.clone(), outbound_route.clone());
                }
            } else if route_accepted_by_service(status, &parent_ref.name) {
                // If the parent_ref doesn't include a port, apply this route
                // to all ResourceRoutes which match the resource name.
                self.routes_by_parent_port
                    .iter_mut()
                    .for_each(|(port, routes)| {
                        if port.parent_name == parent_ref.name {
                            tracing::debug!(
                                parent = %port.parent_name,
                                route = route.name_unchecked(),
                                "Attaching TCPRoute"
                            );
                            routes.apply_tcp_route(gknn.clone(), outbound_route.clone());
                        }
                    });

                // Also add the route to the list of routes that target the
                // resource without specifying a port.
                self.service_tcp_routes
                    .entry(parent_ref.name.clone())
                    .or_default()
                    .insert(gknn.clone(), outbound_route.clone());
            }
        }

        // Remove the route from all parents that are not in the route's parent_refs.
        for (resource_port, resource_routes) in self.routes_by_parent_port.iter_mut() {
            if !route_accepted_by_resource_port(status, resource_port) {
                resource_routes.delete_tcp_route(&gknn);
            }
        }
        for (parent_name, routes) in self.service_tcp_routes.iter_mut() {
            if !route_accepted_by_service(status, parent_name) {
                routes.remove(&gknn);
            }
        }
    }

    fn reindex_resources(&mut self, resource_info: &HashMap<ParentRef, ParentState>) {
        let update_backend = |backend: &mut Backend| {
            match backend {
                Backend::Service(svc) => {
                    let service_ref = ParentRef {
                        kind: ParentFlavor::Service,
                        name: svc.name.clone(),
                        namespace: svc.namespace.clone(),
                    };
                    svc.exists = resource_info.contains_key(&service_ref);
                }
                Backend::EgressNetwork(egress_net) => {
                    let egress_net_ref = ParentRef {
                        kind: ParentFlavor::EgressNetwork,
                        name: egress_net.name.clone(),
                        namespace: egress_net.namespace.clone(),
                    };
                    egress_net.exists = resource_info.contains_key(&egress_net_ref);
                }

                _ => {}
            };
        };

        for routes in self.routes_by_parent_port.values_mut() {
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
                let tls_backends = watch
                    .tls_routes
                    .values_mut()
                    .flat_map(|route| route.rule.backends.iter_mut());
                let tcp_backends = watch
                    .tcp_routes
                    .values_mut()
                    .flat_map(|route| route.rule.backends.iter_mut());

                http_backends
                    .chain(grpc_backends)
                    .chain(tls_backends)
                    .chain(tcp_backends)
                    .for_each(update_backend);

                watch.send_if_modified();
            }
        }
    }

    fn reinitialize_egress_watches(&mut self) {
        for routes in self.routes_by_parent_port.values_mut() {
            if let ParentKind::EgressNetwork { .. } = routes.parent.kind {
                routes.reinitialize_watches();
            }
        }
    }

    fn update_resource(&mut self, name: String, kind: ParentFlavor, resource: &ParentState) {
        tracing::debug!(?name, ?resource, "Updating");
        for (port, routes) in self.routes_by_parent_port.iter_mut() {
            if port.parent_name == name && kind == port.kind {
                routes.update_resource(port.port, resource)
            }
        }
    }

    fn delete_http_route(&mut self, gknn: &GroupKindNamespaceName) {
        for resource in self.routes_by_parent_port.values_mut() {
            resource.delete_http_route(gknn);
        }

        self.service_http_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn delete_grpc_route(&mut self, gknn: &GroupKindNamespaceName) {
        for resource in self.routes_by_parent_port.values_mut() {
            resource.delete_grpc_route(gknn);
        }

        self.service_grpc_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn delete_tls_route(&mut self, gknn: &GroupKindNamespaceName) {
        for resource in self.routes_by_parent_port.values_mut() {
            resource.delete_tls_route(gknn);
        }

        self.service_tls_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn delete_tcp_route(&mut self, gknn: &GroupKindNamespaceName) {
        for resource in self.routes_by_parent_port.values_mut() {
            resource.delete_tcp_route(gknn);
        }

        self.service_tcp_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn resource_routes_or_default(
        &mut self,
        rp: ParentPortKey,
        cluster: &ClusterInfo,
        resource_info: &HashMap<ParentRef, ParentState>,
    ) -> &mut Routes {
        self.routes_by_parent_port
            .entry(rp.clone())
            .or_insert_with(|| {
                let resource_ref = ParentRef {
                    name: rp.parent_name.clone(),
                    namespace: self.namespace.to_string(),
                    kind: rp.kind.clone(),
                };

                let mut parent_kind = match rp.kind {
                    ParentFlavor::EgressNetwork => ParentKind::EgressNetwork {
                        traffic: TrafficPolicy::Deny,
                    },
                    ParentFlavor::Service => {
                        let authority = cluster.service_dns_authority(
                            &self.namespace,
                            &rp.parent_name,
                            rp.port,
                        );
                        ParentKind::Service { authority }
                    }
                };
                let mut opaque = false;
                let mut accrual = None;
                let mut http_retry = None;
                let mut grpc_retry = None;
                let mut timeouts = Default::default();
                if let Some(resource) = resource_info.get(&resource_ref) {
                    opaque = port_is_opaque(&resource.ports, rp.port);
                    accrual = resource.accrual;
                    http_retry = resource.http_retry.clone();
                    grpc_retry = resource.grpc_retry.clone();
                    timeouts = resource.timeouts.clone();

                    if let Some(traffic) = resource.traffic_policy {
                        parent_kind = ParentKind::EgressNetwork { traffic }
                    }
                }

                // The routes which target this Resource but don't specify
                // a port apply to all ports. Therefore, we include them.
                let http_routes = self
                    .service_http_routes
                    .get(&rp.parent_name)
                    .cloned()
                    .unwrap_or_default();
                let grpc_routes = self
                    .service_grpc_routes
                    .get(&rp.parent_name)
                    .cloned()
                    .unwrap_or_default();
                let tls_routes = self
                    .service_tls_routes
                    .get(&rp.parent_name)
                    .cloned()
                    .unwrap_or_default();
                let tcp_routes = self
                    .service_tcp_routes
                    .get(&rp.parent_name)
                    .cloned()
                    .unwrap_or_default();

                let parent = Parent {
                    kind: parent_kind,
                    name: rp.parent_name.clone(),
                    namespace: self.namespace.to_string(),
                    port: ParentPort {
                        number: rp.port,
                        name: None,
                        opaque,
                    },
                };

                let mut resource_routes = Routes {
                    parent,
                    accrual,
                    http_retry,
                    grpc_retry,
                    timeouts,
                    namespace: self.namespace.clone(),
                    watches_by_ns: Default::default(),
                    allow_l5d_request_headers: false, // FIXME
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
                let (producer_tls_routes, consumer_tls_routes): (Vec<_>, Vec<_>) = tls_routes
                    .into_iter()
                    .partition(|(gknn, _)| gknn.namespace == *self.namespace);
                let (producer_tcp_routes, consumer_tcp_routes): (Vec<_>, Vec<_>) = tcp_routes
                    .into_iter()
                    .partition(|(gknn, _)| gknn.namespace == *self.namespace);

                for (consumer_gknn, consumer_route) in consumer_http_routes {
                    // Consumer routes should only apply to watches from the
                    // consumer namespace.
                    let consumer_watch = resource_routes
                        .watch_for_ns_or_default(consumer_gknn.namespace.to_string());

                    consumer_watch.insert_http_route(consumer_gknn.clone(), consumer_route.clone());
                }
                for (consumer_gknn, consumer_route) in consumer_grpc_routes {
                    // Consumer routes should only apply to watches from the
                    // consumer namespace.
                    let consumer_watch = resource_routes
                        .watch_for_ns_or_default(consumer_gknn.namespace.to_string());

                    consumer_watch.insert_grpc_route(consumer_gknn.clone(), consumer_route.clone());
                }
                for (consumer_gknn, consumer_route) in consumer_tls_routes {
                    // Consumer routes should only apply to watches from the
                    // consumer namespace.
                    let consumer_watch = resource_routes
                        .watch_for_ns_or_default(consumer_gknn.namespace.to_string());

                    consumer_watch.insert_tls_route(consumer_gknn.clone(), consumer_route.clone());
                }

                for (consumer_gknn, consumer_route) in consumer_tcp_routes {
                    // Consumer routes should only apply to watches from the
                    // consumer namespace.
                    let consumer_watch = resource_routes
                        .watch_for_ns_or_default(consumer_gknn.namespace.to_string());

                    consumer_watch.insert_tcp_route(consumer_gknn.clone(), consumer_route.clone());
                }

                for (producer_gknn, producer_route) in producer_http_routes {
                    // Insert the route into the producer namespace.
                    let producer_watch = resource_routes
                        .watch_for_ns_or_default(producer_gknn.namespace.to_string());

                    producer_watch.insert_http_route(producer_gknn.clone(), producer_route.clone());

                    // Producer routes apply to clients in all namespaces, so
                    // apply it to watches for all other namespaces too.
                    resource_routes
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
                    let producer_watch = resource_routes
                        .watch_for_ns_or_default(producer_gknn.namespace.to_string());

                    producer_watch.insert_grpc_route(producer_gknn.clone(), producer_route.clone());

                    // Producer routes apply to clients in all namespaces, so
                    // apply it to watches for all other namespaces too.
                    resource_routes
                        .watches_by_ns
                        .iter_mut()
                        .filter(|(namespace, _)| {
                            namespace.as_str() != producer_gknn.namespace.as_ref()
                        })
                        .for_each(|(_, watch)| {
                            watch.insert_grpc_route(producer_gknn.clone(), producer_route.clone())
                        });
                }

                for (producer_gknn, producer_route) in producer_tls_routes {
                    // Insert the route into the producer namespace.
                    let producer_watch = resource_routes
                        .watch_for_ns_or_default(producer_gknn.namespace.to_string());

                    producer_watch.insert_tls_route(producer_gknn.clone(), producer_route.clone());

                    // Producer routes apply to clients in all namespaces, so
                    // apply it to watches for all other namespaces too.
                    resource_routes
                        .watches_by_ns
                        .iter_mut()
                        .filter(|(namespace, _)| {
                            namespace.as_str() != producer_gknn.namespace.as_ref()
                        })
                        .for_each(|(_, watch)| {
                            watch.insert_tls_route(producer_gknn.clone(), producer_route.clone())
                        });
                }

                for (producer_gknn, producer_route) in producer_tcp_routes {
                    // Insert the route into the producer namespace.
                    let producer_watch = resource_routes
                        .watch_for_ns_or_default(producer_gknn.namespace.to_string());

                    producer_watch.insert_tcp_route(producer_gknn.clone(), producer_route.clone());

                    // Producer routes apply to clients in all namespaces, so
                    // apply it to watches for all other namespaces too.
                    resource_routes
                        .watches_by_ns
                        .iter_mut()
                        .filter(|(namespace, _)| {
                            namespace.as_str() != producer_gknn.namespace.as_ref()
                        })
                        .for_each(|(_, watch)| {
                            watch.insert_tcp_route(producer_gknn.clone(), producer_route.clone())
                        });
                }

                resource_routes
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
fn is_egress_network(group: Option<&str>, kind: &str) -> bool {
    // If the group is not specified or empty, assume it's 'policy.linkerd.io'.
    group
        .map(|g| g.eq_ignore_ascii_case("policy.linkerd.io"))
        .unwrap_or(false)
        && kind.eq_ignore_ascii_case("EgressNetwork")
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
pub fn is_parent_egress_network(parent: &ParentReference) -> bool {
    parent
        .kind
        .as_deref()
        .map(|k| is_egress_network(parent.group.as_deref(), k))
        // Parent refs require a `kind`.
        .unwrap_or(false)
}

#[inline]
pub fn is_parent_service_or_egress_network(parent: &ParentReference) -> bool {
    is_parent_service(parent) || is_parent_egress_network(parent)
}

#[inline]
fn route_accepted_by_resource_port(
    route_status: Option<&k8s_gateway_api::RouteStatus>,
    resource_port: &ParentPortKey,
) -> bool {
    let (kind, group) = match resource_port.kind {
        ParentFlavor::Service => (Service::kind(&()), Service::group(&())),
        ParentFlavor::EgressNetwork => (
            linkerd_k8s_api::EgressNetwork::kind(&()),
            linkerd_k8s_api::EgressNetwork::group(&()),
        ),
    };
    let mut group = &*group;
    if group.is_empty() {
        group = "core";
    }
    route_status
        .map(|status| status.parents.as_slice())
        .unwrap_or_default()
        .iter()
        .any(|parent_status| {
            let port_matches = match parent_status.parent_ref.port {
                Some(port) => port == resource_port.port.get(),
                None => true,
            };
            let mut parent_group = parent_status.parent_ref.group.as_deref().unwrap_or("core");
            if parent_group.is_empty() {
                parent_group = "core";
            }
            resource_port.parent_name == parent_status.parent_ref.name
                && Some(kind.as_ref()) == parent_status.parent_ref.kind.as_deref()
                && group == parent_group
                && port_matches
                && parent_status
                    .conditions
                    .iter()
                    .any(|condition| condition.type_ == "Accepted" && condition.status == "True")
        })
}

#[inline]
fn route_accepted_by_service(
    route_status: Option<&k8s_gateway_api::RouteStatus>,
    service: &str,
) -> bool {
    let mut service_group = &*Service::group(&());
    if service_group.is_empty() {
        service_group = "core";
    }
    route_status
        .map(|status| status.parents.as_slice())
        .unwrap_or_default()
        .iter()
        .any(|parent_status| {
            let mut parent_group = parent_status.parent_ref.group.as_deref().unwrap_or("core");
            if parent_group.is_empty() {
                parent_group = "core";
            }
            parent_status.parent_ref.name == service
                && parent_status.parent_ref.kind.as_deref() == Some(Service::kind(&()).as_ref())
                && parent_group == service_group
                && parent_status
                    .conditions
                    .iter()
                    .any(|condition| condition.type_ == "Accepted" && condition.status == "True")
        })
}

impl Routes {
    fn reinitialize_watches(&mut self) {
        for watch in self.watches_by_ns.values_mut() {
            watch.reinitialize_watch();
        }
    }

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

        let tls_routes = self
            .watches_by_ns
            .get(self.namespace.as_ref())
            .map(|watch| watch.tls_routes.clone())
            .unwrap_or_default();

        let tcp_routes = self
            .watches_by_ns
            .get(self.namespace.as_ref())
            .map(|watch| watch.tcp_routes.clone())
            .unwrap_or_default();

        self.watches_by_ns.entry(namespace).or_insert_with(|| {
            let (sender, _) = watch::channel(OutboundPolicy {
                parent: self.parent.clone(),
                accrual: self.accrual,
                http_retry: self.http_retry.clone(),
                grpc_retry: self.grpc_retry.clone(),
                timeouts: self.timeouts.clone(),
                allow_l5d_request_headers: self.allow_l5d_request_headers,
                http_routes: http_routes.clone(),
                grpc_routes: grpc_routes.clone(),
                tls_routes: tls_routes.clone(),
                tcp_routes: tcp_routes.clone(),
            });

            RoutesWatch {
                parent: self.parent.clone(),
                http_routes,
                grpc_routes,
                tls_routes,
                tcp_routes,
                watch: sender,
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

    fn apply_tls_route(&mut self, gknn: GroupKindNamespaceName, route: TlsRoute) {
        if *gknn.namespace == *self.namespace {
            // This is a producer namespace route.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());

            watch.insert_tls_route(gknn.clone(), route.clone());

            // Producer routes apply to clients in all namespaces, so
            // apply it to watches for all other namespaces too.
            for (ns, ns_watch) in self.watches_by_ns.iter_mut() {
                if ns != &gknn.namespace {
                    ns_watch.insert_tls_route(gknn.clone(), route.clone());
                }
            }
        } else {
            // This is a consumer namespace route and should only apply to
            // watches from that namespace.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());
            watch.insert_tls_route(gknn, route);
        }
    }

    fn apply_tcp_route(&mut self, gknn: GroupKindNamespaceName, route: TcpRoute) {
        if *gknn.namespace == *self.namespace {
            // This is a producer namespace route.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());

            watch.insert_tcp_route(gknn.clone(), route.clone());

            // Producer routes apply to clients in all namespaces, so
            // apply it to watches for all other namespaces too.
            for (ns, ns_watch) in self.watches_by_ns.iter_mut() {
                if ns != &gknn.namespace {
                    ns_watch.insert_tcp_route(gknn.clone(), route.clone());
                }
            }
        } else {
            // This is a consumer namespace route and should only apply to
            // watches from that namespace.
            let watch = self.watch_for_ns_or_default(gknn.namespace.to_string());
            watch.insert_tcp_route(gknn, route);
        }
    }

    fn update_resource(&mut self, port: NonZeroU16, rsc: &ParentState) {
        let ParentState {
            ports,
            accrual,
            http_retry,
            grpc_retry,
            timeouts,
            traffic_policy,
            ..
        } = rsc;

        self.parent.port.opaque = port_is_opaque(ports, port);
        self.accrual = *accrual;
        self.http_retry = http_retry.clone();
        self.grpc_retry = grpc_retry.clone();
        self.timeouts = timeouts.clone();
        self.update_traffic_policy(*traffic_policy);

        for watch in self.watches_by_ns.values_mut() {
            watch.parent = self.parent.clone();
            watch.accrual = *accrual;
            watch.http_retry = http_retry.clone();
            watch.grpc_retry = grpc_retry.clone();
            watch.timeouts = timeouts.clone();
            watch.update_traffic_policy(*traffic_policy);
            watch.send_if_modified();
        }
    }

    fn update_traffic_policy(&mut self, traffic_policy: Option<TrafficPolicy>) {
        if let (ParentKind::EgressNetwork { traffic, .. }, Some(tp)) =
            (&mut self.parent.kind, traffic_policy)
        {
            *traffic = tp;
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

    fn delete_tls_route(&mut self, gknn: &GroupKindNamespaceName) {
        for watch in self.watches_by_ns.values_mut() {
            watch.remove_tls_route(gknn);
        }
    }

    fn delete_tcp_route(&mut self, gknn: &GroupKindNamespaceName) {
        for watch in self.watches_by_ns.values_mut() {
            watch.remove_tcp_route(gknn);
        }
    }
}

impl RoutesWatch {
    fn reinitialize_watch(&mut self) {
        let current_policy = self.watch.borrow().clone();
        let (new_sender, _) = watch::channel(current_policy);
        self.watch = new_sender;
    }

    fn update_traffic_policy(&mut self, new: Option<TrafficPolicy>) {
        if let (ParentKind::EgressNetwork { traffic, .. }, Some(tp)) = (&mut self.parent.kind, new)
        {
            *traffic = tp
        }
    }

    fn send_if_modified(&mut self) {
        self.watch.send_if_modified(|policy| {
            let mut modified = false;

            if self.parent != policy.parent {
                policy.parent = self.parent.clone();
                modified = true;
            }

            if self.http_routes != policy.http_routes {
                policy.http_routes = self.http_routes.clone();
                modified = true;
            }

            if self.grpc_routes != policy.grpc_routes {
                policy.grpc_routes = self.grpc_routes.clone();
                modified = true;
            }

            if self.tls_routes != policy.tls_routes {
                policy.tls_routes = self.tls_routes.clone();
                modified = true;
            }

            if self.tcp_routes != policy.tcp_routes {
                policy.tcp_routes = self.tcp_routes.clone();
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

    fn insert_tls_route(&mut self, gknn: GroupKindNamespaceName, route: TlsRoute) {
        self.tls_routes.insert(gknn, route);

        self.send_if_modified();
    }

    fn insert_tcp_route(&mut self, gknn: GroupKindNamespaceName, route: TcpRoute) {
        self.tcp_routes.insert(gknn, route);

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

    fn remove_tls_route(&mut self, gknn: &GroupKindNamespaceName) {
        self.tls_routes.remove(gknn);
        self.send_if_modified();
    }

    fn remove_tcp_route(&mut self, gknn: &GroupKindNamespaceName) {
        self.tcp_routes.remove(gknn);
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

#[inline]
pub(crate) fn backend_kind(
    backend: &k8s_gateway_api::BackendObjectReference,
) -> Option<ParentFlavor> {
    let group = backend.group.as_deref();
    // Backends default to `Service` if no kind is specified.
    let kind = backend.kind.as_deref().unwrap_or("Service");
    if is_service(group, kind) {
        Some(ParentFlavor::Service)
    } else if is_egress_network(group, kind) {
        Some(ParentFlavor::EgressNetwork)
    } else {
        None
    }
}

fn port_is_opaque(ports: &HashMap<NonZeroU16, ParentPortSpec>, port: NonZeroU16) -> bool {
    // If the appProtocol is known and it is not one that we support, mark the
    // port as opaque.
    ports.get(&port).map_or(false, |ps| {
        ps.app_protocol
            .as_deref()
            .map_or(false, |ap| !is_known_app_protocol(ap))
    })
}

fn is_known_app_protocol(protocol: &str) -> bool {
    matches!(
        protocol,
        "http"
            | "https"
            | "kubernetes.io/h2c"
            | "kubernetes.io/ws"
            | "linkerd.io/h2c"
            | "linkerd.io/grpc"
    )
}
