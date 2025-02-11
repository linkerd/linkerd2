use crate::{
    ratelimit,
    resource_id::{NamespaceGroupKindName, ResourceId},
    routes,
    service::Service,
};

use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use chrono::{offset::Utc, DateTime};
use kubert::lease::Claim;
use linkerd_policy_controller_core::{routes::GroupKindName, IpNet, POLICY_CONTROLLER_NAME};
use linkerd_policy_controller_k8s_api::{
    self as k8s, gateway,
    policy::{self, Cidr, Network},
    NamespaceResourceScope, Resource, ResourceExt, Time,
};
use parking_lot::RwLock;
use prometheus_client::{
    metrics::{counter::Counter, histogram::Histogram},
    registry::{Registry, Unit},
};
use serde::de::DeserializeOwned;
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::{
    sync::{mpsc, watch::Receiver},
    time::{self, Duration},
};

pub(crate) const POLICY_API_GROUP: &str = "policy.linkerd.io";
pub(crate) const GATEWAY_API_GROUP: &str = "gateway.networking.k8s.io";

mod conditions {
    pub const RESOLVED_REFS: &str = "ResolvedRefs";
    pub const ACCEPTED: &str = "Accepted";
}
mod reasons {
    pub const RESOLVED_REFS: &str = "ResolvedRefs";
    pub const BACKEND_NOT_FOUND: &str = "BackendNotFound";
    pub const INVALID_KIND: &str = "InvalidKind";
    pub const NO_MATCHING_PARENT: &str = "NoMatchingParent";
    pub const NO_MATCHING_TARGET: &str = "NoMatchingTarget";
    pub const ROUTE_REASON_CONFLICTED: &str = "RouteReasonConflicted";
    pub const RATELIMIT_REASON_ALREADY_EXISTS: &str = "RateLimitReasonAlreadyExists";
    pub const EGRESS_NET_REASON_OVERLAP: &str = "EgressReasonNetworkOverlap";
}

mod cond_statuses {
    pub const STATUS_TRUE: &str = "True";
    pub const STATUS_FALSE: &str = "False";
}

pub type SharedIndex = Arc<RwLock<Index>>;

pub struct Controller {
    claims: Receiver<Arc<Claim>>,
    client: k8s::Client,
    name: String,
    updates: mpsc::Receiver<Update>,
    patch_timeout: Duration,

    metrics: ControllerMetrics,
}

pub struct ControllerMetrics {
    patch_succeeded: Counter,
    patch_failed: Counter,
    patch_timeout: Counter,
    patch_duration: Histogram,
    patch_dequeues: Counter,
    patch_drops: Counter,
}

pub struct Index {
    /// Used to compare against the current claim's claimant to determine if
    /// this policy controller is the leader.
    name: String,

    /// Used in the IndexNamespacedResource trait methods to check who the
    /// current leader is and if updates should be sent to the Controller.
    claims: Receiver<Arc<Claim>>,
    updates: mpsc::Sender<Update>,

    /// Maps route ids to a list of their parent and backend refs,
    /// regardless of if those parents have accepted the route.
    http_route_refs: HashMap<NamespaceGroupKindName, HTTPRouteRef>,
    grpc_route_refs: HashMap<NamespaceGroupKindName, GRPCRouteRef>,
    tcp_route_refs: HashMap<NamespaceGroupKindName, TCPRouteRef>,
    tls_route_refs: HashMap<NamespaceGroupKindName, TLSRouteRef>,

    /// Maps rate limit ids to a list of details about these rate limits.
    ratelimits: HashMap<ResourceId, HttpLocalRateLimitPolicyRef>,

    /// Maps egress network ids to a list of details about these networks.
    egress_networks: HashMap<ResourceId, EgressNetworkRef>,

    servers: HashSet<ResourceId>,
    services: HashMap<ResourceId, Service>,
    cluster_networks: Vec<Cidr>,

    metrics: IndexMetrics,
}

pub struct IndexMetrics {
    patch_enqueues: Counter,
    patch_channel_full: Counter,
}

#[derive(Clone, PartialEq, Debug)]
pub(crate) struct RouteRef<S> {
    pub(crate) parents: Vec<routes::ParentReference>,
    pub(crate) backends: Vec<routes::BackendReference>,
    pub(crate) statuses: Vec<S>,
}

pub(crate) type HTTPRouteRef = RouteRef<gateway::httproutes::HTTPRouteStatus>;
pub(crate) type GRPCRouteRef = RouteRef<gateway::grpcroutes::GRPCRouteStatus>;
pub(crate) type TLSRouteRef = RouteRef<gateway::tlsroutes::TLSRouteStatus>;
pub(crate) type TCPRouteRef = RouteRef<gateway::tcproutes::TCPRouteStatus>;

#[derive(Clone, PartialEq, Debug)]
struct HttpLocalRateLimitPolicyRef {
    creation_timestamp: Option<DateTime<Utc>>,
    target_ref: ratelimit::TargetReference,
    status_conditions: Vec<k8s::Condition>,
}

#[derive(Clone, PartialEq, Debug)]
struct EgressNetworkRef {
    networks: Vec<Network>,
    status_conditions: Vec<k8s::Condition>,
}

impl EgressNetworkRef {
    fn is_accepted(&self) -> bool {
        self.status_conditions
            .iter()
            .any(|c| c.type_ == *conditions::ACCEPTED && c.status == *cond_statuses::STATUS_TRUE)
    }
}

#[derive(Debug, PartialEq)]
pub struct Update {
    pub id: NamespaceGroupKindName,
    pub patch: k8s::Patch<serde_json::Value>,
}

impl ControllerMetrics {
    pub fn register(prom: &mut Registry) -> Self {
        let patch_succeeded = Counter::default();
        prom.register(
            "patches",
            "Count of successful patch operations",
            patch_succeeded.clone(),
        );

        let patch_failed = Counter::default();
        prom.register(
            "patch_api_errors",
            "Count of patch operations that failed with an API error",
            patch_failed.clone(),
        );

        let patch_timeout = Counter::default();
        prom.register(
            "patch_timeouts",
            "Count of patch operations that did not complete within the timeout",
            patch_timeout.clone(),
        );

        let patch_duration =
            Histogram::new([0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0].into_iter());
        prom.register_with_unit(
            "patch_duration",
            "Histogram of time taken to apply patch operations",
            Unit::Seconds,
            patch_duration.clone(),
        );

        let patch_dequeues = Counter::default();
        prom.register(
            "patch_dequeues",
            "Count of patches dequeued from the updates channel",
            patch_dequeues.clone(),
        );

        let patch_drops = Counter::default();
        prom.register(
            "patch_drops",
            "Count of patches dropped because we are not the leader",
            patch_drops.clone(),
        );

        Self {
            patch_succeeded,
            patch_failed,
            patch_timeout,
            patch_duration,
            patch_dequeues,
            patch_drops,
        }
    }
}

impl IndexMetrics {
    pub fn register(prom: &mut Registry) -> Self {
        let patch_enqueues = Counter::default();
        prom.register(
            "patch_enqueues",
            "Count of patches enqueued to the updates channel",
            patch_enqueues.clone(),
        );

        let patch_channel_full = Counter::default();
        prom.register(
            "patch_enqueue_overflows",
            "Count of patches dropped because the updates channel is full",
            patch_channel_full.clone(),
        );

        Self {
            patch_enqueues,
            patch_channel_full,
        }
    }
}

impl Controller {
    pub fn new(
        claims: Receiver<Arc<Claim>>,
        client: k8s::Client,
        name: String,
        updates: mpsc::Receiver<Update>,
        patch_timeout: Duration,
        metrics: ControllerMetrics,
    ) -> Self {
        Self {
            claims,
            client,
            name,
            updates,
            patch_timeout,
            metrics,
        }
    }

    /// Process updates received from the index; each update is a patch that
    /// should be applied to update the status of a route. A patch should
    /// only be applied if we are the holder of the write lease.
    pub async fn run(mut self) {
        // Select between the write lease claim changing and receiving updates
        // from the index. If the lease claim changes, then check if we are
        // now the leader. If so, we should apply the patches received;
        // otherwise, we should drain the updates queue but not apply any
        // patches since another policy controller is responsible for that.
        let mut was_leader = false;
        loop {
            // Refresh the state of the lease on each iteration to ensure we're
            // checking expiration.
            let is_leader = self.claims.borrow_and_update().is_current_for(&self.name);
            if was_leader != is_leader {
                tracing::info!(leader=%is_leader, "Status controller leadership change");
            }
            was_leader = is_leader;

            tokio::select! {
                biased;
                res = self.claims.changed() => {
                    res.expect("Claims watch must not be dropped");
                }

                Some(Update { id, patch}) = self.updates.recv() => {
                    self.metrics.patch_dequeues.inc();
                    // If this policy controller is not the leader, it should
                    // process through the updates queue but not actually patch
                    // any resources.
                    if is_leader {
                        if id.is_a::<policy::HttpRoute>() {
                            self.patch::<policy::HttpRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.is_a::<gateway::httproutes::HTTPRoute>() {
                            self.patch::<gateway::httproutes::HTTPRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.is_a::<gateway::grpcroutes::GRPCRoute>() {
                            self.patch::<gateway::grpcroutes::GRPCRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.is_a::<gateway::tcproutes::TCPRoute>() {
                            self.patch::<gateway::tcproutes::TCPRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.is_a::<gateway::tlsroutes::TLSRoute>() {
                            self.patch::<gateway::tlsroutes::TLSRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.is_a::<policy::HttpLocalRateLimitPolicy>() {
                            self.patch::<policy::HttpLocalRateLimitPolicy>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.is_a::<policy::EgressNetwork>() {
                            self.patch::<policy::EgressNetwork>(&id.gkn.name, &id.namespace, patch).await;
                        }
                    } else {
                        tracing::debug!(?id, "Dropping patch because we are not the leader");
                        self.metrics.patch_drops.inc();
                    }
                }
            }
        }
    }

    #[tracing::instrument(
        level = tracing::Level::ERROR,
        skip(self, patch),
        fields(
            group=%K::group(&Default::default()),
            kind=%K::kind(&Default::default()),
        ),
    )]
    async fn patch<K>(&self, name: &str, namespace: &str, patch: k8s::Patch<serde_json::Value>)
    where
        K: Resource<Scope = NamespaceResourceScope>,
        <K as Resource>::DynamicType: Default,
        K: DeserializeOwned,
    {
        tracing::trace!(?patch);
        let api = k8s::Api::<K>::namespaced(self.client.clone(), namespace);
        let patch_params = k8s::PatchParams::apply(POLICY_CONTROLLER_NAME);
        let start = time::Instant::now();
        let result = time::timeout(
            self.patch_timeout,
            api.patch_status(name, &patch_params, &patch),
        )
        .await;
        let elapsed = start.elapsed();
        tracing::trace!(?elapsed);
        match result {
            Ok(Ok(_)) => {
                self.metrics.patch_succeeded.inc();
                self.metrics.patch_duration.observe(elapsed.as_secs_f64());
                tracing::info!("Patched status");
            }
            Ok(Err(error)) => {
                self.metrics.patch_failed.inc();
                self.metrics.patch_duration.observe(elapsed.as_secs_f64());
                tracing::error!(%error);
            }
            Err(_) => {
                self.metrics.patch_timeout.inc();
                tracing::error!("Timed out");
            }
        }
    }
}

impl Index {
    pub fn shared(
        name: impl ToString,
        claims: Receiver<Arc<Claim>>,
        updates: mpsc::Sender<Update>,
        metrics: IndexMetrics,
        cluster_networks: Vec<IpNet>,
    ) -> SharedIndex {
        let cluster_networks = cluster_networks.into_iter().map(Into::into).collect();
        Arc::new(RwLock::new(Self {
            name: name.to_string(),
            claims,
            updates,
            http_route_refs: HashMap::new(),
            grpc_route_refs: HashMap::new(),
            tls_route_refs: HashMap::new(),
            tcp_route_refs: HashMap::new(),
            ratelimits: HashMap::new(),
            egress_networks: HashMap::new(),
            servers: HashSet::new(),
            services: HashMap::new(),
            metrics,
            cluster_networks,
        }))
    }

    /// When the write leaseholder changes or a time duration has elapsed,
    /// the index reconciles the statuses for all routes on the cluster.
    ///
    /// This reconciliation loop ensures that if errors occur when the
    /// Controller applies patches or the write leaseholder changes, all
    /// routes have an up-to-date status.
    pub async fn run(index: Arc<RwLock<Self>>, reconciliation_period: Duration) {
        // Extract what we need from the index so we don't need to lock it for
        // housekeeping.
        let (instance, mut claims) = {
            let idx = index.read();
            (idx.name.clone(), idx.claims.clone())
        };

        // The timer is reset when this instance becomes the leader and it is
        // polled as long as it is the leader. The timer ensures that
        // reconciliation happens at consistent intervals after leadership is
        // acquired.
        let mut timer = time::interval(reconciliation_period);
        timer.set_missed_tick_behavior(time::MissedTickBehavior::Delay);

        let mut was_leader = false;
        loop {
            // Refresh the state of the lease on each iteration to ensure we're
            // checking expiration.
            let is_leader = claims.borrow_and_update().is_current_for(&instance);
            if is_leader && !was_leader {
                tracing::debug!("Became leader; resetting timer");
                timer.reset_immediately();
            }
            was_leader = is_leader;

            tokio::select! {
                // Eagerly process claim updates to track leadership changes. If
                // the claim changes, refesh the leadership status.
                biased;
                res = claims.changed() => {
                    res.expect("Claims watch must not be dropped");
                    if tracing::enabled!(tracing::Level::TRACE) {
                        let c = claims.borrow();
                        tracing::trace!(claim=?*c, "Changed");
                    }
                }

                // Only wait for the timer if this instance is the leader.
                _ = timer.tick(), if is_leader => {
                    index.read().reconcile_if_leader();
                }
            }
        }
    }

    // If the network is new or its contents have changed, return true, so that a
    // patch is generated; otherwise return false.
    fn update_egress_net(&mut self, id: ResourceId, net: &EgressNetworkRef) -> bool {
        match self.egress_networks.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(net.clone());
            }
            Entry::Occupied(mut entry) => {
                if entry.get() == net {
                    return false;
                }
                entry.insert(net.clone());
            }
        }
        true
    }

    // If the route is new or its contents have changed, return true, so that a
    // patch is generated; otherwise return false.
    pub(crate) fn update_http_route(
        &mut self,
        id: NamespaceGroupKindName,
        route: &HTTPRouteRef,
    ) -> bool {
        match self.http_route_refs.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(route.clone());
            }
            Entry::Occupied(mut entry) => {
                if entry.get() == route {
                    return false;
                }
                entry.insert(route.clone());
            }
        }
        true
    }

    // If the route is new or its contents have changed, return true, so that a
    // patch is generated; otherwise return false.
    pub(crate) fn update_grpc_route(
        &mut self,
        id: NamespaceGroupKindName,
        route: &GRPCRouteRef,
    ) -> bool {
        match self.grpc_route_refs.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(route.clone());
            }
            Entry::Occupied(mut entry) => {
                if entry.get() == route {
                    return false;
                }
                entry.insert(route.clone());
            }
        }
        true
    }

    // If the route is new or its contents have changed, return true, so that a
    // patch is generated; otherwise return false.
    pub(crate) fn update_tls_route(
        &mut self,
        id: NamespaceGroupKindName,
        route: &TLSRouteRef,
    ) -> bool {
        match self.tls_route_refs.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(route.clone());
            }
            Entry::Occupied(mut entry) => {
                if entry.get() == route {
                    return false;
                }
                entry.insert(route.clone());
            }
        }
        true
    }

    // If the route is new or its contents have changed, return true, so that a
    // patch is generated; otherwise return false.
    pub(crate) fn update_tcp_route(
        &mut self,
        id: NamespaceGroupKindName,
        route: &TCPRouteRef,
    ) -> bool {
        match self.tcp_route_refs.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(route.clone());
            }
            Entry::Occupied(mut entry) => {
                if entry.get() == route {
                    return false;
                }
                entry.insert(route.clone());
            }
        }
        true
    }

    // If the rate limit is new or its contents have changed, return true, so that a patch is
    // generated; otherwise return false.
    fn update_ratelimit(
        &mut self,
        id: ResourceId,
        ratelimit: &HttpLocalRateLimitPolicyRef,
    ) -> bool {
        match self.ratelimits.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(ratelimit.clone());
            }
            Entry::Occupied(mut entry) => {
                if entry.get() == ratelimit {
                    return false;
                }
                entry.insert(ratelimit.clone());
            }
        }
        true
    }

    // This method determines whether a parent that a route attempts to
    // attach to has any routes attached that are in conflict with the one
    // that we are about to attach. This is done following the logs outlined in:
    // https://gateway-api.sigs.k8s.io/geps/gep-1426/#route-types
    pub fn parent_has_conflicting_routes(
        &self,
        parent_ref: &routes::ParentReference,
        candidate_kind: &str,
    ) -> bool {
        let grpc_kind = gateway::grpcroutes::GRPCRoute::kind(&());
        let http_kind = gateway::httproutes::HTTPRoute::kind(&());
        let tls_kind = gateway::tlsroutes::TLSRoute::kind(&());
        let tcp_kind = gateway::tcproutes::TCPRoute::kind(&());

        if *candidate_kind == grpc_kind {
            false
        } else if *candidate_kind == http_kind {
            self.grpc_route_refs
                .values()
                .any(|route| route.parents.contains(parent_ref))
        } else if *candidate_kind == tls_kind {
            self.grpc_route_refs
                .values()
                .any(|route| route.parents.contains(parent_ref))
                || self
                    .http_route_refs
                    .values()
                    .any(|route| route.parents.contains(parent_ref))
        } else if *candidate_kind == tcp_kind {
            self.grpc_route_refs
                .values()
                .any(|route| route.parents.contains(parent_ref))
                || self
                    .http_route_refs
                    .values()
                    .any(|route| route.parents.contains(parent_ref))
                || self
                    .tls_route_refs
                    .values()
                    .any(|route| route.parents.contains(parent_ref))
        } else {
            false
        }
    }

    fn parent_condition_server(
        &self,
        server: &ResourceId,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
    ) -> k8s::Condition {
        if self.servers.contains(server) {
            if self.parent_has_conflicting_routes(parent_ref, &id.gkn.kind) {
                route_conflicted()
            } else {
                accepted()
            }
        } else {
            no_matching_parent()
        }
    }

    fn parent_condition_service(
        &self,
        service: &ResourceId,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
    ) -> k8s::Condition {
        // service is a valid parent if it exists and it has a cluster_ip.
        match self.services.get(service) {
            Some(svc) if svc.valid_parent_service() => {
                if self.parent_has_conflicting_routes(parent_ref, &id.gkn.kind) {
                    route_conflicted()
                } else {
                    accepted()
                }
            }
            Some(_svc) => headless_parent(),
            None => no_matching_parent(),
        }
    }

    fn parent_condition_egress_network(
        &self,
        egress_net: &ResourceId,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
    ) -> k8s::Condition {
        // egress network is a valid parent if it exists and is accepted.
        match self.egress_networks.get(egress_net) {
            Some(egress_net) if egress_net.is_accepted() => {
                if self.parent_has_conflicting_routes(parent_ref, &id.gkn.kind) {
                    route_conflicted()
                } else {
                    accepted()
                }
            }
            Some(_) => egress_net_not_accepted(),
            None => no_matching_parent(),
        }
    }

    fn grpc_parent_status(
        &self,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
        backend_condition: k8s::Condition,
    ) -> Option<gateway::grpcroutes::GRPCRouteStatusParents> {
        match parent_ref {
            routes::ParentReference::Server(server) => {
                let condition = self.parent_condition_server(server, id, parent_ref);
                Some(gateway::grpcroutes::GRPCRouteStatusParents {
                    parent_ref: gateway::grpcroutes::GRPCRouteStatusParentsParentRef {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(server.namespace.clone()),
                        name: server.name.clone(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition]),
                })
            }

            routes::ParentReference::Service(service, port) => {
                let condition = self.parent_condition_service(service, id, parent_ref);
                Some(gateway::grpcroutes::GRPCRouteStatusParents {
                    parent_ref: gateway::grpcroutes::GRPCRouteStatusParentsParentRef {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        namespace: Some(service.namespace.clone()),
                        name: service.name.clone(),
                        section_name: None,
                        port: port.map(Into::into),
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition, backend_condition]),
                })
            }

            routes::ParentReference::EgressNetwork(egress_net, port) => {
                let condition = self.parent_condition_egress_network(egress_net, id, parent_ref);
                Some(gateway::grpcroutes::GRPCRouteStatusParents {
                    parent_ref: gateway::grpcroutes::GRPCRouteStatusParentsParentRef {
                        group: Some("policy.linkerd.io".to_string()),
                        kind: Some("EgressNetwork".to_string()),
                        namespace: Some(egress_net.namespace.clone()),
                        name: egress_net.name.clone(),
                        section_name: None,
                        port: port.map(Into::into),
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition, backend_condition]),
                })
            }
            routes::ParentReference::UnknownKind => None,
        }
    }

    fn http_parent_status(
        &self,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
        backend_condition: k8s::Condition,
    ) -> Option<gateway::httproutes::HTTPRouteStatusParents> {
        match parent_ref {
            routes::ParentReference::Server(server) => {
                let condition = self.parent_condition_server(server, id, parent_ref);
                Some(gateway::httproutes::HTTPRouteStatusParents {
                    parent_ref: gateway::httproutes::HTTPRouteStatusParentsParentRef {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(server.namespace.clone()),
                        name: server.name.clone(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition]),
                })
            }

            routes::ParentReference::Service(service, port) => {
                let condition = self.parent_condition_service(service, id, parent_ref);
                Some(gateway::httproutes::HTTPRouteStatusParents {
                    parent_ref: gateway::httproutes::HTTPRouteStatusParentsParentRef {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        namespace: Some(service.namespace.clone()),
                        name: service.name.clone(),
                        section_name: None,
                        port: port.map(Into::into),
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition, backend_condition]),
                })
            }

            routes::ParentReference::EgressNetwork(egress_net, port) => {
                let condition = self.parent_condition_egress_network(egress_net, id, parent_ref);
                Some(gateway::httproutes::HTTPRouteStatusParents {
                    parent_ref: gateway::httproutes::HTTPRouteStatusParentsParentRef {
                        group: Some("policy.linkerd.io".to_string()),
                        kind: Some("EgressNetwork".to_string()),
                        namespace: Some(egress_net.namespace.clone()),
                        name: egress_net.name.clone(),
                        section_name: None,
                        port: port.map(Into::into),
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition, backend_condition]),
                })
            }
            routes::ParentReference::UnknownKind => None,
        }
    }

    fn tls_parent_status(
        &self,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
        backend_condition: k8s::Condition,
    ) -> Option<gateway::tlsroutes::TLSRouteStatusParents> {
        match parent_ref {
            routes::ParentReference::Server(server) => {
                let condition = self.parent_condition_server(server, id, parent_ref);
                Some(gateway::tlsroutes::TLSRouteStatusParents {
                    parent_ref: gateway::tlsroutes::TLSRouteStatusParentsParentRef {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(server.namespace.clone()),
                        name: server.name.clone(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition]),
                })
            }

            routes::ParentReference::Service(service, port) => {
                let condition = self.parent_condition_service(service, id, parent_ref);
                Some(gateway::tlsroutes::TLSRouteStatusParents {
                    parent_ref: gateway::tlsroutes::TLSRouteStatusParentsParentRef {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        namespace: Some(service.namespace.clone()),
                        name: service.name.clone(),
                        section_name: None,
                        port: port.map(Into::into),
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition, backend_condition]),
                })
            }

            routes::ParentReference::EgressNetwork(egress_net, port) => {
                let condition = self.parent_condition_egress_network(egress_net, id, parent_ref);
                Some(gateway::tlsroutes::TLSRouteStatusParents {
                    parent_ref: gateway::tlsroutes::TLSRouteStatusParentsParentRef {
                        group: Some("policy.linkerd.io".to_string()),
                        kind: Some("EgressNetwork".to_string()),
                        namespace: Some(egress_net.namespace.clone()),
                        name: egress_net.name.clone(),
                        section_name: None,
                        port: port.map(Into::into),
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition, backend_condition]),
                })
            }
            routes::ParentReference::UnknownKind => None,
        }
    }

    fn tcp_parent_status(
        &self,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
        backend_condition: k8s::Condition,
    ) -> Option<gateway::tcproutes::TCPRouteStatusParents> {
        match parent_ref {
            routes::ParentReference::Server(server) => {
                let condition = self.parent_condition_server(server, id, parent_ref);
                Some(gateway::tcproutes::TCPRouteStatusParents {
                    parent_ref: gateway::tcproutes::TCPRouteStatusParentsParentRef {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(server.namespace.clone()),
                        name: server.name.clone(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition]),
                })
            }

            routes::ParentReference::Service(service, port) => {
                let condition = self.parent_condition_service(service, id, parent_ref);
                Some(gateway::tcproutes::TCPRouteStatusParents {
                    parent_ref: gateway::tcproutes::TCPRouteStatusParentsParentRef {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        namespace: Some(service.namespace.clone()),
                        name: service.name.clone(),
                        section_name: None,
                        port: port.map(Into::into),
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition, backend_condition]),
                })
            }

            routes::ParentReference::EgressNetwork(egress_net, port) => {
                let condition = self.parent_condition_egress_network(egress_net, id, parent_ref);
                Some(gateway::tcproutes::TCPRouteStatusParents {
                    parent_ref: gateway::tcproutes::TCPRouteStatusParentsParentRef {
                        group: Some("policy.linkerd.io".to_string()),
                        kind: Some("EgressNetwork".to_string()),
                        namespace: Some(egress_net.namespace.clone()),
                        name: egress_net.name.clone(),
                        section_name: None,
                        port: port.map(Into::into),
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: Some(vec![condition, backend_condition]),
                })
            }
            routes::ParentReference::UnknownKind => None,
        }
    }

    fn backend_condition(
        &self,
        parent_ref: &routes::ParentReference,
        backend_refs: &[routes::BackendReference],
    ) -> k8s::Condition {
        for backend_ref in backend_refs.iter() {
            match backend_ref {
                routes::BackendReference::Unknown => {
                    // If even one backend has a reference to an unknown / unsupported
                    // reference, return invalid backend condition
                    return invalid_backend_kind("");
                }

                routes::BackendReference::Service(service) => {
                    if !self.services.contains_key(service) {
                        return backend_not_found();
                    }
                }
                routes::BackendReference::EgressNetwork(egress_net) => {
                    if !self.egress_networks.contains_key(egress_net) {
                        return backend_not_found();
                    }

                    match parent_ref {
                        routes::ParentReference::EgressNetwork(parent_resource, _)
                            if parent_resource == egress_net =>
                        {
                            continue;
                        }
                        _ => {
                            let message =
                            "EgressNetwork backend needs to be on a route that has an EgressNetwork parent";
                            return invalid_backend_kind(message);
                        }
                    }
                }
            }
        }

        resolved_refs()
    }

    fn make_http_route_patch(
        &self,
        id: &NamespaceGroupKindName,
        route: &HTTPRouteRef,
    ) -> Option<k8s::Patch<serde_json::Value>> {
        // To preserve any statuses from other controllers, we copy those
        // statuses.
        let unowned_statuses = route
            .statuses
            .iter()
            .flat_map(|status| status.parents.clone())
            .filter(|status| status.controller_name != POLICY_CONTROLLER_NAME);

        // Compute a status for each parent_ref which has a kind we support.
        let parent_statuses = route.parents.iter().filter_map(|parent_ref| {
            let backend_condition = self.backend_condition(parent_ref, &route.backends);
            self.http_parent_status(id, parent_ref, backend_condition.clone())
        });

        let all_statuses = unowned_statuses.chain(parent_statuses).collect::<Vec<_>>();
        let route_statuses = route
            .statuses
            .iter()
            .flat_map(|status| status.parents.clone())
            .collect::<Vec<_>>();
        if eq_time_insensitive_http_route_parent_statuses(&all_statuses, &route_statuses) {
            return None;
        }

        let status = gateway::httproutes::HTTPRouteStatus {
            parents: all_statuses,
        };

        make_patch(id, status)
    }

    fn make_grpc_route_patch(
        &self,
        id: &NamespaceGroupKindName,
        route: &GRPCRouteRef,
    ) -> Option<k8s::Patch<serde_json::Value>> {
        // To preserve any statuses from other controllers, we copy those
        // statuses.
        let unowned_statuses = route
            .statuses
            .iter()
            .flat_map(|status| status.parents.clone())
            .filter(|status| status.controller_name != POLICY_CONTROLLER_NAME);

        // Compute a status for each parent_ref which has a kind we support.
        let parent_statuses = route.parents.iter().filter_map(|parent_ref| {
            let backend_condition = self.backend_condition(parent_ref, &route.backends);
            self.grpc_parent_status(id, parent_ref, backend_condition.clone())
        });

        let all_statuses = unowned_statuses.chain(parent_statuses).collect::<Vec<_>>();
        let route_statuses = route
            .statuses
            .iter()
            .flat_map(|status| status.parents.clone())
            .collect::<Vec<_>>();

        if eq_time_insensitive_grpc_route_parent_statuses(&all_statuses, &route_statuses) {
            return None;
        }

        let status = gateway::grpcroutes::GRPCRouteStatus {
            parents: all_statuses,
        };

        make_patch(id, status)
    }

    fn make_tls_route_patch(
        &self,
        id: &NamespaceGroupKindName,
        route: &TLSRouteRef,
    ) -> Option<k8s::Patch<serde_json::Value>> {
        // To preserve any statuses from other controllers, we copy those
        // statuses.
        let unowned_statuses = route
            .statuses
            .iter()
            .flat_map(|status| status.parents.clone())
            .filter(|status| status.controller_name != POLICY_CONTROLLER_NAME);

        // Compute a status for each parent_ref which has a kind we support.
        let parent_statuses = route.parents.iter().filter_map(|parent_ref| {
            let backend_condition = self.backend_condition(parent_ref, &route.backends);
            self.tls_parent_status(id, parent_ref, backend_condition.clone())
        });

        let all_statuses = unowned_statuses.chain(parent_statuses).collect::<Vec<_>>();
        let route_statuses = route
            .statuses
            .iter()
            .flat_map(|status| status.parents.clone())
            .collect::<Vec<_>>();

        if eq_time_insensitive_tls_route_parent_statuses(&all_statuses, &route_statuses) {
            return None;
        }

        let status = gateway::tlsroutes::TLSRouteStatus {
            parents: all_statuses,
        };

        make_patch(id, status)
    }

    fn make_tcp_route_patch(
        &self,
        id: &NamespaceGroupKindName,
        route: &TCPRouteRef,
    ) -> Option<k8s::Patch<serde_json::Value>> {
        // To preserve any statuses from other controllers, we copy those
        // statuses.
        let unowned_statuses = route
            .statuses
            .iter()
            .flat_map(|status| status.parents.clone())
            .filter(|status| status.controller_name != POLICY_CONTROLLER_NAME);

        // Compute a status for each parent_ref which has a kind we support.
        let parent_statuses = route.parents.iter().filter_map(|parent_ref| {
            let backend_condition = self.backend_condition(parent_ref, &route.backends);
            self.tcp_parent_status(id, parent_ref, backend_condition.clone())
        });

        let all_statuses = unowned_statuses.chain(parent_statuses).collect::<Vec<_>>();
        let route_statuses = route
            .statuses
            .iter()
            .flat_map(|status| status.parents.clone())
            .collect::<Vec<_>>();

        if eq_time_insensitive_tcp_route_parent_statuses(&all_statuses, &route_statuses) {
            return None;
        }

        let status = gateway::tcproutes::TCPRouteStatus {
            parents: all_statuses,
        };

        make_patch(id, status)
    }

    fn target_ref_status(
        &self,
        id: &NamespaceGroupKindName,
        target_ref: &ratelimit::TargetReference,
    ) -> Option<policy::HttpLocalRateLimitPolicyStatus> {
        match target_ref {
            ratelimit::TargetReference::Server(server) => {
                let condition = if self.servers.contains(server) {
                    // Collect rate limits for this server, sorted by creation timestamp and then
                    // by name. If the current RL is the first one in the list, it is accepted.
                    let mut rate_limits = self
                        .ratelimits
                        .iter()
                        .filter(|(_, rl_ref)| rl_ref.target_ref == *target_ref)
                        .collect::<Vec<_>>();
                    rate_limits.sort_by(|(a_id, a), (b_id, b)| {
                        let by_ts = match (&a.creation_timestamp, &b.creation_timestamp) {
                            (Some(a_ts), Some(b_ts)) => a_ts.cmp(b_ts),
                            (None, None) => std::cmp::Ordering::Equal,
                            // entries with timestamps are preferred over ones without
                            (Some(_), None) => return std::cmp::Ordering::Less,
                            (None, Some(_)) => return std::cmp::Ordering::Greater,
                        };
                        by_ts.then_with(|| a_id.name.cmp(&b_id.name))
                    });

                    let Some((first_id, _)) = rate_limits.first() else {
                        // No rate limits exist for this server; we shouldn't reach this point!
                        return None;
                    };

                    if first_id.name == id.gkn.name {
                        accepted()
                    } else {
                        ratelimit_already_exists()
                    }
                } else {
                    no_matching_target()
                };

                Some(policy::HttpLocalRateLimitPolicyStatus {
                    conditions: vec![condition],
                    target_ref: policy::LocalTargetRef {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: "Server".to_string(),
                        name: server.name.clone(),
                    },
                })
            }
            ratelimit::TargetReference::UnknownKind => None,
        }
    }

    fn make_ratelimit_patch(
        &self,
        id: &NamespaceGroupKindName,
        ratelimit: &HttpLocalRateLimitPolicyRef,
    ) -> Option<k8s::Patch<serde_json::Value>> {
        let status = self.target_ref_status(id, &ratelimit.target_ref)?;
        if eq_time_insensitive_conditions(&status.conditions, &ratelimit.status_conditions) {
            return None;
        }

        make_patch(id, status)
    }

    fn network_condition(&self, egress_net: &EgressNetworkRef) -> k8s::Condition {
        for egress_network_block in &egress_net.networks {
            for cluster_network_block in &self.cluster_networks {
                if egress_network_block.intersect(cluster_network_block) {
                    return in_cluster_net_overlap();
                }
            }
        }

        accepted()
    }

    fn make_egress_net_patch(
        &self,
        id: &NamespaceGroupKindName,
        egress_net: &EgressNetworkRef,
    ) -> Option<k8s::Patch<serde_json::Value>> {
        let unowned_conditions = egress_net
            .status_conditions
            .iter()
            .filter(|c| c.type_ != conditions::ACCEPTED)
            .cloned();

        let all_conditions: Vec<linkerd_policy_controller_k8s_api::Condition> = unowned_conditions
            .chain(std::iter::once(self.network_condition(egress_net)))
            .collect::<Vec<_>>();

        if eq_time_insensitive_conditions(&all_conditions, &egress_net.status_conditions) {
            return None;
        }

        let status = policy::EgressNetworkStatus {
            conditions: all_conditions,
        };

        make_patch(id, status)
    }

    /// If this instance is the leader, reconcile the statuses for all resources
    /// for which we control the status.
    fn reconcile_if_leader(&self) {
        let lease = self.claims.borrow();
        if !lease.is_current_for(&self.name) {
            tracing::trace!(%lease.holder, ?lease.expiry, "Reconcilation skipped");
            return;
        }
        drop(lease);

        tracing::trace!(
            egressnetworks = self.egress_networks.len(),
            http_routes = self.http_route_refs.len(),
            grpc_routes = self.grpc_route_refs.len(),
            tls_routes = self.tls_route_refs.len(),
            tcp_routes = self.tcp_route_refs.len(),
            httplocalratelimits = self.ratelimits.len(),
            "Reconciling"
        );
        let egressnetworks = self.reconcile_egress_networks();
        let routes = self.reconcile_routes();
        let ratelimits = self.reconcile_ratelimits();

        if egressnetworks + routes + ratelimits > 0 {
            tracing::debug!(egressnetworks, routes, ratelimits, "Reconciled");
        }
    }

    fn reconcile_egress_networks(&self) -> usize {
        let mut patches = 0;
        for (id, net) in self.egress_networks.iter() {
            let id = NamespaceGroupKindName {
                namespace: id.namespace.clone(),
                gkn: GroupKindName {
                    group: policy::EgressNetwork::group(&()),
                    kind: policy::EgressNetwork::kind(&()),
                    name: id.name.clone().into(),
                },
            };

            if let Some(patch) = self.make_egress_net_patch(&id, net) {
                match self.updates.try_send(Update {
                    id: id.clone(),
                    patch,
                }) {
                    Ok(()) => {
                        patches += 1;
                        self.metrics.patch_enqueues.inc();
                    }
                    Err(error) => {
                        self.metrics.patch_channel_full.inc();
                        tracing::error!(%id.namespace, route = ?id.gkn, %error, "Failed to send egress network patch");
                    }
                }
            }
        }
        patches
    }

    fn reconcile_routes(&self) -> usize {
        let mut patches = 0;
        let http_patches = self
            .http_route_refs
            .iter()
            .filter_map(|(id, route)| self.make_http_route_patch(id, route).map(|p| (id, p)));
        let grpc_patches = self
            .grpc_route_refs
            .iter()
            .filter_map(|(id, route)| self.make_grpc_route_patch(id, route).map(|p| (id, p)));
        let tls_patches = self
            .tls_route_refs
            .iter()
            .filter_map(|(id, route)| self.make_tls_route_patch(id, route).map(|p| (id, p)));
        let tcp_patches = self
            .tcp_route_refs
            .iter()
            .filter_map(|(id, route)| self.make_tcp_route_patch(id, route).map(|p| (id, p)));

        for (id, patch) in http_patches
            .chain(grpc_patches)
            .chain(tls_patches)
            .chain(tcp_patches)
        {
            match self.updates.try_send(Update {
                id: id.clone(),
                patch,
            }) {
                Ok(()) => {
                    patches += 1;
                    self.metrics.patch_enqueues.inc();
                }
                Err(error) => {
                    self.metrics.patch_channel_full.inc();
                    tracing::error!(%id.namespace, route = ?id.gkn, %error, "Failed to send route patch");
                }
            }
        }
        patches
    }

    fn reconcile_ratelimits(&self) -> usize {
        let mut patches = 0;
        for (id, rl) in self.ratelimits.iter() {
            let id = NamespaceGroupKindName {
                namespace: id.namespace.clone(),
                gkn: GroupKindName {
                    group: policy::HttpLocalRateLimitPolicy::group(&()),
                    kind: policy::HttpLocalRateLimitPolicy::kind(&()),
                    name: id.name.clone().into(),
                },
            };

            if let Some(patch) = self.make_ratelimit_patch(&id, rl) {
                match self.updates.try_send(Update {
                    id: id.clone(),
                    patch,
                }) {
                    Ok(()) => {
                        patches += 1;
                        self.metrics.patch_enqueues.inc();
                    }
                    Err(error) => {
                        self.metrics.patch_channel_full.inc();
                        tracing::error!(%id.namespace, ratelimit = ?id.gkn, %error, "Failed to send ratelimit patch");
                    }
                }
            }
        }
        patches
    }

    #[tracing::instrument(level = "debug", skip(self, net))]
    pub(crate) fn index_egress_network(&mut self, id: ResourceId, net: EgressNetworkRef) {
        tracing::trace!(?net);
        // Insert into the index; if the network is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_egress_net(id, &net) {
            return;
        }

        self.reconcile_if_leader();
    }

    #[tracing::instrument(level = "debug", skip(self, ratelimit))]
    fn index_ratelimit(&mut self, id: ResourceId, ratelimit: HttpLocalRateLimitPolicyRef) {
        tracing::trace!(?ratelimit);
        // Insert into the index; if the route is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_ratelimit(id.clone(), &ratelimit) {
            return;
        }

        self.reconcile_if_leader();
    }
}

impl kubert::index::IndexNamespacedResource<policy::HttpRoute> for Index {
    fn apply(&mut self, resource: policy::HttpRoute) {
        let namespace = resource
            .namespace()
            .expect("HTTPRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                group: policy::HttpRoute::group(&()),
                kind: policy::HttpRoute::kind(&()),
                name: name.into(),
            },
        };

        // Create the route parents
        let parents =
            routes::http::make_parents(&namespace, &resource.spec.parent_refs.unwrap_or_default());

        // Create the route backends
        let backends = routes::http::make_backends(
            &namespace,
            resource
                .spec
                .rules
                .into_iter()
                .flatten()
                .flat_map(|rule| rule.backend_refs)
                .flatten(),
        );

        let statuses = resource
            .status
            .into_iter()
            .flat_map(|status| status.inner.parents)
            .collect();

        // Construct route and insert into the index; if the HTTPRoute is
        // already in the index, and it hasn't changed, skip creating a patch.
        let route = HTTPRouteRef {
            parents,
            backends,
            statuses: vec![gateway::httproutes::HTTPRouteStatus { parents: statuses }],
        };
        tracing::trace!(?route);
        // Insert into the index; if the route is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_http_route(id.clone(), &route) {
            return;
        }

        self.reconcile_if_leader();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group: policy::HttpRoute::group(&()),
                kind: policy::HttpRoute::kind(&()),
                name: name.into(),
            },
        };
        self.http_route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<gateway::httproutes::HTTPRoute> for Index {
    fn apply(&mut self, resource: gateway::httproutes::HTTPRoute) {
        let namespace = resource
            .namespace()
            .expect("HTTPRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                group: gateway::httproutes::HTTPRoute::group(&()),
                kind: gateway::httproutes::HTTPRoute::kind(&()),
                name: name.into(),
            },
        };

        // Create the route parents
        let parents =
            routes::http::make_parents(&namespace, &resource.spec.parent_refs.unwrap_or_default());

        // Create the route backends
        let backends = routes::http::make_backends(
            &namespace,
            resource
                .spec
                .rules
                .into_iter()
                .flatten()
                .flat_map(|rule| rule.backend_refs)
                .flatten(),
        );

        let statuses = resource
            .status
            .into_iter()
            .flat_map(|status| status.parents)
            .collect();

        // Construct route and insert into the index; if the HTTPRoute is
        // already in the index, and it hasn't changed, skip creating a patch.
        let route = RouteRef {
            parents,
            backends,
            statuses: vec![gateway::httproutes::HTTPRouteStatus { parents: statuses }],
        };
        tracing::trace!(?route);
        // Insert into the index; if the route is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_http_route(id.clone(), &route) {
            return;
        }

        self.reconcile_if_leader();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group: gateway::httproutes::HTTPRoute::group(&()),
                kind: gateway::httproutes::HTTPRoute::kind(&()),
                name: name.into(),
            },
        };
        self.http_route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<gateway::grpcroutes::GRPCRoute> for Index {
    fn apply(&mut self, resource: gateway::grpcroutes::GRPCRoute) {
        let namespace = resource
            .namespace()
            .expect("GRPCRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                name: name.into(),
                kind: gateway::grpcroutes::GRPCRoute::kind(&()),
                group: gateway::grpcroutes::GRPCRoute::group(&()),
            },
        };

        // Create the route parents
        let parents =
            routes::grpc::make_parents(&namespace, &resource.spec.parent_refs.unwrap_or_default());

        // Create the route backends
        let backends = routes::grpc::make_backends(
            &namespace,
            resource
                .spec
                .rules
                .into_iter()
                .flatten()
                .flat_map(|rule| rule.backend_refs)
                .flatten(),
        );

        let statuses = resource
            .status
            .into_iter()
            .flat_map(|status| status.parents)
            .collect();

        // Construct route and insert into the index; if the GRPCRoute is
        // already in the index and it hasn't changed, skip creating a patch.
        let route = RouteRef {
            parents,
            backends,
            statuses: vec![gateway::grpcroutes::GRPCRouteStatus { parents: statuses }],
        };
        tracing::trace!(?route);
        // Insert into the index; if the route is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_grpc_route(id.clone(), &route) {
            return;
        }

        self.reconcile_if_leader();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                name: name.into(),
                kind: gateway::grpcroutes::GRPCRoute::kind(&()),
                group: gateway::grpcroutes::GRPCRoute::group(&()),
            },
        };
        self.grpc_route_refs.remove(&id);
    }

    // Since apply only reindexes a single GRPCRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<gateway::tlsroutes::TLSRoute> for Index {
    fn apply(&mut self, resource: gateway::tlsroutes::TLSRoute) {
        let namespace = resource
            .namespace()
            .expect("TlsRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                group: gateway::tlsroutes::TLSRoute::group(&()),
                kind: gateway::tlsroutes::TLSRoute::kind(&()),
                name: name.into(),
            },
        };

        // Create the route parents
        let parents =
            routes::tls::make_parents(&namespace, &resource.spec.parent_refs.unwrap_or_default());

        // Create the route backends
        let backends = routes::tls::make_backends(
            &namespace,
            resource
                .spec
                .rules
                .into_iter()
                .flat_map(|rule| rule.backend_refs)
                .flatten(),
        );

        let statuses = resource
            .status
            .into_iter()
            .flat_map(|status| status.parents)
            .collect();

        // Construct route and insert into the index; if the TLSRoute is
        // already in the index, and it hasn't changed, skip creating a patch.
        let route = RouteRef {
            parents,
            backends,
            statuses: vec![gateway::tlsroutes::TLSRouteStatus { parents: statuses }],
        };
        tracing::trace!(?route);
        // Insert into the index; if the route is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_tls_route(id.clone(), &route) {
            return;
        }

        self.reconcile_if_leader();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group: gateway::tlsroutes::TLSRoute::group(&()),
                kind: gateway::tlsroutes::TLSRoute::kind(&()),
                name: name.into(),
            },
        };
        self.tls_route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<gateway::tcproutes::TCPRoute> for Index {
    fn apply(&mut self, resource: gateway::tcproutes::TCPRoute) {
        let namespace = resource
            .namespace()
            .expect("TcpRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                group: gateway::tcproutes::TCPRoute::group(&()),
                kind: gateway::tcproutes::TCPRoute::kind(&()),
                name: name.into(),
            },
        };

        // Create the route parents
        let parents =
            routes::tcp::make_parents(&namespace, &resource.spec.parent_refs.unwrap_or_default());

        // Create the route backends
        let backends = routes::tcp::make_backends(
            &namespace,
            resource
                .spec
                .rules
                .into_iter()
                .flat_map(|rule| rule.backend_refs)
                .flatten(),
        );

        let statuses = resource
            .status
            .into_iter()
            .flat_map(|status| status.parents)
            .collect();

        // Construct route and insert into the index; if the TCPRoute is
        // already in the index, and it hasn't changed, skip creating a patch.
        let route = RouteRef {
            parents,
            backends,
            statuses: vec![gateway::tcproutes::TCPRouteStatus { parents: statuses }],
        };
        tracing::trace!(?route);
        // Insert into the index; if the route is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_tcp_route(id.clone(), &route) {
            return;
        }

        self.reconcile_if_leader();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group: gateway::tcproutes::TCPRoute::group(&()),
                kind: gateway::tcproutes::TCPRoute::kind(&()),
                name: name.into(),
            },
        };
        self.tcp_route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<policy::Server> for Index {
    fn apply(&mut self, resource: policy::Server) {
        let namespace = resource.namespace().expect("Server must have a namespace");
        let name = resource.name_unchecked();
        self.servers.insert(ResourceId::new(namespace, name));
        self.reconcile_if_leader();
    }

    fn delete(&mut self, namespace: String, name: String) {
        self.servers.remove(&ResourceId::new(namespace, name));
        self.reconcile_if_leader();
    }

    // Since apply only reindexes a single Server at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s::Service> for Index {
    fn apply(&mut self, resource: k8s::Service) {
        let namespace = resource.namespace().expect("Service must have a namespace");
        let name = resource.name_unchecked();
        self.services
            .insert(ResourceId::new(namespace, name), resource.into());
        self.reconcile_if_leader();
    }

    fn delete(&mut self, namespace: String, name: String) {
        self.services.remove(&ResourceId::new(namespace, name));
        self.reconcile_if_leader();
    }

    // Since apply only reindexes a single Service at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<policy::HttpLocalRateLimitPolicy> for Index {
    fn apply(&mut self, resource: policy::HttpLocalRateLimitPolicy) {
        let namespace = resource
            .namespace()
            .expect("HTTPLocalRateLimitPolicy must have a namespace");
        let name = resource.name_unchecked();

        let status_conditions = resource
            .status
            .into_iter()
            .flat_map(|s| s.conditions)
            .collect();

        let id = ResourceId::new(namespace.clone(), name);
        let creation_timestamp = resource.metadata.creation_timestamp.map(|Time(t)| t);
        let target_ref = ratelimit::TargetReference::make_target_ref(&namespace, &resource.spec);

        let rl = HttpLocalRateLimitPolicyRef {
            creation_timestamp,
            target_ref,
            status_conditions,
        };

        self.index_ratelimit(id, rl);
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);
        self.ratelimits.remove(&id);
        self.reconcile_if_leader();
    }
}

impl kubert::index::IndexNamespacedResource<policy::EgressNetwork> for Index {
    fn apply(&mut self, resource: policy::EgressNetwork) {
        let namespace = resource
            .namespace()
            .expect("EgressNetwork must have a namespace");
        let name = resource.name_unchecked();

        let status_conditions = resource
            .status
            .into_iter()
            .flat_map(|s| s.conditions)
            .collect();

        let networks = resource.spec.networks.unwrap_or_else(|| {
            let (v6, v4) = self
                .cluster_networks
                .iter()
                .cloned()
                .partition(Cidr::is_ipv6);

            vec![
                Network {
                    cidr: "0.0.0.0/0".parse().expect("should parse"),
                    except: Some(v4),
                },
                Network {
                    cidr: "::/0".parse().expect("should parse"),
                    except: Some(v6),
                },
            ]
        });

        let id = ResourceId::new(namespace, name);

        let net = EgressNetworkRef {
            status_conditions,
            networks,
        };

        self.index_egress_network(id, net);
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);
        self.egress_networks.remove(&id);
        self.reconcile_if_leader();
    }
}

pub(crate) fn make_patch<Status>(
    resource_id: &NamespaceGroupKindName,
    status: Status,
) -> Option<k8s::Patch<serde_json::Value>>
where
    Status: serde::Serialize,
{
    match resource_id.api_version() {
        Err(error) => {
            tracing::error!(error = %error, "Failed to create patch for resource");
            None
        }
        Ok(api_version) => {
            let patch = serde_json::json!({
                "apiVersion": api_version,
                    "kind": &resource_id.gkn.kind,
                    "name": &resource_id.gkn.name,
                    "status": status,
            });

            Some(k8s::Patch::Merge(patch))
        }
    }
}

fn now() -> DateTime<Utc> {
    #[cfg(not(test))]
    let now = Utc::now();
    #[cfg(test)]
    let now = DateTime::<Utc>::MIN_UTC;
    now
}

pub(crate) fn no_matching_parent() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_PARENT.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn no_matching_target() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_TARGET.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

fn headless_parent() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "parent service must have a ClusterIP".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_PARENT.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

fn egress_net_not_accepted() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "EgressNetwork parent has not been accepted".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_PARENT.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn route_conflicted() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::ROUTE_REASON_CONFLICTED.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn ratelimit_already_exists() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::RATELIMIT_REASON_ALREADY_EXISTS.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn accepted() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: conditions::ACCEPTED.to_string(),
        status: cond_statuses::STATUS_TRUE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn in_cluster_net_overlap() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "networks overlap with clusterNetworks".to_string(),
        observed_generation: None,
        reason: reasons::EGRESS_NET_REASON_OVERLAP.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn resolved_refs() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::RESOLVED_REFS.to_string(),
        status: cond_statuses::STATUS_TRUE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

pub(crate) fn backend_not_found() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::BACKEND_NOT_FOUND.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

pub(crate) fn invalid_backend_kind(message: &str) -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: message.to_string(),
        observed_generation: None,
        reason: reasons::INVALID_KIND.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

pub(crate) fn eq_time_insensitive_http_route_parent_statuses(
    left: &[gateway::httproutes::HTTPRouteStatusParents],
    right: &[gateway::httproutes::HTTPRouteStatusParents],
) -> bool {
    if left.len() != right.len() {
        return false;
    }

    // Create sorted versions of the input slices
    let mut left_sorted: Vec<_> = left.to_vec();
    let mut right_sorted: Vec<_> = right.to_vec();

    left_sorted.sort_by(|a, b| {
        a.controller_name
            .cmp(&b.controller_name)
            .then_with(|| a.parent_ref.name.cmp(&b.parent_ref.name))
            .then_with(|| a.parent_ref.namespace.cmp(&b.parent_ref.namespace))
    });
    right_sorted.sort_by(|a, b| {
        a.controller_name
            .cmp(&b.controller_name)
            .then_with(|| a.parent_ref.name.cmp(&b.parent_ref.name))
            .then_with(|| a.parent_ref.namespace.cmp(&b.parent_ref.namespace))
    });

    // Compare each element in sorted order
    left_sorted.iter().zip(right_sorted.iter()).all(|(l, r)| {
        let cond_eq = match (&l.conditions, &r.conditions) {
            (Some(l), Some(r)) => eq_time_insensitive_conditions(l.as_ref(), r.as_ref()),
            (None, None) => true,
            _ => false,
        };
        l.parent_ref == r.parent_ref && l.controller_name == r.controller_name && cond_eq
    })
}

pub(crate) fn eq_time_insensitive_grpc_route_parent_statuses(
    left: &[gateway::grpcroutes::GRPCRouteStatusParents],
    right: &[gateway::grpcroutes::GRPCRouteStatusParents],
) -> bool {
    if left.len() != right.len() {
        return false;
    }

    // Create sorted versions of the input slices
    let mut left_sorted: Vec<_> = left.to_vec();
    let mut right_sorted: Vec<_> = right.to_vec();

    left_sorted.sort_by(|a, b| {
        a.controller_name
            .cmp(&b.controller_name)
            .then_with(|| a.parent_ref.name.cmp(&b.parent_ref.name))
            .then_with(|| a.parent_ref.namespace.cmp(&b.parent_ref.namespace))
    });
    right_sorted.sort_by(|a, b| {
        a.controller_name
            .cmp(&b.controller_name)
            .then_with(|| a.parent_ref.name.cmp(&b.parent_ref.name))
            .then_with(|| a.parent_ref.namespace.cmp(&b.parent_ref.namespace))
    });

    // Compare each element in sorted order
    left_sorted.iter().zip(right_sorted.iter()).all(|(l, r)| {
        let cond_eq = match (&l.conditions, &r.conditions) {
            (Some(l), Some(r)) => eq_time_insensitive_conditions(l.as_ref(), r.as_ref()),
            (None, None) => true,
            _ => false,
        };
        l.parent_ref == r.parent_ref && l.controller_name == r.controller_name && cond_eq
    })
}

pub(crate) fn eq_time_insensitive_tls_route_parent_statuses(
    left: &[gateway::tlsroutes::TLSRouteStatusParents],
    right: &[gateway::tlsroutes::TLSRouteStatusParents],
) -> bool {
    if left.len() != right.len() {
        return false;
    }

    // Create sorted versions of the input slices
    let mut left_sorted: Vec<_> = left.to_vec();
    let mut right_sorted: Vec<_> = right.to_vec();

    left_sorted.sort_by(|a, b| {
        a.controller_name
            .cmp(&b.controller_name)
            .then_with(|| a.parent_ref.name.cmp(&b.parent_ref.name))
            .then_with(|| a.parent_ref.namespace.cmp(&b.parent_ref.namespace))
    });
    right_sorted.sort_by(|a, b| {
        a.controller_name
            .cmp(&b.controller_name)
            .then_with(|| a.parent_ref.name.cmp(&b.parent_ref.name))
            .then_with(|| a.parent_ref.namespace.cmp(&b.parent_ref.namespace))
    });

    // Compare each element in sorted order
    left_sorted.iter().zip(right_sorted.iter()).all(|(l, r)| {
        let cond_eq = match (&l.conditions, &r.conditions) {
            (Some(l), Some(r)) => eq_time_insensitive_conditions(l.as_ref(), r.as_ref()),
            (None, None) => true,
            _ => false,
        };
        l.parent_ref == r.parent_ref && l.controller_name == r.controller_name && cond_eq
    })
}

pub(crate) fn eq_time_insensitive_tcp_route_parent_statuses(
    left: &[gateway::tcproutes::TCPRouteStatusParents],
    right: &[gateway::tcproutes::TCPRouteStatusParents],
) -> bool {
    if left.len() != right.len() {
        return false;
    }

    // Create sorted versions of the input slices
    let mut left_sorted: Vec<_> = left.to_vec();
    let mut right_sorted: Vec<_> = right.to_vec();

    left_sorted.sort_by(|a, b| {
        a.controller_name
            .cmp(&b.controller_name)
            .then_with(|| a.parent_ref.name.cmp(&b.parent_ref.name))
            .then_with(|| a.parent_ref.namespace.cmp(&b.parent_ref.namespace))
    });
    right_sorted.sort_by(|a, b| {
        a.controller_name
            .cmp(&b.controller_name)
            .then_with(|| a.parent_ref.name.cmp(&b.parent_ref.name))
            .then_with(|| a.parent_ref.namespace.cmp(&b.parent_ref.namespace))
    });

    // Compare each element in sorted order
    left_sorted.iter().zip(right_sorted.iter()).all(|(l, r)| {
        let cond_eq = match (&l.conditions, &r.conditions) {
            (Some(l), Some(r)) => eq_time_insensitive_conditions(l.as_ref(), r.as_ref()),
            (None, None) => true,
            _ => false,
        };
        l.parent_ref == r.parent_ref && l.controller_name == r.controller_name && cond_eq
    })
}

fn eq_time_insensitive_conditions(left: &[k8s::Condition], right: &[k8s::Condition]) -> bool {
    if left.len() != right.len() {
        return false;
    }

    left.iter().zip(right.iter()).all(|(l, r)| {
        l.message == r.message
            && l.observed_generation == r.observed_generation
            && l.reason == r.reason
            && l.status == r.status
            && l.type_ == r.type_
    })
}
