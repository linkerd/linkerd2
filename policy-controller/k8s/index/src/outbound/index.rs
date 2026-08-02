use crate::{
    ports::{ports_annotation, PortMap, PortSet},
    routes::{ExplicitGKN, HttpRouteResource, ImpliedGKN},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, ensure, Context, Result};
use egress_network::EgressNetwork;
use linkerd_policy_controller_core::{
    outbound::{
        AppProtocol, Backend, Backoff, FailureAccrual, GrpcRetryCondition, GrpcRoute,
        HttpRetryCondition, HttpRoute, Kind, LoadBiaserConfig, OutboundDiscoverTarget,
        OutboundPolicy, ParentInfo, ResourceTarget, RouteRetry, RouteSet, RouteTimeouts,
        SuccessRateConfig, TcpRoute, TlsRoute, TrafficPolicy, DEFAULT_LOAD_BIASER_MAX_RETRY_AFTER,
        DEFAULT_LOAD_BIASER_PENALTY, DEFAULT_SUCCESS_RATE_MIN_REQUESTS,
        DEFAULT_SUCCESS_RATE_THRESHOLD, DEFAULT_SUCCESS_RATE_WINDOW, MAX_SUCCESS_RATE_MIN_REQUESTS,
        MIN_SUCCESS_RATE_WINDOW,
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
    // a Fallback policy are subscribed. It is used to force these clients
    // to reconnect and obtain new policy once the current one may no longer
    // be valid
    fallback_policy_tx: watch::Sender<()>,
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
    load_biaser: Option<LoadBiaserConfig>,
    honor_retry_after: bool,
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
    load_biaser: Option<LoadBiaserConfig>,
    honor_retry_after: bool,
    http_retry: Option<RouteRetry<HttpRetryCondition>>,
    grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
    timeouts: RouteTimeouts,
}

#[derive(Debug)]
struct RoutesWatch {
    parent_info: ParentInfo,
    app_protocol: Option<AppProtocol>,
    accrual: Option<FailureAccrual>,
    load_biaser: Option<LoadBiaserConfig>,
    honor_retry_after: bool,
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
        tracing::debug!(service = name, namespace = ns, "indexing service");
        let accrual = parse_accrual_config(service.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse accrual config"))
            .unwrap_or_default();
        let load_biaser = parse_load_biaser_config(service.annotations())
            .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse load biaser config"))
            .unwrap_or_default();
        let honor_retry_after = parse_bool_annotation(
            service.annotations(),
            "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after",
        )
        .map_err(|error| tracing::warn!(%error, service=name, namespace=ns, "Failed to parse honor-retry-after toggle"))
        .unwrap_or_default();

        // honor-retry-after only configures the failure-accrual backoff. It
        // does nothing without a mode set.
        if honor_retry_after && accrual.is_none() {
            tracing::debug!(
                service = name,
                namespace = ns,
                "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after has \
                 no effect without a failure-accrual mode"
            );
        }

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
            load_biaser,
            honor_retry_after,
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
        tracing::debug!(
            egress_network = name,
            namespace = ns,
            "indexing EgressNetwork"
        );
        let accrual = parse_accrual_config(egress_network.annotations())
            .map_err(|error| tracing::warn!(%error, egress_network=name, namespace=ns, "Failed to parse accrual config"))
            .unwrap_or_default();
        // EgressNetwork's default backend forwards without a balancer. The
        // penalty estimator never applies there. Route backends that resolve
        // to Services do balance, and the code below disables their penalty.
        // The Retry-After opt-in is attached to the failure-accrual backoff.
        // That backoff reaches only the Service-backed route backends.
        let load_biaser = None;
        let honor_retry_after = parse_bool_annotation(
            egress_network.annotations(),
            "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after",
        )
        .map_err(|error| tracing::warn!(%error, egress_network=name, namespace=ns, "Failed to parse honor-retry-after toggle"))
        .unwrap_or_default();

        // honor-retry-after only configures the failure-accrual backoff. It
        // does nothing without a mode set.
        if honor_retry_after && accrual.is_none() {
            tracing::debug!(
                egress_network = name,
                namespace = ns,
                "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after has \
                 no effect without a failure-accrual mode"
            );
        }

        // Read the penalize-failures toggle directly to warn the operator.
        // A full load biaser parse here would only be discarded.
        match parse_bool_annotation(
            egress_network.annotations(),
            "balancer.alpha.linkerd.io/penalize-failures",
        ) {
            Ok(true) => tracing::debug!(
                egress_network = name,
                namespace = ns,
                "penalize-failures annotation has no effect on EgressNetwork; \
                 the forward path has no balancer and Service-backed routes \
                 drop the penalty"
            ),
            Ok(false) => {}
            Err(error) => tracing::warn!(
                %error,
                egress_network = name,
                namespace = ns,
                "Failed to parse penalize-failures toggle"
            ),
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
            .map_err(|error| tracing::warn!(%error, egress_network=name, namespace=ns, "Failed to parse timeouts"))
            .unwrap_or_default();

        let http_retry = http::parse_http_retry(egress_network.annotations()).map_err(|error| {
            tracing::warn!(%error, egress_network=name, namespace=ns, "Failed to parse http retry")
        }).unwrap_or_default();
        let grpc_retry = grpc::parse_grpc_retry(egress_network.annotations()).map_err(|error| {
            tracing::warn!(%error, egress_network=name, namespace=ns, "Failed to parse grpc retry")
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
            load_biaser,
            honor_retry_after,
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

        let (fallback_policy_tx, _) = watch::channel(());
        Arc::new(RwLock::new(Self {
            namespaces: NamespaceIndex {
                by_ns: HashMap::default(),
                cluster_info,
            },
            services_by_ip: HashMap::default(),
            egress_networks_by_ref: HashMap::default(),
            resource_info: HashMap::default(),
            cluster_networks: cluster_networks.into_iter().map(Cidr::from).collect(),
            fallback_policy_tx,
            global_egress_network_namespace,
        }))
    }

    pub fn is_address_in_cluster(&self, addr: IpAddr) -> bool {
        self.cluster_networks
            .iter()
            .any(|net| net.contains(&addr.into()))
    }

    pub fn fallback_policy_rx(&self) -> watch::Receiver<()> {
        self.fallback_policy_tx.subscribe()
    }

    fn reinitialize_fallback_watches(&mut self) {
        let (new_fallback_tx, _) = watch::channel(());
        self.fallback_policy_tx = new_fallback_tx;
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
                resource.load_biaser,
                resource.honor_retry_after,
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
                let mut load_biaser = None;
                let mut honor_retry_after = false;
                let mut http_retry = None;
                let mut grpc_retry = None;
                let mut timeouts = Default::default();
                if let Some(resource) = resource_info.get(&resource_ref) {
                    app_protocol = resource.app_protocols.get(&rp.port).cloned();
                    accrual = resource.accrual;
                    load_biaser = resource.load_biaser;
                    honor_retry_after = resource.honor_retry_after;
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
                    load_biaser,
                    honor_retry_after,
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
                load_biaser: self.load_biaser,
                honor_retry_after: self.honor_retry_after,
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
                load_biaser: self.load_biaser,
                honor_retry_after: self.honor_retry_after,
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
        load_biaser: Option<LoadBiaserConfig>,
        honor_retry_after: bool,
        http_retry: Option<RouteRetry<HttpRetryCondition>>,
        grpc_retry: Option<RouteRetry<GrpcRetryCondition>>,
        timeouts: RouteTimeouts,
        traffic_policy: Option<TrafficPolicy>,
    ) {
        self.app_protocol = app_protocol.clone();
        self.accrual = accrual;
        self.load_biaser = load_biaser;
        self.honor_retry_after = honor_retry_after;
        self.http_retry = http_retry.clone();
        self.grpc_retry = grpc_retry.clone();
        self.timeouts = timeouts.clone();
        self.update_traffic_policy(traffic_policy);
        for watch in self.watches_by_ns.values_mut() {
            watch.app_protocol = app_protocol.clone();
            watch.accrual = accrual;
            watch.load_biaser = load_biaser;
            watch.honor_retry_after = honor_retry_after;
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

            if self.load_biaser != policy.load_biaser {
                policy.load_biaser = self.load_biaser;
                modified = true;
            }

            if self.honor_retry_after != policy.honor_retry_after {
                policy.honor_retry_after = self.honor_retry_after;
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

/// Builds a success-rate annotation key from its suffix. The shared
/// prefix then stays in one place.
macro_rules! success_rate_key {
    ($suffix:literal) => {
        concat!(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate",
            $suffix
        )
    };
}

pub fn parse_accrual_config(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<FailureAccrual>> {
    let Some(mode) = annotations.get("balancer.linkerd.io/failure-accrual") else {
        // Success-rate annotations only take effect under a failure-accrual
        // mode. Warn if they are set without one.
        if annotations
            .keys()
            .any(|k| k.starts_with(success_rate_key!("")))
        {
            tracing::debug!(
                "success-rate annotations are set but no failure-accrual mode \
                 is configured; set balancer.linkerd.io/failure-accrual to \
                 'unified'"
            );
        }
        return Ok(None);
    };

    if mode == "consecutive" {
        // Success-rate annotations apply only in unified mode. Under
        // consecutive mode they have no effect. Warn and keep the
        // consecutive breaker instead of dropping the whole config.
        if annotations
            .keys()
            .any(|k| k.starts_with(success_rate_key!("")))
        {
            tracing::debug!(
                "success-rate annotations have no effect under \
                 failure-accrual mode 'consecutive'; use mode 'unified' \
                 to enable success-rate circuit breaking"
            );
        }

        let (max_failures, backoff) = parse_consecutive_params(annotations)?;

        Ok(Some(FailureAccrual::Consecutive {
            max_failures,
            backoff,
        }))
    } else if mode == "unified" {
        // The unified breaker keeps the consecutive-failures dimension.
        // Its parameters are read from the same annotations that the
        // consecutive mode uses, with the same defaults.
        let (max_failures, backoff) = parse_consecutive_params(annotations)?;
        let success_rate = parse_success_rate_params(annotations)?;

        // Warn about success-rate annotations the parser does not
        // recognize. A typo here silently falls back to the default.
        for key in annotations.keys() {
            if let Some(suffix) = key.strip_prefix(success_rate_key!("")) {
                if !matches!(suffix, "-threshold" | "-window" | "-min-requests") {
                    tracing::debug!("unrecognized success-rate annotation: {key}");
                }
            }
        }

        // Both dimensions disabled means no breaker runs at all.
        if max_failures == 0 && success_rate.threshold == 0.0 {
            tracing::debug!(
                "unified failure-accrual has both dimensions disabled \
                 (max-failures=0 and success-rate-threshold=0); no breaker \
                 will run"
            );
        }

        Ok(Some(FailureAccrual::Unified {
            max_failures,
            backoff,
            success_rate,
        }))
    } else {
        bail!("unsupported failure accrual mode: {mode}");
    }
}

/// Parses the failure-accrual-consecutive-* annotations shared by the
/// consecutive and unified accrual modes.
fn parse_consecutive_params(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<(u32, Backoff)> {
    let max_failures = annotations
        .get("balancer.linkerd.io/failure-accrual-consecutive-max-failures")
        .map(|s| s.trim().parse::<u32>())
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
        .map(|s| s.trim().parse::<f32>())
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

    Ok((
        max_failures,
        Backoff {
            min_penalty,
            max_penalty,
            jitter,
        },
    ))
}

/// Parses the balancer.alpha.linkerd.io success-rate annotations read by the
/// unified accrual mode.
fn parse_success_rate_params(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<SuccessRateConfig> {
    let threshold = annotations
        .get(success_rate_key!("-threshold"))
        // The proto defines success_rate_threshold as a double, unlike the
        // f32 jitter_ratio.
        .map(|s| s.trim().parse::<f64>())
        .transpose()?
        .unwrap_or(DEFAULT_SUCCESS_RATE_THRESHOLD);
    ensure!(
        (0.0..=1.0).contains(&threshold),
        "success-rate threshold must be between 0.0 and 1.0, got {threshold}"
    );

    if threshold == 0.0 {
        tracing::debug!(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold=0 \
             disables success-rate circuit breaking; the success-rate window \
             will not trip the breaker"
        );
    }

    let window = annotations
        .get(success_rate_key!("-window"))
        .map(|s| parse_duration(s))
        .transpose()?
        .unwrap_or(DEFAULT_SUCCESS_RATE_WINDOW);
    ensure!(
        window > time::Duration::ZERO,
        "success-rate-window must be greater than zero"
    );
    // The proxy rejects a success-rate window below 10ms, its minimum
    // sampling window, when converting the client policy. A rejected
    // conversion invalidates the entire policy. The proxy applies this
    // floor only when the threshold is non-zero. Gate it the same way to
    // keep a consecutive-only unified policy (threshold 0) intact.
    if threshold != 0.0 {
        ensure!(
            window >= MIN_SUCCESS_RATE_WINDOW,
            "success-rate-window must be at least 10ms, got {window:?}"
        );
    }

    // Skip the advisory when success-rate is off, like the floor above.
    if threshold != 0.0 && window < time::Duration::from_secs(1) {
        tracing::debug!(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-window={window:?} \
             is below 1s; the window is divided into ~10 fixed-duration buckets, \
             so a sub-second window gives very coarse buckets and may cause \
             spurious circuit-breaker trips"
        );
    }

    let min_requests = annotations
        .get(success_rate_key!("-min-requests"))
        .map(|s| s.trim().parse::<u32>())
        .transpose()?
        .unwrap_or(DEFAULT_SUCCESS_RATE_MIN_REQUESTS);
    // The proxy rejects min-requests above 1,000,000 when converting the
    // client policy. A rejected conversion invalidates the entire policy.
    // Like the window floor, this ceiling applies only when the threshold
    // is non-zero. Gate it to match.
    if threshold != 0.0 {
        ensure!(
            min_requests <= MAX_SUCCESS_RATE_MIN_REQUESTS,
            "success-rate-min-requests cannot exceed 1000000, got {min_requests}"
        );
    }

    // Skip when success-rate is off, like the window advisory above.
    if threshold != 0.0 && min_requests == 0 {
        tracing::debug!(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-min-requests=0 \
             means the breaker can trip on the very first failure; \
             consider setting a higher value"
        );
    }

    Ok(SuccessRateConfig {
        threshold,
        window,
        min_requests,
    })
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

pub fn parse_load_biaser_config(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<LoadBiaserConfig>> {
    let enabled =
        parse_bool_annotation(annotations, "balancer.alpha.linkerd.io/penalize-failures")?;
    if !enabled {
        return Ok(None);
    }
    let penalty = parse_duration_annotation(
        annotations,
        "balancer.alpha.linkerd.io/load-biaser-penalty",
        DEFAULT_LOAD_BIASER_PENALTY,
    )?;
    // The proxy clamps the penalty to a u32 of milliseconds. Reject a larger
    // value here so it does not saturate silently on the wire.
    ensure!(
        penalty.as_millis() <= u32::MAX as u128,
        "load-biaser-penalty exceeds the maximum supported value (~49.7 days)"
    );
    let max_retry_after = parse_duration_annotation(
        annotations,
        "balancer.alpha.linkerd.io/load-biaser-max-retry-after",
        DEFAULT_LOAD_BIASER_MAX_RETRY_AFTER,
    )?;
    Ok(Some(LoadBiaserConfig {
        penalty,
        max_retry_after,
    }))
}

/// Reads an opt-in boolean annotation. Absence is treated as `false`,
/// matching the failure-accrual annotations, where an unset toggle leaves the
/// feature off. Accepts the same token set as Go's `strconv.ParseBool`, the
/// parser every boolean control-plane annotation already passes through.
/// An unrecognized value is rejected. A typo surfaces rather than silently
/// flipping the feature.
fn parse_bool_annotation(
    annotations: &std::collections::BTreeMap<String, String>,
    key: &str,
) -> Result<bool> {
    match annotations.get(key).map(|s| s.trim()) {
        None | Some("0" | "f" | "F" | "false" | "FALSE" | "False") => Ok(false),
        Some("1" | "t" | "T" | "true" | "TRUE" | "True") => Ok(true),
        Some(other) => {
            bail!(
                "unsupported {key} value: '{other}' (expected a boolean such as 'true' or 'false')"
            )
        }
    }
}

/// Reads an optional Duration annotation. Returns `default` when the key is
/// absent. A present value is parsed and surfaces any error.
fn parse_duration_annotation(
    annotations: &std::collections::BTreeMap<String, String>,
    key: &str,
    default: time::Duration,
) -> Result<time::Duration> {
    match annotations.get(key) {
        Some(v) => parse_duration(v).with_context(|| format!("invalid {key} value")),
        None => Ok(default),
    }
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

    // Any fractional duration is rejected. A fractional value with a 's'
    // suffix could in principle round to a whole number of milliseconds, but
    // every other case in this branch also rejects, so a single message is
    // clearer than enumerating the individual reasons.
    if magnitude.contains('.') {
        bail!("fractional values not supported");
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

    /// Returns an annotation map with the failure-accrual mode set to
    /// "unified" plus any additional key-value pairs.
    fn accrual_annotations(extra: &[(&str, &str)]) -> BTreeMap<String, String> {
        let mut m = BTreeMap::new();
        m.insert(
            "balancer.linkerd.io/failure-accrual".to_string(),
            "unified".to_string(),
        );
        for (k, v) in extra {
            m.insert(k.to_string(), v.to_string());
        }
        m
    }

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
            err.to_string().contains("fractional values not supported"),
            "should reject fractional: {err}"
        );
    }

    #[test]
    fn parse_duration_fractional_zero_seconds_rejected() {
        let err = parse_duration("0.0s").expect_err("fractional zero should fail");
        assert!(
            err.to_string().contains("fractional values not supported"),
            "should reject fractional: {err}"
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
    fn penalize_failures_true_returns_defaults() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "true".to_string(),
        );
        let config = parse_load_biaser_config(&annotations)
            .expect("penalize-failures=true should succeed")
            .expect("should return Some");
        assert_eq!(config.penalty, DEFAULT_LOAD_BIASER_PENALTY);
        assert_eq!(config.max_retry_after, DEFAULT_LOAD_BIASER_MAX_RETRY_AFTER);
    }

    #[test]
    fn penalize_failures_false_returns_none() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "false".to_string(),
        );
        let result =
            parse_load_biaser_config(&annotations).expect("penalize-failures=false should succeed");
        assert!(
            result.is_none(),
            "penalize-failures=false should return None"
        );
    }

    #[test]
    fn penalize_failures_absent_returns_none() {
        let annotations = BTreeMap::new();
        let result =
            parse_load_biaser_config(&annotations).expect("absent annotation should succeed");
        assert!(result.is_none(), "absent annotation should return None");
    }

    #[test]
    fn penalize_failures_invalid_value_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "maybe".to_string(),
        );
        let err = parse_load_biaser_config(&annotations).expect_err("maybe should be rejected");
        assert!(
            err.to_string().contains("'maybe'"),
            "error should quote the invalid value: {err}"
        );
    }

    #[test]
    fn penalize_failures_empty_value_rejected_with_visible_quotes() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "".to_string(),
        );
        let err =
            parse_load_biaser_config(&annotations).expect_err("empty value should be rejected");
        let msg = err.to_string();
        assert!(
            msg.contains("''"),
            "error should show empty value in quotes: {msg}"
        );
    }

    #[test]
    fn penalize_failures_whitespace_true_accepted() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            " true ".to_string(),
        );
        parse_load_biaser_config(&annotations)
            .expect("whitespace-padded 'true' should succeed")
            .expect("should return Some");
    }

    #[test]
    fn penalize_failures_whitespace_false_returns_none() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            " false ".to_string(),
        );
        let result = parse_load_biaser_config(&annotations)
            .expect("whitespace-padded 'false' should succeed");
        assert!(result.is_none());
    }

    #[test]
    fn penalize_failures_parsebool_truthy_accepted() {
        for value in ["True", "TRUE", "1", "t", "T"] {
            let mut annotations = BTreeMap::new();
            annotations.insert(
                "balancer.alpha.linkerd.io/penalize-failures".to_string(),
                value.to_string(),
            );
            parse_load_biaser_config(&annotations)
                .unwrap_or_else(|e| panic!("ParseBool truthy '{value}' should succeed: {e}"))
                .unwrap_or_else(|| panic!("ParseBool truthy '{value}' should return Some"));
        }
    }

    #[test]
    fn penalize_failures_parsebool_falsy_returns_none() {
        for value in ["False", "FALSE", "0", "f", "F"] {
            let mut annotations = BTreeMap::new();
            annotations.insert(
                "balancer.alpha.linkerd.io/penalize-failures".to_string(),
                value.to_string(),
            );
            let result = parse_load_biaser_config(&annotations)
                .unwrap_or_else(|e| panic!("ParseBool falsy '{value}' should succeed: {e}"));
            assert!(result.is_none(), "value '{value}' should return None");
        }
    }

    #[test]
    fn penalize_failures_non_parsebool_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "yes".to_string(),
        );
        let err = parse_load_biaser_config(&annotations).expect_err("yes should be rejected");
        assert!(
            err.to_string().contains("'yes'"),
            "error should quote the invalid value: {err}"
        );
    }

    const HONOR_RETRY_AFTER_KEY: &str =
        "balancer.alpha.linkerd.io/failure-accrual-honor-retry-after";

    #[test]
    fn honor_retry_after_true() {
        let mut annotations = BTreeMap::new();
        annotations.insert(HONOR_RETRY_AFTER_KEY.to_string(), "true".to_string());
        let honor = parse_bool_annotation(&annotations, HONOR_RETRY_AFTER_KEY)
            .expect("honor-retry-after=true should succeed");
        assert!(honor, "honor-retry-after=true should be true");
    }

    #[test]
    fn honor_retry_after_false() {
        let mut annotations = BTreeMap::new();
        annotations.insert(HONOR_RETRY_AFTER_KEY.to_string(), "false".to_string());
        let honor = parse_bool_annotation(&annotations, HONOR_RETRY_AFTER_KEY)
            .expect("honor-retry-after=false should succeed");
        assert!(!honor, "honor-retry-after=false should be false");
    }

    #[test]
    fn honor_retry_after_absent_is_false() {
        let annotations = BTreeMap::new();
        let honor = parse_bool_annotation(&annotations, HONOR_RETRY_AFTER_KEY)
            .expect("absent annotation should succeed");
        assert!(!honor, "absent annotation should be false");
    }

    #[test]
    fn honor_retry_after_whitespace_true_accepted() {
        let mut annotations = BTreeMap::new();
        annotations.insert(HONOR_RETRY_AFTER_KEY.to_string(), " true ".to_string());
        let honor = parse_bool_annotation(&annotations, HONOR_RETRY_AFTER_KEY)
            .expect("whitespace-padded 'true' should succeed");
        assert!(honor, "whitespace-padded 'true' should be true");
    }

    #[test]
    fn honor_retry_after_invalid_value_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(HONOR_RETRY_AFTER_KEY.to_string(), "sometimes".to_string());
        let err = parse_bool_annotation(&annotations, HONOR_RETRY_AFTER_KEY)
            .expect_err("sometimes should be rejected");
        assert!(
            err.to_string().contains("'sometimes'"),
            "error should quote the invalid value: {err}"
        );
    }

    #[test]
    fn load_biaser_penalty_explicit_value_parsed() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-biaser-penalty".to_string(),
            "2s".to_string(),
        );
        let config = parse_load_biaser_config(&annotations)
            .expect("explicit penalty should succeed")
            .expect("should return Some");
        assert_eq!(config.penalty, time::Duration::from_secs(2));
        assert_eq!(config.max_retry_after, DEFAULT_LOAD_BIASER_MAX_RETRY_AFTER);
    }

    #[test]
    fn load_biaser_max_retry_after_explicit_value_parsed() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-biaser-max-retry-after".to_string(),
            "60s".to_string(),
        );
        let config = parse_load_biaser_config(&annotations)
            .expect("explicit max-retry-after should succeed")
            .expect("should return Some");
        assert_eq!(config.penalty, DEFAULT_LOAD_BIASER_PENALTY);
        assert_eq!(config.max_retry_after, time::Duration::from_secs(60));
    }

    #[test]
    fn load_biaser_duration_whitespace_tolerated() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-biaser-penalty".to_string(),
            " 2s ".to_string(),
        );
        let config = parse_load_biaser_config(&annotations)
            .expect("whitespace-padded duration should succeed")
            .expect("should return Some");
        assert_eq!(config.penalty, time::Duration::from_secs(2));
    }

    #[test]
    fn load_biaser_invalid_duration_rejected() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-biaser-penalty".to_string(),
            "1.5s".to_string(),
        );
        assert!(
            parse_load_biaser_config(&annotations).is_err(),
            "fractional duration should be rejected"
        );
    }

    #[test]
    fn load_biaser_penalty_over_u32_millis_rejected() {
        // 50 days exceeds u32::MAX milliseconds (~49.7 days), which the proxy
        // would otherwise saturate.
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "true".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-biaser-penalty".to_string(),
            "50d".to_string(),
        );
        assert!(
            parse_load_biaser_config(&annotations).is_err(),
            "penalty over u32::MAX ms should be rejected"
        );
    }

    #[test]
    fn load_biaser_annotations_ignored_when_penalize_off() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/penalize-failures".to_string(),
            "false".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-biaser-penalty".to_string(),
            "2s".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/load-biaser-max-retry-after".to_string(),
            "60s".to_string(),
        );
        let result = parse_load_biaser_config(&annotations)
            .expect("disabled toggle should ignore duration annotations");
        assert!(
            result.is_none(),
            "penalize-failures=false should return None even with overrides present"
        );
    }

    #[test]
    fn mode_unified_uses_success_rate_defaults() {
        let annotations = accrual_annotations(&[]);
        let accrual = parse_accrual_config(&annotations)
            .expect("mode=unified should succeed")
            .expect("should return Some");
        let FailureAccrual::Unified {
            max_failures,
            backoff,
            success_rate,
        } = accrual
        else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(max_failures, 7);
        assert_eq!(backoff.min_penalty, time::Duration::from_secs(1));
        assert_eq!(backoff.max_penalty, time::Duration::from_secs(60));
        assert!((backoff.jitter - 0.5).abs() < f32::EPSILON);
        assert!((success_rate.threshold - 0.8).abs() < f64::EPSILON);
        assert_eq!(success_rate.window, time::Duration::from_secs(10));
        assert_eq!(success_rate.min_requests, 5);
    }

    #[test]
    fn mode_unified_with_explicit_values() {
        let annotations = accrual_annotations(&[
            (
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold",
                "0.9",
            ),
            (
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-window",
                "15s",
            ),
            (
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-min-requests",
                "10",
            ),
            (
                "balancer.linkerd.io/failure-accrual-consecutive-max-failures",
                "3",
            ),
            (
                "balancer.linkerd.io/failure-accrual-consecutive-min-penalty",
                "2s",
            ),
        ]);
        let accrual = parse_accrual_config(&annotations)
            .expect("mode=unified should succeed")
            .expect("should return Some");
        let FailureAccrual::Unified {
            max_failures,
            backoff,
            success_rate,
        } = accrual
        else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(max_failures, 3);
        assert_eq!(backoff.min_penalty, time::Duration::from_secs(2));
        assert!((success_rate.threshold - 0.9).abs() < f64::EPSILON);
        assert_eq!(success_rate.window, time::Duration::from_secs(15));
        assert_eq!(success_rate.min_requests, 10);
    }

    #[test]
    fn success_rate_threshold_whitespace_accepted() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold",
            " 0.9 ",
        )]);
        let accrual = parse_accrual_config(&annotations)
            .expect("whitespace-padded threshold should succeed")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert!((success_rate.threshold - 0.9).abs() < f64::EPSILON);
    }

    #[test]
    fn success_rate_min_requests_whitespace_accepted() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-min-requests",
            " 5 ",
        )]);
        let accrual = parse_accrual_config(&annotations)
            .expect("whitespace-padded min-requests should succeed")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(success_rate.min_requests, 5);
    }

    #[test]
    fn mode_consecutive_unchanged() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.linkerd.io/failure-accrual".to_string(),
            "consecutive".to_string(),
        );
        let accrual = parse_accrual_config(&annotations)
            .expect("mode=consecutive should succeed")
            .expect("should return Some");
        assert_eq!(
            accrual,
            FailureAccrual::Consecutive {
                max_failures: 7,
                backoff: Backoff {
                    min_penalty: time::Duration::from_secs(1),
                    max_penalty: time::Duration::from_secs(60),
                    jitter: 0.5,
                },
            }
        );
    }

    #[test]
    fn unified_threshold_out_of_range_rejected() {
        for value in ["-0.1", "1.5"] {
            let annotations = accrual_annotations(&[(
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold",
                value,
            )]);
            let err = parse_accrual_config(&annotations)
                .expect_err("out-of-range threshold should be rejected");
            assert!(
                err.to_string().contains("between 0.0 and 1.0"),
                "error should mention the valid range: {err}"
            );
        }
    }

    #[test]
    fn boolean_accrual_modes_rejected() {
        for mode in ["true", "false"] {
            let mut annotations = BTreeMap::new();
            annotations.insert(
                "balancer.linkerd.io/failure-accrual".to_string(),
                mode.to_string(),
            );
            let err =
                parse_accrual_config(&annotations).expect_err("boolean modes should be rejected");
            assert!(
                err.to_string().contains("unsupported failure accrual mode"),
                "error should mention the unsupported mode: {err}"
            );
        }
    }

    #[test]
    fn window_zero_rejected() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-window",
            "0s",
        )]);
        let err = parse_accrual_config(&annotations).expect_err("window=0 should be rejected");
        assert!(
            err.to_string()
                .contains("success-rate-window must be greater than zero"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn window_below_proxy_bound_rejected() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-window",
            "9ms",
        )]);
        let err =
            parse_accrual_config(&annotations).expect_err("window below 10ms should be rejected");
        assert!(
            err.to_string().contains("10ms"),
            "error should mention the bound: {err}"
        );
    }

    #[test]
    fn window_at_proxy_bound_accepted() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-window",
            "10ms",
        )]);
        let accrual = parse_accrual_config(&annotations)
            .expect("window at the bound should parse")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(success_rate.window, MIN_SUCCESS_RATE_WINDOW);
    }

    #[test]
    fn min_requests_zero_warns_but_succeeds() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-min-requests",
            "0",
        )]);
        let accrual = parse_accrual_config(&annotations)
            .expect("min_requests=0 should succeed with a warning")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(success_rate.min_requests, 0);
    }

    #[test]
    fn threshold_zero_warns_but_succeeds() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold",
            "0.0",
        )]);
        let accrual = parse_accrual_config(&annotations)
            .expect("threshold=0.0 should succeed with a warning")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert!((success_rate.threshold - 0.0).abs() < f64::EPSILON);
    }

    #[test]
    fn sub_second_window_warns_but_succeeds() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-window",
            "500ms",
        )]);
        let accrual = parse_accrual_config(&annotations)
            .expect("sub-second window should succeed with a warning")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(success_rate.window, time::Duration::from_millis(500));
    }

    #[test]
    fn success_rate_annotations_ignored_under_consecutive() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.linkerd.io/failure-accrual".to_string(),
            "consecutive".to_string(),
        );
        annotations.insert(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold".to_string(),
            "0.9".to_string(),
        );
        let accrual = parse_accrual_config(&annotations)
            .expect("consecutive mode should keep the breaker despite success-rate annotations")
            .expect("should return Some");
        assert!(
            matches!(accrual, FailureAccrual::Consecutive { .. }),
            "expected consecutive accrual, got: {accrual:?}"
        );
    }

    #[test]
    fn mode_consecutive_without_success_rate_succeeds() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.linkerd.io/failure-accrual".to_string(),
            "consecutive".to_string(),
        );
        let accrual = parse_accrual_config(&annotations)
            .expect("consecutive without success-rate annotations should succeed")
            .expect("should return Some");
        assert!(
            matches!(accrual, FailureAccrual::Consecutive { .. }),
            "expected consecutive accrual, got: {accrual:?}"
        );
    }

    #[test]
    fn min_requests_above_proxy_bound_rejected() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-min-requests",
            "1000001",
        )]);
        let err = parse_accrual_config(&annotations)
            .expect_err("min-requests above 1000000 should be rejected");
        assert!(
            err.to_string().contains("1000000"),
            "error should mention the bound: {err}"
        );
    }

    #[test]
    fn min_requests_at_proxy_bound_accepted() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-min-requests",
            "1000000",
        )]);
        let accrual = parse_accrual_config(&annotations)
            .expect("min-requests at the bound should parse")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(success_rate.min_requests, MAX_SUCCESS_RATE_MIN_REQUESTS);
    }

    #[test]
    fn unified_threshold_zero_accepts_sub_10ms_window() {
        let annotations = accrual_annotations(&[
            (
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold",
                "0",
            ),
            (
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-window",
                "5ms",
            ),
        ]);
        let accrual = parse_accrual_config(&annotations)
            .expect("threshold 0 should gate off the 10ms window floor")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(success_rate.window, time::Duration::from_millis(5));
    }

    #[test]
    fn unified_threshold_zero_accepts_large_min_requests() {
        let annotations = accrual_annotations(&[
            (
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold",
                "0",
            ),
            (
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-min-requests",
                "2000000",
            ),
        ]);
        let accrual = parse_accrual_config(&annotations)
            .expect("threshold 0 should gate off the min-requests ceiling")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(success_rate.min_requests, 2_000_000);
    }

    #[test]
    fn success_rate_keys_without_mode_returns_none() {
        let mut annotations = BTreeMap::new();
        annotations.insert(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold".to_string(),
            "0.9".to_string(),
        );
        let result = parse_accrual_config(&annotations)
            .expect("success-rate annotations without a mode should succeed");
        assert!(
            result.is_none(),
            "no mode means no accrual, got: {result:?}"
        );
    }

    #[test]
    fn unified_unknown_success_rate_key_ignored() {
        let annotations = accrual_annotations(&[(
            "balancer.alpha.linkerd.io/failure-accrual-success-rate-thresold",
            "0.99",
        )]);
        let accrual = parse_accrual_config(&annotations)
            .expect("a typo'd success-rate key should be ignored, not rejected")
            .expect("should return Some");
        let FailureAccrual::Unified { success_rate, .. } = accrual else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert!(
            (success_rate.threshold - DEFAULT_SUCCESS_RATE_THRESHOLD).abs() < f64::EPSILON,
            "typo'd key must not be parsed; threshold should be the default"
        );
    }

    #[test]
    fn unified_both_dimensions_disabled_parses() {
        let annotations = accrual_annotations(&[
            (
                "balancer.alpha.linkerd.io/failure-accrual-success-rate-threshold",
                "0",
            ),
            (
                "balancer.linkerd.io/failure-accrual-consecutive-max-failures",
                "0",
            ),
        ]);
        let accrual = parse_accrual_config(&annotations)
            .expect("both dimensions disabled should still parse")
            .expect("should return Some");
        let FailureAccrual::Unified {
            max_failures,
            success_rate,
            ..
        } = accrual
        else {
            panic!("expected unified accrual, got: {accrual:?}");
        };
        assert_eq!(max_failures, 0);
        assert!((success_rate.threshold - 0.0).abs() < f64::EPSILON);
    }
}
