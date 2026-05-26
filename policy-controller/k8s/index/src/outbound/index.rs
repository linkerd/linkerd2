use crate::{
    ports::{ports_annotation, PortMap, PortSet},
    routes::{ExplicitGKN, HttpRouteResource, ImpliedGKN},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, ensure, Result};
use egress_network::EgressNetwork;
use linkerd_policy_controller_core::{
    outbound::{
        AppProtocol, Backend, Backoff, FailureAccrual, GrpcRetryCondition, GrpcRoute,
        HttpRetryCondition, HttpRoute, Kind, LoadBiasConfig, OutboundDiscoverTarget,
        OutboundPolicy, ParentInfo, ResourceTarget, RetryAfterConfig, RouteRetry, RouteSet,
        RouteTimeouts, TcpRoute, TlsRoute, TrafficPolicy, DEFAULT_LOAD_BIAS_PENALTY,
        DEFAULT_LOAD_BIAS_PENALTY_DECAY, DEFAULT_RETRY_AFTER_MAX_DURATION,
    },
    routes::GroupKindNamespaceName,
};
use linkerd_policy_controller_k8s_api::{
    gateway,
    policy::{self as linkerd_k8s_api, Cidr},
    ResourceExt, Service,
};
use parking_lot::RwLock;
use std::{
    collections::hash_map::Entry, hash::Hash, net::IpAddr, num::NonZeroU16, str::FromStr,
    sync::Arc, time,
};
use tokio::sync::watch;

#[allow(dead_code)]
#[derive(Debug)]
pub struct Index {
    namespaces: NamespaceIndex,
    services_by_ip: HashMap<IpAddr, ServicePorts>,
    egress_networks_by_ref: HashMap<ResourceRef, EgressNetwork>,
    // holds information about resources. currently EgressNetworks and Services
    resource_info: HashMap<ResourceRef, ResourceInfo>,

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
pub enum ResourceKind {
    EgressNetwork,
    Service,
}

#[derive(Debug, Clone, Hash, PartialEq, Eq)]
pub struct ResourceRef {
    pub kind: ResourceKind,
    pub name: String,
    pub namespace: String,
}

#[derive(Debug)]
struct ServicePorts {
    service: ResourceRef,
    ports: PortSet,
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
    resource_port_routes: HashMap<ResourcePort, ResourceRoutes>,
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
struct ResourceInfo {
    app_protocols: PortMap<AppProtocol>,
    accrual: Option<FailureAccrual>,
    load_bias: Option<LoadBiasConfig>,
    retry_after: Option<RetryAfterConfig>,
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
    traffic_policy: Option<TrafficPolicy>,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
struct ResourcePort {
    kind: ResourceKind,
    name: String,
    port: NonZeroU16,
}

#[derive(Debug)]
struct ResourceRoutes {
    parent_info: ParentInfo,
    namespace: Arc<String>,
    port: NonZeroU16,
    watches_by_ns: HashMap<String, RoutesWatch>,
    app_protocol: Option<AppProtocol>,
    accrual: Option<FailureAccrual>,
    load_bias: Option<LoadBiasConfig>,
    retry_after: Option<RetryAfterConfig>,
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
}

#[derive(Debug)]
struct RoutesWatch {
    parent_info: ParentInfo,
    app_protocol: Option<AppProtocol>,
    accrual: Option<FailureAccrual>,
    load_bias: Option<LoadBiasConfig>,
    retry_after: Option<RetryAfterConfig>,
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

impl kubert::index::IndexNamespacedResource<gateway::HTTPRoute> for Index {
    fn apply(&mut self, route: gateway::HTTPRoute) {
        self.apply_http(HttpRouteResource::GatewayHttp(route))
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name.gkn::<gateway::HTTPRoute>().namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete_http_route(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<gateway::GRPCRoute> for Index {
    fn apply(&mut self, route: gateway::GRPCRoute) {
        self.apply_grpc(route)
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name.gkn::<gateway::GRPCRoute>().namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete_grpc_route(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<gateway::TLSRoute> for Index {
    fn apply(&mut self, route: gateway::TLSRoute) {
        self.apply_tls(route)
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name.gkn::<gateway::TLSRoute>().namespaced(namespace);
        for ns_index in self.namespaces.by_ns.values_mut() {
            ns_index.delete_tls_route(&gknn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<gateway::TCPRoute> for Index {
    fn apply(&mut self, route: gateway::TCPRoute) {
        self.apply_tcp(route)
    }

    fn delete(&mut self, namespace: String, name: String) {
        let gknn = name.gkn::<gateway::TCPRoute>().namespaced(namespace);
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
        // NB: The admission webhook only intercepts Route resources, not Services, so
        // annotation parse errors here surface only as controller log warnings (not API rejections).
        let accrual = parse_accrual_config(service.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse accrual config"))
            .unwrap_or_default();
        let load_bias = parse_load_bias_config(service.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse load bias config"))
            .unwrap_or_default();
        let retry_after = parse_retry_after_config(service.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse retry after config"))
            .unwrap_or_default();

        let mut app_protocols = service
            .spec
            .as_ref()
            .and_then(|spec| {
                spec.ports.as_ref().map(|ports| {
                    ports
                        .iter()
                        .filter_map(|port| {
                            port.app_protocol.as_ref().and_then(|p| {
                                Some((
                                    port.port.try_into().ok().and_then(NonZeroU16::new)?,
                                    AppProtocol::from_str(p.as_str()).expect("Infalliable"),
                                ))
                            })
                        })
                        .collect::<PortMap<AppProtocol>>()
                })
            })
            .unwrap_or_default();
        let opaque_ports =
            ports_annotation(service.annotations(), "config.linkerd.io/opaque-ports")
                .unwrap_or_else(|| self.namespaces.cluster_info.default_opaque_ports.clone());
        for opaque_port in opaque_ports {
            match app_protocols.entry(opaque_port) {
                Entry::Occupied(occupied) => {
                    tracing::debug!(
                        appProtocol = ?occupied.get(),
                        port = opaque_port.get(),
                        ns,
                        name,
                        "`appProtocol` set on service port, ignoring opaque port setting"
                    );
                }
                Entry::Vacant(vacant) => {
                    vacant.insert(AppProtocol::Opaque);
                }
            }
        }

        let timeouts = parse_timeouts(service.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse timeouts"))
            .unwrap_or_default();

        let http_retry = http::parse_http_retry(service.annotations()).map_err(|error| {
            tracing::warn!(%error, service=name, namespace=ns, "Failed to parse http retry")
        }).unwrap_or_default();
        let grpc_retry = grpc::parse_grpc_retry(service.annotations()).map_err(|error| {
            tracing::warn!(%error, service=name, namespace=ns, "Failed to parse grpc retry")
        }).unwrap_or_default();

        let service_ref = ResourceRef {
            kind: ResourceKind::Service,
            name: name.clone(),
            namespace: ns.clone(),
        };
        self.services_by_ip.retain(|_, v| v.service != service_ref);
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
                        service.spec.as_ref().and_then(|spec| {
                            spec.ports.as_ref().map(|ports| {
                                ports.iter().for_each(|port| {
                                    let port = match port.port.try_into().ok().and_then(NonZeroU16::new) {
                                        Some(port) => port,
                                        None => {
                                            tracing::warn!(%port.port, service=name, "Invalid service port");
                                            return;
                                        }
                                    };
                                    tracing::debug!(
                                        %addr,
                                        port,
                                        service = name,
                                        "inserting service into ip index"
                                    );
                                    self.services_by_ip
                                        .entry(addr)
                                        .or_insert(
                                            ServicePorts { service: service_ref.clone(), ports: Default::default() }
                                        ).ports.insert(port);
                                });
                            })
                        });
                    }
                    Err(error) => {
                        tracing::warn!(%error, service=name, cluster_ip, "Invalid cluster ip");
                    }
                }
            }
        }

        let service_info = ResourceInfo {
            app_protocols,
            accrual,
            load_bias,
            retry_after,
            http_retry,
            grpc_retry,
            timeouts,
            traffic_policy: None,
        };

        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_http_routes: Default::default(),
                service_grpc_routes: Default::default(),
                service_tls_routes: Default::default(),
                service_tcp_routes: Default::default(),
                resource_port_routes: Default::default(),
                namespace: Arc::new(ns),
            })
            .update_resource(
                service.name_unchecked(),
                ResourceKind::Service,
                &service_info,
            );

        self.resource_info.insert(
            ResourceRef {
                kind: ResourceKind::Service,
                name: service.name_unchecked(),
                namespace: service.namespace().expect("Service must have Namespace"),
            },
            service_info,
        );

        self.reindex_resources();
    }

    fn delete(&mut self, namespace: String, name: String) {
        tracing::debug!(name, namespace, "deleting service");
        let service_ref = ResourceRef {
            kind: ResourceKind::Service,
            name,
            namespace,
        };
        self.resource_info.remove(&service_ref);
        self.services_by_ip.retain(|_, v| v.service != service_ref);

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
        // EgressNetwork uses Forward instead of Balancer, so load bias and
        // retry-after have no effect. Warn if someone set them anyway.
        let load_bias = None;
        let retry_after = None;
        if egress_network
            .annotations()
            .contains_key("balancer.alpha.linkerd.io/load-bias")
        {
            tracing::warn!(
                service = name,
                namespace = ns,
                "load-bias annotation has no effect on EgressNetwork (no balancer)"
            );
        }
        if egress_network
            .annotations()
            .contains_key("balancer.alpha.linkerd.io/retry-after")
        {
            tracing::warn!(
                service = name,
                namespace = ns,
                "retry-after annotation has no effect on EgressNetwork (no balancer)"
            );
        }
        let opaque_ports = ports_annotation(
            egress_network.annotations(),
            "config.linkerd.io/opaque-ports",
        )
        .unwrap_or_else(|| self.namespaces.cluster_info.default_opaque_ports.clone());
        let app_protocols = opaque_ports
            .into_iter()
            .map(|port| (port, AppProtocol::Opaque))
            .collect();

        let timeouts = parse_timeouts(egress_network.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse timeouts"))
            .unwrap_or_default();

        let http_retry = http::parse_http_retry(egress_network.annotations()).map_err(|error| {
            tracing::warn!(%error, service=name, namespace=ns, "Failed to parse http retry")
        }).unwrap_or_default();
        let grpc_retry = grpc::parse_grpc_retry(egress_network.annotations()).map_err(|error| {
            tracing::warn!(%error, service=name, namespace=ns, "Failed to parse grpc retry")
        }).unwrap_or_default();

        let egress_net_ref = ResourceRef {
            kind: ResourceKind::EgressNetwork,
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

        let egress_network_info = ResourceInfo {
            app_protocols,
            accrual,
            load_bias,
            retry_after,
            http_retry,
            grpc_retry,
            timeouts,
            traffic_policy,
        };

        let ns = Arc::new(ns);
        self.namespaces
            .by_ns
            .entry(ns.to_string())
            .or_insert_with(|| Namespace {
                service_http_routes: Default::default(),
                service_grpc_routes: Default::default(),
                service_tls_routes: Default::default(),
                service_tcp_routes: Default::default(),
                resource_port_routes: Default::default(),
                namespace: ns.clone(),
            })
            .update_resource(
                egress_network.name_unchecked(),
                ResourceKind::EgressNetwork,
                &egress_network_info,
            );

        self.resource_info
            .insert(egress_net_ref, egress_network_info);

        self.reindex_resources();
        self.reinitialize_egress_watches(&ns);
        self.reinitialize_fallback_watches()
    }

    fn delete(&mut self, namespace: String, name: String) {
        tracing::debug!(name, namespace, "deleting EgressNetwork");
        let egress_net_ref = ResourceRef {
            kind: ResourceKind::EgressNetwork,
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
            resource_info: HashMap::default(),
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
            Kind::EgressNetwork { .. } => ResourceKind::EgressNetwork,
            Kind::Service => ResourceKind::Service,
        };

        let ns = self
            .namespaces
            .by_ns
            .entry(namespace.clone())
            .or_insert_with(|| Namespace {
                namespace: Arc::new(namespace.to_string()),
                service_http_routes: Default::default(),
                service_grpc_routes: Default::default(),
                service_tls_routes: Default::default(),
                service_tcp_routes: Default::default(),
                resource_port_routes: Default::default(),
            });

        let key = ResourcePort { kind, name, port };

        tracing::debug!(?key, "subscribing to resource port");

        let routes =
            ns.resource_routes_or_default(key, &self.namespaces.cluster_info, &self.resource_info);

        let watch = routes.watch_for_ns_or_default(source_namespace);

        Ok(watch.watch.subscribe())
    }

    pub fn lookup_service(
        &self,
        addr: IpAddr,
        port: NonZeroU16,
        source_namespace: String,
    ) -> Option<OutboundDiscoverTarget> {
        tracing::debug!(?addr, "looking up service");

        let service = self.services_by_ip.get(&addr)?;
        tracing::debug!(service=?service.service, "found service");
        if service.ports.contains(&port) {
            Some(OutboundDiscoverTarget::Resource(ResourceTarget {
                name: service.service.name.clone(),
                namespace: service.service.namespace.clone(),
                port,
                source_namespace,
                kind: Kind::Service,
            }))
        } else {
            Some(OutboundDiscoverTarget::UndefinedPort(ResourceTarget {
                name: service.service.name.clone(),
                namespace: service.service.namespace.clone(),
                port,
                source_namespace,
                kind: Kind::Service,
            }))
        }
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
        for parent_ref in route.parent_refs().iter().flatten() {
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
                    service_tls_routes: Default::default(),
                    service_tcp_routes: Default::default(),
                    resource_port_routes: Default::default(),
                });
        }

        // We must send the route update to all namespace indexes in case this
        // route's parent_refs have changed and this route must be removed by
        // any of them.
        self.namespaces.by_ns.values_mut().for_each(|ns| {
            ns.apply_http_route(
                route.clone(),
                &self.namespaces.cluster_info,
                &self.resource_info,
            );
        });
    }

    fn apply_grpc(&mut self, route: gateway::GRPCRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing grpcroute");

        // For each parent_ref, create a namespace index for it if it doesn't
        // already exist.
        for parent_ref in route.spec.parent_refs.iter().flatten() {
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
                    service_tls_routes: Default::default(),
                    service_tcp_routes: Default::default(),
                    resource_port_routes: Default::default(),
                });
        }

        // We must send the route update to all namespace indexes in case this
        // route's parent_refs have changed and this route must be removed by
        // any of them.
        for ns in self.namespaces.by_ns.values_mut() {
            ns.apply_grpc_route(
                route.clone(),
                &self.namespaces.cluster_info,
                &self.resource_info,
            );
        }
    }

    fn apply_tls(&mut self, route: gateway::TLSRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing tlsroute");

        // For each parent_ref, create a namespace index for it if it doesn't
        // already exist.
        for parent_ref in route.spec.parent_refs.iter().flatten() {
            let ns = parent_ref
                .namespace
                .clone()
                .unwrap_or_else(|| route.namespace().expect("TlsRoute must have a namespace"));

            self.namespaces
                .by_ns
                .entry(ns.clone())
                .or_insert_with(|| Namespace {
                    namespace: Arc::new(ns),
                    service_http_routes: Default::default(),
                    service_grpc_routes: Default::default(),
                    service_tls_routes: Default::default(),
                    service_tcp_routes: Default::default(),
                    resource_port_routes: Default::default(),
                });
        }

        // We must send the route update to all namespace indexes in case this
        // route's parent_refs have changed and this route must be removed by
        // any of them.
        for ns in self.namespaces.by_ns.values_mut() {
            ns.apply_tls_route(
                route.clone(),
                &self.namespaces.cluster_info,
                &self.resource_info,
            );
        }
    }

    fn apply_tcp(&mut self, route: gateway::TCPRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing tcproute");

        // For each parent_ref, create a namespace index for it if it doesn't
        // already exist.
        for parent_ref in route.spec.parent_refs.iter().flatten() {
            let ns = parent_ref
                .namespace
                .clone()
                .unwrap_or_else(|| route.namespace().expect("TcpRoute must have a namespace"));

            self.namespaces
                .by_ns
                .entry(ns.clone())
                .or_insert_with(|| Namespace {
                    namespace: Arc::new(ns),
                    service_http_routes: Default::default(),
                    service_grpc_routes: Default::default(),
                    service_tls_routes: Default::default(),
                    service_tcp_routes: Default::default(),
                    resource_port_routes: Default::default(),
                });
        }

        // We must send the route update to all namespace indexes in case this
        // route's parent_refs have changed and this route must be removed by
        // any of them.
        for ns in self.namespaces.by_ns.values_mut() {
            ns.apply_tcp_route(
                route.clone(),
                &self.namespaces.cluster_info,
                &self.resource_info,
            );
        }
    }

    fn reindex_resources(&mut self) {
        for ns in self.namespaces.by_ns.values_mut() {
            ns.reindex_resources(&self.resource_info);
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
    fn apply_http_route(
        &mut self,
        route: HttpRouteResource,
        cluster_info: &ClusterInfo,
        resource_info: &HashMap<ResourceRef, ResourceInfo>,
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

        for parent_ref in route.parent_refs().iter().flatten() {
            let parent_kind = if is_parent_service(&parent_ref.kind, &parent_ref.group) {
                ResourceKind::Service
            } else if is_parent_egress_network(&parent_ref.kind, &parent_ref.group) {
                ResourceKind::EgressNetwork
            } else {
                continue;
            };
            let route_namespace = route.namespace();
            let parent_namespace = parent_ref.namespace.as_ref().unwrap_or(&route_namespace);
            if *parent_namespace != *self.namespace {
                continue;
            }

            let port = parent_ref
                .port
                .and_then(|p| p.try_into().ok())
                .and_then(NonZeroU16::new);

            if let Some(port) = port {
                let resource_port = ResourcePort {
                    kind: parent_kind,
                    port,
                    name: parent_ref.name.clone(),
                };

                if !http::route_accepted_by_resource_port(route.status(), &resource_port) {
                    continue;
                }

                tracing::debug!(
                    ?resource_port,
                    route = route.name(),
                    "inserting httproute for resource"
                );

                let service_routes =
                    self.resource_routes_or_default(resource_port, cluster_info, resource_info);

                service_routes.apply_http_route(route.gknn(), outbound_route.clone());
            } else {
                if !http::route_accepted_by_service(route.status(), &parent_ref.name) {
                    continue;
                }
                // If the parent_ref doesn't include a port, apply this route
                // to all ResourceRoutes which match the resource name.
                for (ResourcePort { name, port: _, .. }, routes) in
                    self.resource_port_routes.iter_mut()
                {
                    if name == &parent_ref.name {
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
        for (resource_port, resource_routes) in self.resource_port_routes.iter_mut() {
            if !http::route_accepted_by_resource_port(route.status(), resource_port) {
                resource_routes.delete_http_route(&route.gknn());
            }
        }
        for (parent_name, routes) in self.service_http_routes.iter_mut() {
            if !http::route_accepted_by_service(route.status(), parent_name) {
                routes.remove(&route.gknn());
            }
        }
    }

    fn apply_grpc_route(
        &mut self,
        route: gateway::GRPCRoute,
        cluster_info: &ClusterInfo,
        resource_info: &HashMap<ResourceRef, ResourceInfo>,
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

        tracing::debug!(?outbound_route);

        for parent_ref in route.spec.parent_refs.iter().flatten() {
            let parent_kind = if is_parent_service(&parent_ref.kind, &parent_ref.group) {
                ResourceKind::Service
            } else if is_parent_egress_network(&parent_ref.kind, &parent_ref.group) {
                ResourceKind::EgressNetwork
            } else {
                continue;
            };
            let route_namespace = route.namespace().expect("GrpcRoute must have a namespace");
            let parent_namespace = parent_ref.namespace.as_ref().unwrap_or(&route_namespace);
            if *parent_namespace != *self.namespace {
                continue;
            }

            let port = parent_ref
                .port
                .and_then(|p| p.try_into().ok())
                .and_then(NonZeroU16::new);

            if let Some(port) = port {
                let port = ResourcePort {
                    kind: parent_kind,
                    port,
                    name: parent_ref.name.clone(),
                };

                if !grpc::route_accepted_by_resource_port(route.status.as_ref(), &port) {
                    continue;
                }

                tracing::debug!(
                    ?port,
                    route = route.name_unchecked(),
                    "inserting grpcroute for resource"
                );

                let service_routes =
                    self.resource_routes_or_default(port, cluster_info, resource_info);

                service_routes.apply_grpc_route(gknn.clone(), outbound_route.clone());
            } else {
                if !grpc::route_accepted_by_service(route.status.as_ref(), &parent_ref.name) {
                    continue;
                }
                // If the parent_ref doesn't include a port, apply this route
                // to all ResourceRoutes which match the resource name.
                self.resource_port_routes.iter_mut().for_each(
                    |(ResourcePort { name, port: _, .. }, routes)| {
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
        for (resource_port, resource_routes) in self.resource_port_routes.iter_mut() {
            if !grpc::route_accepted_by_resource_port(route.status.as_ref(), resource_port) {
                resource_routes.delete_grpc_route(&gknn);
            }
        }
        for (parent_name, routes) in self.service_grpc_routes.iter_mut() {
            if !grpc::route_accepted_by_service(route.status.as_ref(), parent_name) {
                routes.remove(&gknn);
            }
        }
    }

    fn apply_tls_route(
        &mut self,
        route: gateway::TLSRoute,
        cluster_info: &ClusterInfo,
        resource_info: &HashMap<ResourceRef, ResourceInfo>,
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
        let status = route.status.as_ref();

        for parent_ref in route.spec.parent_refs.iter().flatten() {
            let parent_kind = if is_parent_service(&parent_ref.kind, &parent_ref.group) {
                ResourceKind::Service
            } else if is_parent_egress_network(&parent_ref.kind, &parent_ref.group) {
                ResourceKind::EgressNetwork
            } else {
                continue;
            };
            let route_namespace = route.namespace().expect("TlsRoute must have a namespace");
            let parent_namespace = parent_ref.namespace.as_ref().unwrap_or(&route_namespace);
            if *parent_namespace != *self.namespace {
                continue;
            }

            let port = parent_ref
                .port
                .and_then(|p| p.try_into().ok())
                .and_then(NonZeroU16::new);

            if let Some(port) = port {
                let port = ResourcePort {
                    kind: parent_kind,
                    port,
                    name: parent_ref.name.clone(),
                };

                if !tls::route_accepted_by_resource_port(status, &port) {
                    continue;
                }

                tracing::debug!(
                    ?port,
                    route = route.name_unchecked(),
                    "inserting tlsroute for resource"
                );

                let resource_routes =
                    self.resource_routes_or_default(port, cluster_info, resource_info);

                resource_routes.apply_tls_route(gknn.clone(), outbound_route.clone());
            } else {
                if !tls::route_accepted_by_service(status, &parent_ref.name) {
                    continue;
                }
                // If the parent_ref doesn't include a port, apply this route
                // to all ResourceRoutes which match the resource name.
                self.resource_port_routes.iter_mut().for_each(
                    |(ResourcePort { name, port: _, .. }, routes)| {
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
        for (resource_port, resource_routes) in self.resource_port_routes.iter_mut() {
            if !tls::route_accepted_by_resource_port(status, resource_port) {
                resource_routes.delete_tls_route(&gknn);
            }
        }
        for (parent_name, routes) in self.service_tls_routes.iter_mut() {
            if !tls::route_accepted_by_service(status, parent_name) {
                routes.remove(&gknn);
            }
        }
    }

    fn apply_tcp_route(
        &mut self,
        route: gateway::TCPRoute,
        cluster_info: &ClusterInfo,
        resource_info: &HashMap<ResourceRef, ResourceInfo>,
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
        let status = route.status.as_ref();

        for parent_ref in route.spec.parent_refs.iter().flatten() {
            let parent_kind = if is_parent_service(&parent_ref.kind, &parent_ref.group) {
                ResourceKind::Service
            } else if is_parent_egress_network(&parent_ref.kind, &parent_ref.group) {
                ResourceKind::EgressNetwork
            } else {
                continue;
            };
            let route_namespace = route.namespace().expect("TcpRoute must have a namespace");
            let parent_namespace = parent_ref.namespace.as_ref().unwrap_or(&route_namespace);
            if *parent_namespace != *self.namespace {
                continue;
            }

            let port = parent_ref
                .port
                .and_then(|p| p.try_into().ok())
                .and_then(NonZeroU16::new);

            if let Some(port) = port {
                let port = ResourcePort {
                    kind: parent_kind,
                    port,
                    name: parent_ref.name.clone(),
                };

                if !tcp::route_accepted_by_resource_port(status, &port) {
                    continue;
                }

                tracing::debug!(
                    ?port,
                    route = route.name_unchecked(),
                    "inserting tcproute for resource"
                );

                let resource_routes =
                    self.resource_routes_or_default(port, cluster_info, resource_info);

                resource_routes.apply_tcp_route(gknn.clone(), outbound_route.clone());
            } else {
                if !tcp::route_accepted_by_service(status, &parent_ref.name) {
                    continue;
                }
                // If the parent_ref doesn't include a port, apply this route
                // to all ResourceRoutes which match the resource name.
                self.resource_port_routes.iter_mut().for_each(
                    |(ResourcePort { name, port: _, .. }, routes)| {
                        if name == &parent_ref.name {
                            routes.apply_tcp_route(gknn.clone(), outbound_route.clone());
                        }
                    },
                );

                // Also add the route to the list of routes that target the
                // resource without specifying a port.
                self.service_tcp_routes
                    .entry(parent_ref.name.clone())
                    .or_default()
                    .insert(gknn.clone(), outbound_route.clone());
            }
        }

        // Remove the route from all parents that are not in the route's parent_refs.
        for (resource_port, resource_routes) in self.resource_port_routes.iter_mut() {
            if !tcp::route_accepted_by_resource_port(status, resource_port) {
                resource_routes.delete_tcp_route(&gknn);
            }
        }
        for (parent_name, routes) in self.service_tcp_routes.iter_mut() {
            if !tcp::route_accepted_by_service(status, parent_name) {
                routes.remove(&gknn);
            }
        }
    }

    fn reindex_resources(&mut self, resource_info: &HashMap<ResourceRef, ResourceInfo>) {
        let update_backend = |backend: &mut Backend| {
            match backend {
                Backend::Service(svc) => {
                    let service_ref = ResourceRef {
                        kind: ResourceKind::Service,
                        name: svc.name.clone(),
                        namespace: svc.namespace.clone(),
                    };
                    svc.exists = resource_info.contains_key(&service_ref);
                }
                Backend::EgressNetwork(egress_net) => {
                    let egress_net_ref = ResourceRef {
                        kind: ResourceKind::EgressNetwork,
                        name: egress_net.name.clone(),
                        namespace: egress_net.namespace.clone(),
                    };
                    egress_net.exists = resource_info.contains_key(&egress_net_ref);
                }

                _ => {}
            };
        };

        for routes in self.resource_port_routes.values_mut() {
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

        let http_backends = self
            .service_http_routes
            .values_mut()
            .flat_map(|routes| routes.values_mut())
            .flat_map(|route| route.rules.iter_mut())
            .flat_map(|rule| rule.backends.iter_mut());
        let grpc_backends = self
            .service_grpc_routes
            .values_mut()
            .flat_map(|routes| routes.values_mut())
            .flat_map(|route| route.rules.iter_mut())
            .flat_map(|rule| rule.backends.iter_mut());
        let tls_backends = self
            .service_tls_routes
            .values_mut()
            .flat_map(|routes| routes.values_mut())
            .flat_map(|route| route.rule.backends.iter_mut());
        let tcp_backends = self
            .service_tcp_routes
            .values_mut()
            .flat_map(|routes| routes.values_mut())
            .flat_map(|route| route.rule.backends.iter_mut());

        http_backends
            .chain(grpc_backends)
            .chain(tls_backends)
            .chain(tcp_backends)
            .for_each(update_backend);
    }

    fn reinitialize_egress_watches(&mut self) {
        for routes in self.resource_port_routes.values_mut() {
            if let ParentInfo::EgressNetwork { .. } = routes.parent_info {
                routes.reinitialize_watches();
            }
        }
    }

    fn update_resource(&mut self, name: String, kind: ResourceKind, resource: &ResourceInfo) {
        tracing::debug!(?name, ?resource, "updating resource");

        for (resource_port, resource_routes) in self.resource_port_routes.iter_mut() {
            if resource_port.name != name || kind != resource_port.kind {
                continue;
            }

            let app_protocol = resource.app_protocols.get(&resource_port.port).cloned();

            resource_routes.update_resource(
                app_protocol,
                resource.accrual,
                resource.load_bias,
                resource.retry_after,
                resource.http_retry.clone(),
                resource.grpc_retry.clone(),
                resource.timeouts.clone(),
                resource.traffic_policy,
            );
        }
    }

    fn delete_http_route(&mut self, gknn: &GroupKindNamespaceName) {
        for resource in self.resource_port_routes.values_mut() {
            resource.delete_http_route(gknn);
        }

        self.service_http_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn delete_grpc_route(&mut self, gknn: &GroupKindNamespaceName) {
        for resource in self.resource_port_routes.values_mut() {
            resource.delete_grpc_route(gknn);
        }

        self.service_grpc_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn delete_tls_route(&mut self, gknn: &GroupKindNamespaceName) {
        for resource in self.resource_port_routes.values_mut() {
            resource.delete_tls_route(gknn);
        }

        self.service_tls_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn delete_tcp_route(&mut self, gknn: &GroupKindNamespaceName) {
        for resource in self.resource_port_routes.values_mut() {
            resource.delete_tcp_route(gknn);
        }

        self.service_tcp_routes.retain(|_, routes| {
            routes.remove(gknn);
            !routes.is_empty()
        });
    }

    fn resource_routes_or_default(
        &mut self,
        rp: ResourcePort,
        cluster: &ClusterInfo,
        resource_info: &HashMap<ResourceRef, ResourceInfo>,
    ) -> &mut ResourceRoutes {
        self.resource_port_routes
            .entry(rp.clone())
            .or_insert_with(|| {
                let resource_ref = ResourceRef {
                    name: rp.name.clone(),
                    namespace: self.namespace.to_string(),
                    kind: rp.kind.clone(),
                };

                let mut parent_info = match rp.kind {
                    ResourceKind::EgressNetwork => ParentInfo::EgressNetwork {
                        traffic_policy: TrafficPolicy::Deny,
                        name: resource_ref.name.clone(),
                        namespace: resource_ref.namespace.clone(),
                    },
                    ResourceKind::Service => {
                        let authority =
                            cluster.service_dns_authority(&self.namespace, &rp.name, rp.port);
                        ParentInfo::Service {
                            authority,
                            name: resource_ref.name.clone(),
                            namespace: resource_ref.namespace.clone(),
                        }
                    }
                };
                let mut app_protocol = None;
                let mut accrual = None;
                let mut load_bias = None;
                let mut retry_after = None;
                let mut http_retry = None;
                let mut grpc_retry = None;
                let mut timeouts = Default::default();
                if let Some(resource) = resource_info.get(&resource_ref) {
                    app_protocol = resource.app_protocols.get(&rp.port).cloned();
                    accrual = resource.accrual;
                    load_bias = resource.load_bias;
                    retry_after = resource.retry_after;
                    http_retry = resource.http_retry.clone();
                    grpc_retry = resource.grpc_retry.clone();
                    timeouts = resource.timeouts.clone();

                    if let Some(traffic_policy) = resource.traffic_policy {
                        parent_info = ParentInfo::EgressNetwork {
                            traffic_policy,
                            name: resource_ref.name,
                            namespace: resource_ref.namespace,
                        }
                    }
                }

                // The routes which target this Resource but don't specify
                // a port apply to all ports. Therefore, we include them.
                let http_routes = self
                    .service_http_routes
                    .get(&rp.name)
                    .cloned()
                    .unwrap_or_default();
                let grpc_routes = self
                    .service_grpc_routes
                    .get(&rp.name)
                    .cloned()
                    .unwrap_or_default();
                let tls_routes = self
                    .service_tls_routes
                    .get(&rp.name)
                    .cloned()
                    .unwrap_or_default();
                let tcp_routes = self
                    .service_tcp_routes
                    .get(&rp.name)
                    .cloned()
                    .unwrap_or_default();

                let mut resource_routes = ResourceRoutes {
                    parent_info,
                    app_protocol,
                    accrual,
                    load_bias,
                    retry_after,
                    http_retry,
                    grpc_retry,
                    timeouts,
                    port: rp.port,
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
pub fn is_parent_service(kind: &Option<String>, group: &Option<String>) -> bool {
    kind.as_deref()
        .map(|k| is_service(group.as_deref(), k))
        // Parent refs require a `kind`.
        .unwrap_or(false)
}

#[inline]
pub fn is_parent_egress_network(kind: &Option<String>, group: &Option<String>) -> bool {
    kind.as_deref()
        .map(|k| is_egress_network(group.as_deref(), k))
        // Parent refs require a `kind`.
        .unwrap_or(false)
}

#[inline]
pub fn is_parent_service_or_egress_network(kind: &Option<String>, group: &Option<String>) -> bool {
    is_parent_service(kind, group) || is_parent_egress_network(kind, group)
}

impl ResourceRoutes {
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
                parent_info: self.parent_info.clone(),
                port: self.port,
                app_protocol: self.app_protocol.clone(),
                accrual: self.accrual,
                load_bias: self.load_bias,
                retry_after: self.retry_after,
                http_retry: self.http_retry.clone(),
                grpc_retry: self.grpc_retry.clone(),
                timeouts: self.timeouts.clone(),
                http_routes: http_routes.clone(),
                grpc_routes: grpc_routes.clone(),
                tls_routes: tls_routes.clone(),
                tcp_routes: tcp_routes.clone(),
            });

            RoutesWatch {
                parent_info: self.parent_info.clone(),
                http_routes,
                grpc_routes,
                tls_routes,
                tcp_routes,
                watch: sender,
                app_protocol: self.app_protocol.clone(),
                accrual: self.accrual,
                load_bias: self.load_bias,
                retry_after: self.retry_after,
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

    #[allow(clippy::too_many_arguments)]
    fn update_resource(
        &mut self,
        app_protocol: Option<AppProtocol>,
        accrual: Option<FailureAccrual>,
        load_bias: Option<LoadBiasConfig>,
        retry_after: Option<RetryAfterConfig>,
        http_retry: Option<RouteRetry<HttpRetryCondition>>,
        grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
        timeouts: RouteTimeouts,
        traffic_policy: Option<TrafficPolicy>,
    ) {
        self.app_protocol = app_protocol.clone();
        self.accrual = accrual;
        self.load_bias = load_bias;
        self.retry_after = retry_after;
        self.http_retry = http_retry.clone();
        self.grpc_retry = grpc_retry.clone();
        self.timeouts = timeouts.clone();
        self.update_traffic_policy(traffic_policy);
        for watch in self.watches_by_ns.values_mut() {
            watch.app_protocol = app_protocol.clone();
            watch.accrual = accrual;
            watch.load_bias = load_bias;
            watch.retry_after = retry_after;
            watch.http_retry = http_retry.clone();
            watch.grpc_retry = grpc_retry.clone();
            watch.timeouts = timeouts.clone();
            watch.update_traffic_policy(traffic_policy);
            watch.send_if_modified();
        }
    }

    fn update_traffic_policy(&mut self, traffic_policy: Option<TrafficPolicy>) {
        if let (ParentInfo::EgressNetwork { traffic_policy, .. }, Some(new)) =
            (&mut self.parent_info, traffic_policy)
        {
            if *traffic_policy != new {
                *traffic_policy = new;
            }
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

    fn update_traffic_policy(&mut self, traffic_policy: Option<TrafficPolicy>) {
        if let (ParentInfo::EgressNetwork { traffic_policy, .. }, Some(new)) =
            (&mut self.parent_info, traffic_policy)
        {
            if *traffic_policy != new {
                *traffic_policy = new;
            }
        }
    }

    fn send_if_modified(&mut self) {
        self.watch.send_if_modified(|policy| {
            let mut modified = false;

            if self.parent_info != policy.parent_info {
                policy.parent_info = self.parent_info.clone();
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

            if self.app_protocol != policy.app_protocol {
                policy.app_protocol = self.app_protocol.clone();
                modified = true;
            }

            if self.accrual != policy.accrual {
                policy.accrual = self.accrual;
                modified = true;
            }

            if self.load_bias != policy.load_bias {
                policy.load_bias = self.load_bias;
                modified = true;
            }

            if self.retry_after != policy.retry_after {
                policy.retry_after = self.retry_after;
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

pub fn parse_load_bias_config(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<LoadBiasConfig>> {
    annotations
        .get("balancer.alpha.linkerd.io/load-bias")
        .and_then(|s| match s.trim() {
            "false" => None,
            mode => Some(mode),
        })
        .map(|mode| {
            if mode != "true" {
                bail!("unsupported load-bias mode: '{mode}' (expected 'true' or 'false')");
            }

            let penalty = annotations
                .get("balancer.alpha.linkerd.io/load-bias-penalty")
                .map(|s| parse_duration(s))
                .transpose()?
                .unwrap_or(DEFAULT_LOAD_BIAS_PENALTY);

            let penalty_decay = annotations
                .get("balancer.alpha.linkerd.io/load-bias-penalty-decay")
                .map(|s| parse_duration(s))
                .transpose()?
                .unwrap_or(DEFAULT_LOAD_BIAS_PENALTY_DECAY);

            ensure!(
                penalty > time::Duration::ZERO,
                "load-bias penalty must be greater than zero"
            );
            ensure!(
                penalty_decay > time::Duration::ZERO,
                "load-bias penalty_decay must be greater than zero"
            );

            Ok(LoadBiasConfig {
                enabled: true,
                penalty,
                penalty_decay,
            })
        })
        .transpose()
}

pub fn parse_retry_after_config(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<RetryAfterConfig>> {
    annotations
        .get("balancer.alpha.linkerd.io/retry-after")
        .and_then(|s| match s.trim() {
            "false" => None,
            mode => Some(mode),
        })
        .map(|mode| {
            if mode != "true" {
                bail!("unsupported retry-after mode: '{mode}' (expected 'true' or 'false')");
            }

            let max_duration = annotations
                .get("balancer.alpha.linkerd.io/retry-after-max-duration")
                .map(|s| parse_duration(s))
                .transpose()?
                .unwrap_or(DEFAULT_RETRY_AFTER_MAX_DURATION);

            ensure!(
                max_duration > time::Duration::ZERO,
                "retry-after-max-duration must be greater than zero"
            );

            Ok(RetryAfterConfig { max_duration })
        })
        .transpose()
}

fn parse_duration(s: &str) -> Result<time::Duration> {
    let s = s.trim();
    if s.starts_with('-') {
        bail!("duration value cannot be negative: '{s}'");
    }
    let offset = s
        .rfind(|c: char| c.is_ascii_digit())
        .ok_or_else(|| anyhow::anyhow!("{s} does not contain a timeout duration value"))?;
    let (magnitude, unit) = s.split_at(offset + 1);

    if magnitude.contains('.') {
        if unit == "s" {
            let frac: f64 = magnitude
                .parse()
                .map_err(|_| anyhow::anyhow!("invalid fractional value {magnitude}"))?;
            if frac == 0.0 {
                bail!("fractional seconds not supported; use '0' for zero duration");
            }
            if !frac.is_finite() || frac > (u64::MAX / 1000) as f64 {
                bail!("duration value {s} overflows when converted to milliseconds");
            }
            let ms = (frac * 1000.0).round() as u64;
            if ms >= 1 {
                bail!("fractional seconds not supported; try '{ms}ms' instead of '{s}'");
            } else {
                bail!("{s} value is sub-millisecond; minimum resolution is 1ms");
            }
        } else {
            bail!("fractional values not supported for duration unit '{unit}'");
        }
    }

    let magnitude = magnitude.parse::<u64>()?;

    let mul = match unit {
        // Special case: "0" is valid as zero duration without requiring a unit suffix.
        // Non-zero bare numbers (ie. "5") require a unit.
        "" if magnitude == 0 => 0,
        "ms" => 1,
        "s" => 1000,
        "m" => 1000 * 60,
        "h" => 1000 * 60 * 60,
        "d" => 1000 * 60 * 60 * 24,
        "" => bail!("missing duration unit; did you mean '{magnitude}s' or '{magnitude}ms'?"),
        _ => bail!("invalid duration unit {unit} (expected one of 'ms', 's', 'm', 'h', or 'd')"),
    };

    let ms = magnitude
        .checked_mul(mul)
        .ok_or_else(|| anyhow::anyhow!("Timeout value {s} overflows when converted to 'ms'"))?;
    Ok(time::Duration::from_millis(ms))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::BTreeMap;

    #[test]
    fn parse_duration_integer_seconds() {
        let d = parse_duration("10s").expect("should parse");
        assert_eq!(d, time::Duration::from_secs(10));
    }

    #[test]
    fn parse_duration_milliseconds() {
        let d = parse_duration("500ms").expect("should parse");
        assert_eq!(d, time::Duration::from_millis(500));
    }

    #[test]
    fn parse_duration_minutes() {
        let d = parse_duration("5m").expect("should parse");
        assert_eq!(d, time::Duration::from_secs(300));
    }

    #[test]
    fn parse_duration_hours() {
        let d = parse_duration("2h").expect("should parse");
        assert_eq!(d, time::Duration::from_secs(7200));
    }

    #[test]
    fn parse_duration_days() {
        let d = parse_duration("1d").expect("should parse");
        assert_eq!(d, time::Duration::from_secs(86400));
    }

    #[test]
    fn parse_duration_zero() {
        let d = parse_duration("0").expect("zero without unit should parse");
        assert_eq!(d, time::Duration::ZERO);
    }

    #[test]
    fn parse_duration_zero_seconds() {
        let d = parse_duration("0s").expect("0s should parse");
        assert_eq!(d, time::Duration::ZERO);
    }

    #[test]
    fn parse_duration_zero_milliseconds() {
        let d = parse_duration("0ms").expect("0ms should parse");
        assert_eq!(d, time::Duration::ZERO);
    }

    #[test]
    fn parse_duration_fractional_seconds_rejected() {
        let err = parse_duration("0.5s").expect_err("fractional seconds should fail");
        assert!(
            err.to_string().contains("500ms"),
            "should suggest ms equivalent: {err}"
        );
        assert!(
            err.to_string().contains("fractional seconds not supported"),
            "should explain the issue: {err}"
        );
    }

    #[test]
    fn parse_duration_fractional_zero_seconds_rejected() {
        let err = parse_duration("0.0s").expect_err("fractional zero should fail");
        assert!(
            err.to_string().contains("use '0' for zero duration"),
            "should suggest bare 0: {err}"
        );
    }

    #[test]
    fn parse_duration_fractional_non_seconds_rejected() {
        let err = parse_duration("0.5m").expect_err("fractional minutes should fail");
        assert!(
            err.to_string().contains("fractional values not supported"),
            "should reject fractional: {err}"
        );
    }

    #[test]
    fn parse_duration_bare_number_rejected() {
        let err = parse_duration("5").expect_err("bare number should fail");
        assert!(
            err.to_string().contains("missing duration unit"),
            "should mention missing unit: {err}"
        );
    }

    #[test]
    fn parse_duration_bare_number_error_suggests_units() {
        let err = parse_duration("100").expect_err("bare number should fail");
        assert!(
            err.to_string().contains("'100s' or '100ms'"),
            "should suggest likely units: {err}"
        );
    }

    #[test]
    fn parse_duration_overflow_rejected() {
        let err = parse_duration("999999999999999999d").expect_err("huge value should overflow");
        assert!(
            err.to_string().contains("overflows"),
            "should mention overflow: {err}"
        );
    }

    #[test]
    fn parse_duration_invalid_unit_rejected() {
        let err = parse_duration("10x").expect_err("invalid unit should fail");
        assert!(
            err.to_string().contains("invalid duration unit"),
            "should mention invalid unit: {err}"
        );
    }

    #[test]
    fn parse_duration_empty_rejected() {
        let result = parse_duration("");
        assert!(result.is_err(), "empty string should fail");
    }

    #[test]
    fn parse_duration_negative_seconds_rejected() {
        let err = parse_duration("-5s").expect_err("negative should fail");
        assert!(
            err.to_string().contains("cannot be negative"),
            "should mention negative: {err}"
        );
    }

    #[test]
    fn parse_duration_negative_fractional_rejected() {
        let err = parse_duration("-0.5s").expect_err("negative fractional should fail");
        assert!(
            err.to_string().contains("cannot be negative"),
            "should mention negative: {err}"
        );
    }

    #[test]
    fn load_bias_true_returns_defaults() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "true".to_string(),
        );
        let config = parse_load_bias_config(&annotations)
            .expect("mode=true should succeed")
            .expect("should return Some");
        assert!(config.enabled);
        assert_eq!(config.penalty, DEFAULT_LOAD_BIAS_PENALTY);
        assert_eq!(config.penalty_decay, DEFAULT_LOAD_BIAS_PENALTY_DECAY);
    }

    #[test]
    fn load_bias_false_returns_none() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "false".to_string(),
        );
        let result = parse_load_bias_config(&annotations).expect("mode=false should succeed");
        assert!(result.is_none(), "mode=false should return None");
    }

    #[test]
    fn load_bias_absent_returns_none() {
        let annotations = BTreeMap::new();
        let result =
            parse_load_bias_config(&annotations).expect("absent annotation should succeed");
        assert!(result.is_none(), "absent annotation should return None");
    }

    #[test]
    fn load_bias_invalid_mode_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "maybe".to_string(),
        );
        let err = parse_load_bias_config(&annotations).expect_err("mode=maybe should be rejected");
        assert!(
            err.to_string().contains("'maybe'"),
            "error should quote the invalid mode: {err}"
        );
    }

    #[test]
    fn load_bias_empty_mode_rejected_with_visible_quotes() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "".to_string(),
        );
        let err = parse_load_bias_config(&annotations).expect_err("empty mode should be rejected");
        let msg = err.to_string();
        assert!(
            msg.contains("''"),
            "error should show empty value in quotes: {msg}"
        );
    }

    #[test]
    fn load_bias_custom_penalty_and_decay() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty".to_string(),
            "3s".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty-decay".to_string(),
            "7s".to_string(),
        );
        let config = parse_load_bias_config(&annotations)
            .expect("custom values should succeed")
            .expect("should return Some");
        assert_eq!(config.penalty, time::Duration::from_secs(3));
        assert_eq!(config.penalty_decay, time::Duration::from_secs(7));
    }

    #[test]
    fn load_bias_invalid_penalty_duration_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty".to_string(),
            "notaduration".to_string(),
        );
        let result = parse_load_bias_config(&annotations);
        assert!(result.is_err(), "invalid penalty duration should fail");
    }

    #[test]
    fn load_bias_zero_penalty_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty".to_string(),
            "0".to_string(),
        );
        let err =
            parse_load_bias_config(&annotations).expect_err("zero penalty should be rejected");
        assert!(
            err.to_string().contains("greater than zero"),
            "error should mention 'greater than zero': {err}"
        );
    }

    #[test]
    fn load_bias_zero_penalty_decay_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty-decay".to_string(),
            "0".to_string(),
        );
        let err = parse_load_bias_config(&annotations)
            .expect_err("zero penalty_decay should be rejected");
        assert!(
            err.to_string().contains("greater than zero"),
            "error should mention 'greater than zero': {err}"
        );
    }

    #[test]
    fn load_bias_whitespace_true_accepted() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            " true ".to_string(),
        );
        let config = parse_load_bias_config(&annotations)
            .expect("whitespace-padded 'true' should succeed")
            .expect("should return Some");
        assert!(config.enabled);
    }

    #[test]
    fn load_bias_whitespace_false_returns_none() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            " false ".to_string(),
        );
        let result =
            parse_load_bias_config(&annotations).expect("whitespace-padded 'false' should succeed");
        assert!(result.is_none());
    }

    #[test]
    fn retry_after_true_returns_defaults() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after".to_string(),
            "true".to_string(),
        );
        let config = parse_retry_after_config(&annotations)
            .expect("mode=true should succeed")
            .expect("should return Some");
        assert_eq!(config.max_duration, DEFAULT_RETRY_AFTER_MAX_DURATION);
    }

    #[test]
    fn retry_after_false_returns_none() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after".to_string(),
            "false".to_string(),
        );
        let result = parse_retry_after_config(&annotations).expect("mode=false should succeed");
        assert!(result.is_none(), "mode=false should return None");
    }

    #[test]
    fn retry_after_absent_returns_none() {
        let annotations = BTreeMap::new();
        let result =
            parse_retry_after_config(&annotations).expect("absent annotation should succeed");
        assert!(result.is_none(), "absent annotation should return None");
    }

    #[test]
    fn retry_after_whitespace_true_accepted() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after".to_string(),
            " true ".to_string(),
        );
        let config = parse_retry_after_config(&annotations)
            .expect("whitespace-padded 'true' should succeed")
            .expect("should return Some");
        assert_eq!(config.max_duration, DEFAULT_RETRY_AFTER_MAX_DURATION);
    }

    #[test]
    fn retry_after_invalid_mode_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after".to_string(),
            "sometimes".to_string(),
        );
        let err =
            parse_retry_after_config(&annotations).expect_err("mode=sometimes should be rejected");
        assert!(
            err.to_string().contains("'sometimes'"),
            "error should quote the invalid mode: {err}"
        );
    }

    #[test]
    fn retry_after_custom_max_duration() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after-max-duration".to_string(),
            "60s".to_string(),
        );
        let config = parse_retry_after_config(&annotations)
            .expect("custom max should succeed")
            .expect("should return Some");
        assert_eq!(config.max_duration, time::Duration::from_secs(60));
    }

    #[test]
    fn retry_after_zero_max_duration_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after-max-duration".to_string(),
            "0s".to_string(),
        );
        let err = parse_retry_after_config(&annotations)
            .expect_err("zero max_duration should be rejected");
        assert!(
            err.to_string().contains("greater than zero"),
            "error should mention 'greater than zero': {err}"
        );
    }

    #[test]
    fn load_bias_custom_decay_default_penalty() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty-decay".to_string(),
            "20s".to_string(),
        );
        let config = parse_load_bias_config(&annotations)
            .expect("custom decay with default penalty should succeed")
            .expect("should return Some");
        assert_eq!(config.penalty, DEFAULT_LOAD_BIAS_PENALTY);
        assert_eq!(config.penalty_decay, time::Duration::from_secs(20));
    }

    #[test]
    fn load_bias_false_ignores_sub_annotations() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "false".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty".to_string(),
            "3s".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty-decay".to_string(),
            "7s".to_string(),
        );
        let result = parse_load_bias_config(&annotations).expect("mode=false should succeed");
        assert!(
            result.is_none(),
            "mode=false should return None even with sub-annotations"
        );
    }

    #[test]
    fn retry_after_false_ignores_sub_annotations() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after".to_string(),
            "false".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after-max-duration".to_string(),
            "60s".to_string(),
        );
        let result = parse_retry_after_config(&annotations).expect("mode=false should succeed");
        assert!(
            result.is_none(),
            "mode=false should return None even with sub-annotations"
        );
    }

    #[test]
    fn load_bias_negative_penalty_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-bias-penalty".to_string(),
            "-5s".to_string(),
        );
        let err =
            parse_load_bias_config(&annotations).expect_err("negative penalty should be rejected");
        assert!(
            err.to_string().contains("cannot be negative"),
            "error should mention negative: {err}"
        );
    }

    #[test]
    fn retry_after_negative_max_duration_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/retry-after-max-duration".to_string(),
            "-10s".to_string(),
        );
        let err = parse_retry_after_config(&annotations)
            .expect_err("negative max_duration should be rejected");
        assert!(
            err.to_string().contains("cannot be negative"),
            "error should mention negative: {err}"
        );
    }
}
