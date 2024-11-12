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
    self as k8s_core_api, gateway as k8s_gateway_api,
    policy::{self as linkerd_k8s_api, Cidr, Network},
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

mod conflict;

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
    client: k8s_core_api::Client,
    name: String,
    updates: mpsc::Receiver<Update>,
    patch_timeout: Duration,

    /// True if this policy controller is the leader — false otherwise.
    leader: bool,

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
    route_refs: HashMap<NamespaceGroupKindName, RouteRef>,

    /// Maps rate limit ids to a list of details about these rate limits.
    ratelimits: HashMap<ResourceId, HTTPLocalRateLimitPolicyRef>,

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

#[derive(Clone, PartialEq)]
struct RouteRef {
    parents: Vec<routes::ParentReference>,
    backends: Vec<routes::BackendReference>,
    statuses: Vec<k8s_gateway_api::RouteParentStatus>,
}

#[derive(Clone, PartialEq)]
struct HTTPLocalRateLimitPolicyRef {
    creation_timestamp: Option<DateTime<Utc>>,
    target_ref: ratelimit::TargetReference,
    status_conditions: Vec<k8s_core_api::Condition>,
}

#[derive(Clone, PartialEq)]
struct EgressNetworkRef {
    networks: Vec<Network>,
    status_conditions: Vec<k8s_core_api::Condition>,
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
    pub patch: k8s_core_api::Patch<serde_json::Value>,
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
        client: k8s_core_api::Client,
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
            leader: false,
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
        loop {
            tokio::select! {
                biased;
                res = self.claims.changed() => {
                    res.expect("Claims watch must not be dropped");
                    let claim = self.claims.borrow_and_update();
                    let was_leader = self.leader;
                    self.leader = claim.is_current_for(&self.name);
                    if was_leader != self.leader {
                        tracing::debug!(leader = %self.leader, "Leadership changed");
                    }
                }

                Some(Update { id, patch}) = self.updates.recv() => {
                    self.metrics.patch_dequeues.inc();
                    // If this policy controller is not the leader, it should
                    // process through the updates queue but not actually patch
                    // any resources.
                    if self.leader {
                        if id.gkn.group == linkerd_k8s_api::HttpRoute::group(&()) && id.gkn.kind == linkerd_k8s_api::HttpRoute::kind(&()){
                            self.patch_status::<linkerd_k8s_api::HttpRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.gkn.group == k8s_gateway_api::HttpRoute::group(&()) && id.gkn.kind == k8s_gateway_api::HttpRoute::kind(&()) {
                            self.patch_status::<k8s_gateway_api::HttpRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.gkn.group == k8s_gateway_api::GrpcRoute::group(&()) && id.gkn.kind == k8s_gateway_api::GrpcRoute::kind(&()) {
                            self.patch_status::<k8s_gateway_api::GrpcRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.gkn.group == k8s_gateway_api::TcpRoute::group(&()) && id.gkn.kind == k8s_gateway_api::TcpRoute::kind(&()) {
                            self.patch_status::<k8s_gateway_api::TcpRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.gkn.group == k8s_gateway_api::TlsRoute::group(&()) && id.gkn.kind == k8s_gateway_api::TlsRoute::kind(&()) {
                            self.patch_status::<k8s_gateway_api::TlsRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.gkn.group == linkerd_k8s_api::HTTPLocalRateLimitPolicy::group(&()) && id.gkn.kind == linkerd_k8s_api::HTTPLocalRateLimitPolicy::kind(&()) {
                            self.patch_status::<linkerd_k8s_api::HTTPLocalRateLimitPolicy>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.gkn.group == linkerd_k8s_api::EgressNetwork::group(&()) && id.gkn.kind == linkerd_k8s_api::EgressNetwork::kind(&()) {
                            self.patch_status::<linkerd_k8s_api::EgressNetwork>(&id.gkn.name, &id.namespace, patch).await;
                        }
                    } else {
                        self.metrics.patch_drops.inc();
                    }
                }
            }
        }
    }

    async fn patch_status<K>(
        &self,
        name: &str,
        namespace: &str,
        patch: k8s_core_api::Patch<serde_json::Value>,
    ) where
        K: Resource<Scope = NamespaceResourceScope>,
        <K as Resource>::DynamicType: Default,
        K: DeserializeOwned,
    {
        let patch_params = k8s_core_api::PatchParams::apply(K::group(&Default::default()).as_ref());
        let api = k8s_core_api::Api::<K>::namespaced(self.client.clone(), namespace);
        let start = time::Instant::now();

        match time::timeout(
            self.patch_timeout,
            api.patch_status(name, &patch_params, &patch),
        )
        .await
        {
            Ok(Ok(_)) => {
                self.metrics.patch_succeeded.inc();
                self.metrics
                    .patch_duration
                    .observe(start.elapsed().as_secs_f64());
            }
            Ok(Err(error)) => {
                self.metrics.patch_failed.inc();
                self.metrics
                    .patch_duration
                    .observe(start.elapsed().as_secs_f64());
                tracing::error!(%namespace, %name, kind = %K::kind(&Default::default()), %error, "Patch failed");
            }
            Err(_) => {
                self.metrics.patch_timeout.inc();
                tracing::error!(%namespace, %name, kind = %K::kind(&Default::default()), "Patch timed out");
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
            route_refs: HashMap::new(),
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
        // Clone the claims watch out of the index. This will immediately
        // drop the read lock on the index so that it is not held for the
        // lifetime of this function.
        let mut claims = index.read().claims.clone();

        loop {
            tokio::select! {
                res = claims.changed() => {
                    res.expect("Claims watch must not be dropped");
                    tracing::debug!("Lease holder has changed");
                }
                _ = time::sleep(reconciliation_period) => {}
            }

            // The claimant has changed, or we should attempt to reconcile all
            //routes to account for any errors. In either case, we should
            // only proceed if we are the current leader.
            let claims = claims.borrow_and_update();
            let index = index.read();

            if !claims.is_current_for(&index.name) {
                continue;
            }
            tracing::debug!(%index.name, "Lease holder reconciling cluster");
            index.reconcile();
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
    fn update_route(&mut self, id: NamespaceGroupKindName, route: &RouteRef) -> bool {
        match self.route_refs.entry(id) {
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
        ratelimit: &HTTPLocalRateLimitPolicyRef,
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

    fn parent_status(
        &self,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
        backend_condition: k8s_core_api::Condition,
    ) -> Option<k8s_gateway_api::RouteParentStatus> {
        match parent_ref {
            routes::ParentReference::Server(server) => {
                let condition = if self.servers.contains(server) {
                    if conflict::parent_has_conflicting_routes(
                        &mut self.route_refs.iter(),
                        parent_ref,
                        &id.gkn.kind,
                    ) {
                        route_conflicted()
                    } else {
                        accepted()
                    }
                } else {
                    no_matching_parent()
                };

                Some(k8s_gateway_api::RouteParentStatus {
                    parent_ref: k8s_gateway_api::ParentReference {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(server.namespace.clone()),
                        name: server.name.clone(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: vec![condition],
                })
            }

            routes::ParentReference::Service(service, port) => {
                // service is a valid parent if it exists and it has a cluster_ip.
                let condition = match self.services.get(service) {
                    Some(svc) if svc.valid_parent_service() => {
                        if conflict::parent_has_conflicting_routes(
                            &mut self.route_refs.iter(),
                            parent_ref,
                            &id.gkn.kind,
                        ) {
                            route_conflicted()
                        } else {
                            accepted()
                        }
                    }
                    Some(_svc) => headless_parent(),
                    None => no_matching_parent(),
                };

                Some(k8s_gateway_api::RouteParentStatus {
                    parent_ref: k8s_gateway_api::ParentReference {
                        group: Some("core".to_string()),
                        kind: Some("Service".to_string()),
                        namespace: Some(service.namespace.clone()),
                        name: service.name.clone(),
                        section_name: None,
                        port: *port,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: vec![condition, backend_condition],
                })
            }

            routes::ParentReference::EgressNetwork(egress_net, port) => {
                // egress network is a valid parent if it exists and is accepted.
                let condition = match self.egress_networks.get(egress_net) {
                    Some(egress_net) if egress_net.is_accepted() => {
                        if conflict::parent_has_conflicting_routes(
                            &mut self.route_refs.iter(),
                            parent_ref,
                            &id.gkn.kind,
                        ) {
                            route_conflicted()
                        } else {
                            accepted()
                        }
                    }
                    Some(_) => egress_net_not_accepted(),
                    None => no_matching_parent(),
                };

                Some(k8s_gateway_api::RouteParentStatus {
                    parent_ref: k8s_gateway_api::ParentReference {
                        group: Some("policy.linkerd.io".to_string()),
                        kind: Some("EgressNetwork".to_string()),
                        namespace: Some(egress_net.namespace.clone()),
                        name: egress_net.name.clone(),
                        section_name: None,
                        port: *port,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: vec![condition, backend_condition],
                })
            }
            routes::ParentReference::UnknownKind => None,
        }
    }

    fn backend_condition(
        &self,
        parent_ref: &routes::ParentReference,
        backend_refs: &[routes::BackendReference],
    ) -> k8s_core_api::Condition {
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

    fn make_route_patch(
        &self,
        id: &NamespaceGroupKindName,
        route: &RouteRef,
    ) -> Option<k8s_core_api::Patch<serde_json::Value>> {
        // To preserve any statuses from other controllers, we copy those
        // statuses.
        let unowned_statuses = route
            .statuses
            .iter()
            .filter(|status| status.controller_name != POLICY_CONTROLLER_NAME)
            .cloned();

        // Compute a status for each parent_ref which has a kind we support.
        let parent_statuses = route.parents.iter().filter_map(|parent_ref| {
            let backend_condition = self.backend_condition(parent_ref, &route.backends);
            self.parent_status(id, parent_ref, backend_condition.clone())
        });

        let all_statuses = unowned_statuses.chain(parent_statuses).collect::<Vec<_>>();

        if eq_time_insensitive_route_parent_statuses(&all_statuses, &route.statuses) {
            return None;
        }

        // Include both existing statuses from other controllers
        // and the parent statuses we have computed.
        match (id.gkn.group.as_ref(), id.gkn.kind.as_ref()) {
            (POLICY_API_GROUP, "HTTPRoute") => {
                let status = linkerd_k8s_api::httproute::HttpRouteStatus {
                    inner: linkerd_k8s_api::httproute::RouteStatus {
                        parents: all_statuses,
                    },
                };

                make_patch(id, status)
            }
            (GATEWAY_API_GROUP, "HTTPRoute") => {
                let status = k8s_gateway_api::HttpRouteStatus {
                    inner: k8s_gateway_api::RouteStatus {
                        parents: all_statuses,
                    },
                };

                make_patch(id, status)
            }
            (GATEWAY_API_GROUP, "GRPCRoute") => {
                let status = k8s_gateway_api::GrpcRouteStatus {
                    inner: k8s_gateway_api::RouteStatus {
                        parents: all_statuses,
                    },
                };

                make_patch(id, status)
            }
            (GATEWAY_API_GROUP, "TLSRoute") => {
                let status = k8s_gateway_api::TlsRouteStatus {
                    inner: k8s_gateway_api::RouteStatus {
                        parents: all_statuses,
                    },
                };

                make_patch(id, status)
            }
            (GATEWAY_API_GROUP, "TCPRoute") => {
                let status = k8s_gateway_api::TcpRouteStatus {
                    inner: k8s_gateway_api::RouteStatus {
                        parents: all_statuses,
                    },
                };

                make_patch(id, status)
            }
            _ => None,
        }
    }

    fn target_ref_status(
        &self,
        id: &NamespaceGroupKindName,
        target_ref: &ratelimit::TargetReference,
    ) -> Option<linkerd_k8s_api::HTTPLocalRateLimitPolicyStatus> {
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

                Some(linkerd_k8s_api::HTTPLocalRateLimitPolicyStatus {
                    conditions: vec![condition],
                    target_ref: linkerd_k8s_api::LocalTargetRef {
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
        ratelimit: &HTTPLocalRateLimitPolicyRef,
    ) -> Option<k8s_core_api::Patch<serde_json::Value>> {
        let status = self.target_ref_status(id, &ratelimit.target_ref);

        let Some(status) = status else {
            return None;
        };

        if eq_time_insensitive_conditions(&status.conditions, &ratelimit.status_conditions) {
            return None;
        }

        make_patch(id, status)
    }

    fn network_condition(&self, egress_net: &EgressNetworkRef) -> k8s_core_api::Condition {
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
    ) -> Option<k8s_core_api::Patch<serde_json::Value>> {
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

        let status = linkerd_k8s_api::EgressNetworkStatus {
            conditions: all_conditions,
        };

        make_patch(id, status)
    }

    fn reconcile(&self) {
        // first update all egress networks and their statuses
        for (id, net) in self.egress_networks.iter() {
            let id = NamespaceGroupKindName {
                namespace: id.namespace.clone(),
                gkn: GroupKindName {
                    group: linkerd_k8s_api::EgressNetwork::group(&()),
                    kind: linkerd_k8s_api::EgressNetwork::kind(&()),
                    name: id.name.clone().into(),
                },
            };

            if let Some(patch) = self.make_egress_net_patch(&id, net) {
                match self.updates.try_send(Update {
                    id: id.clone(),
                    patch,
                }) {
                    Ok(()) => {
                        self.metrics.patch_enqueues.inc();
                    }
                    Err(error) => {
                        self.metrics.patch_channel_full.inc();
                        tracing::error!(%id.namespace, route = ?id.gkn, %error, "Failed to send egress network patch");
                    }
                }
            }
        }

        // then update all route refs
        for (id, route) in self.route_refs.iter() {
            if let Some(patch) = self.make_route_patch(id, route) {
                match self.updates.try_send(Update {
                    id: id.clone(),
                    patch,
                }) {
                    Ok(()) => {
                        self.metrics.patch_enqueues.inc();
                    }
                    Err(error) => {
                        self.metrics.patch_channel_full.inc();
                        tracing::error!(%id.namespace, route = ?id.gkn, %error, "Failed to send route patch");
                    }
                }
            }
        }

        // then update all ratelimit policies and their statuses
        for (id, rl) in self.ratelimits.iter() {
            let id = NamespaceGroupKindName {
                namespace: id.namespace.clone(),
                gkn: GroupKindName {
                    group: linkerd_k8s_api::HTTPLocalRateLimitPolicy::group(&()),
                    kind: linkerd_k8s_api::HTTPLocalRateLimitPolicy::kind(&()),
                    name: id.name.clone().into(),
                },
            };

            if let Some(patch) = self.make_ratelimit_patch(&id, rl) {
                match self.updates.try_send(Update {
                    id: id.clone(),
                    patch,
                }) {
                    Ok(()) => {
                        self.metrics.patch_enqueues.inc();
                    }
                    Err(error) => {
                        self.metrics.patch_channel_full.inc();
                        tracing::error!(%id.namespace, ratelimit = ?id.gkn, %error, "Failed to send ratelimit patch");
                    }
                }
            }
        }
    }
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::HttpRoute> for Index {
    fn apply(&mut self, resource: linkerd_k8s_api::HttpRoute) {
        let namespace = resource
            .namespace()
            .expect("HTTPRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                group: linkerd_k8s_api::HttpRoute::group(&()),
                kind: linkerd_k8s_api::HttpRoute::kind(&()),
                name: name.into(),
            },
        };

        // Create the route parents
        let parents = routes::make_parents(&namespace, &resource.spec.inner);

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
        let route = RouteRef {
            parents,
            backends,
            statuses,
        };
        self.index_route(id, route);
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group: linkerd_k8s_api::HttpRoute::group(&()),
                kind: linkerd_k8s_api::HttpRoute::kind(&()),
                name: name.into(),
            },
        };
        self.route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::HttpRoute> for Index {
    fn apply(&mut self, resource: k8s_gateway_api::HttpRoute) {
        let namespace = resource
            .namespace()
            .expect("HTTPRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                group: k8s_gateway_api::HttpRoute::group(&()),
                kind: k8s_gateway_api::HttpRoute::kind(&()),
                name: name.into(),
            },
        };

        // Create the route parents
        let parents = routes::make_parents(&namespace, &resource.spec.inner);

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
        let route = RouteRef {
            parents,
            backends,
            statuses,
        };
        self.index_route(id, route);
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group: k8s_gateway_api::HttpRoute::group(&()),
                kind: k8s_gateway_api::HttpRoute::kind(&()),
                name: name.into(),
            },
        };
        self.route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::GrpcRoute> for Index {
    fn apply(&mut self, resource: k8s_gateway_api::GrpcRoute) {
        let namespace = resource
            .namespace()
            .expect("GRPCRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                name: name.into(),
                kind: k8s_gateway_api::GrpcRoute::kind(&()),
                group: k8s_gateway_api::GrpcRoute::group(&()),
            },
        };

        // Create the route parents
        let parents = routes::make_parents(&namespace, &resource.spec.inner);

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
            .flat_map(|status| status.inner.parents)
            .collect();

        // Construct route and insert into the index; if the GRPCRoute is
        // already in the index and it hasn't changed, skip creating a patch.
        let route = RouteRef {
            parents,
            backends,
            statuses,
        };
        self.index_route(id, route);
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                name: name.into(),
                kind: k8s_gateway_api::GrpcRoute::kind(&()),
                group: k8s_gateway_api::GrpcRoute::group(&()),
            },
        };
        self.route_refs.remove(&id);
    }

    // Since apply only reindexes a single GRPCRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::TlsRoute> for Index {
    fn apply(&mut self, resource: k8s_gateway_api::TlsRoute) {
        let namespace = resource
            .namespace()
            .expect("TlsRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                group: k8s_gateway_api::TlsRoute::group(&()),
                kind: k8s_gateway_api::TlsRoute::kind(&()),
                name: name.into(),
            },
        };

        // Create the route parents
        let parents = routes::make_parents(&namespace, &resource.spec.inner);

        let backends = resource
            .spec
            .rules
            .into_iter()
            .flat_map(|rule| rule.backend_refs)
            .map(|br| routes::BackendReference::from_backend_ref(&br.inner, &namespace))
            .collect();

        let statuses = resource
            .status
            .into_iter()
            .flat_map(|status| status.inner.parents)
            .collect();

        // Construct route and insert into the index; if the TLSRoute is
        // already in the index, and it hasn't changed, skip creating a patch.
        let route = RouteRef {
            parents,
            backends,
            statuses,
        };
        self.index_route(id, route);
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group: k8s_gateway_api::TlsRoute::group(&()),
                kind: k8s_gateway_api::TlsRoute::kind(&()),
                name: name.into(),
            },
        };
        self.route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::TcpRoute> for Index {
    fn apply(&mut self, resource: k8s_gateway_api::TcpRoute) {
        let namespace = resource
            .namespace()
            .expect("TcpRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = NamespaceGroupKindName {
            namespace: namespace.clone(),
            gkn: GroupKindName {
                group: k8s_gateway_api::TcpRoute::group(&()),
                kind: k8s_gateway_api::TcpRoute::kind(&()),
                name: name.into(),
            },
        };

        // Create the route parents
        let parents = routes::make_parents(&namespace, &resource.spec.inner);

        let backends = resource
            .spec
            .rules
            .into_iter()
            .flat_map(|rule| rule.backend_refs)
            .map(|br| routes::BackendReference::from_backend_ref(&br.inner, &namespace))
            .collect();

        let statuses = resource
            .status
            .into_iter()
            .flat_map(|status| status.inner.parents)
            .collect();

        // Construct route and insert into the index; if the TCPRoute is
        // already in the index, and it hasn't changed, skip creating a patch.
        let route = RouteRef {
            parents,
            backends,
            statuses,
        };
        self.index_route(id, route);
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = NamespaceGroupKindName {
            namespace,
            gkn: GroupKindName {
                group: k8s_gateway_api::TcpRoute::group(&()),
                kind: k8s_gateway_api::TcpRoute::kind(&()),
                name: name.into(),
            },
        };
        self.route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::Server> for Index {
    fn apply(&mut self, resource: linkerd_k8s_api::Server) {
        let namespace = resource.namespace().expect("Server must have a namespace");
        let name = resource.name_unchecked();
        let id = ResourceId::new(namespace, name);

        self.servers.insert(id);

        // If we're not the leader, skip reconciling the cluster.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }
        self.reconcile();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);

        self.servers.remove(&id);

        // If we're not the leader, skip reconciling the cluster.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }
        self.reconcile();
    }

    // Since apply only reindexes a single Server at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s_core_api::Service> for Index {
    fn apply(&mut self, resource: k8s_core_api::Service) {
        let namespace = resource.namespace().expect("Service must have a namespace");
        let name = resource.name_unchecked();
        let id = ResourceId::new(namespace, name);

        self.services.insert(id, resource.into());

        // If we're not the leader, skip reconciling the cluster.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }
        self.reconcile();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);

        self.services.remove(&id);

        // If we're not the leader, skip reconciling the cluster.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }
        self.reconcile();
    }

    // Since apply only reindexes a single Service at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::HTTPLocalRateLimitPolicy> for Index {
    fn apply(&mut self, resource: linkerd_k8s_api::HTTPLocalRateLimitPolicy) {
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

        let rl = HTTPLocalRateLimitPolicyRef {
            creation_timestamp,
            target_ref,
            status_conditions,
        };

        self.index_ratelimit(id, rl);
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);
        self.ratelimits.remove(&id);

        // If we're not the leader, skip reconciling the cluster.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }
        self.reconcile();
    }
}

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::EgressNetwork> for Index {
    fn apply(&mut self, resource: linkerd_k8s_api::EgressNetwork) {
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

        // If we're not the leader, skip reconciling the cluster.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }
        self.reconcile();
    }
}

impl Index {
    fn index_egress_network(&mut self, id: ResourceId, net: EgressNetworkRef) {
        // Insert into the index; if the network is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_egress_net(id, &net) {
            return;
        }

        // If we're not the leader, skip creating a patch and sending an
        // update to the Controller.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }

        self.reconcile()
    }

    fn index_route(&mut self, id: NamespaceGroupKindName, route: RouteRef) {
        // Insert into the index; if the route is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_route(id.clone(), &route) {
            return;
        }

        // If we're not the leader, skip creating a patch and sending an
        // update to the Controller.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }

        self.reconcile()
    }

    fn index_ratelimit(&mut self, id: ResourceId, ratelimit: HTTPLocalRateLimitPolicyRef) {
        // Insert into the index; if the route is already in the index, and it hasn't
        // changed, skip creating a patch.
        if !self.update_ratelimit(id.clone(), &ratelimit) {
            return;
        }

        // If we're not the leader, skip creating a patch and sending an
        // update to the Controller.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }

        self.reconcile()
    }
}

pub(crate) fn make_patch<Status>(
    resource_id: &NamespaceGroupKindName,
    status: Status,
) -> Option<k8s_core_api::Patch<serde_json::Value>>
where
    Status: serde::Serialize,
{
    match resource_id.api_version() {
        Err(error) => {
            tracing::error!(error = %error, "failed to create patch for resource");
            None
        }
        Ok(api_version) => {
            let patch = serde_json::json!({
                "apiVersion": api_version,
                    "kind": &resource_id.gkn.kind,
                    "name": &resource_id.gkn.name,
                    "status": status,
            });

            Some(k8s_core_api::Patch::Merge(patch))
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

pub(crate) fn no_matching_parent() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_PARENT.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn no_matching_target() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_TARGET.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

fn headless_parent() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "parent service must have a ClusterIP".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_PARENT.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

fn egress_net_not_accepted() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "EgressNetwork parent has not been accepted".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_PARENT.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn route_conflicted() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::ROUTE_REASON_CONFLICTED.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn ratelimit_already_exists() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::RATELIMIT_REASON_ALREADY_EXISTS.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn accepted() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: conditions::ACCEPTED.to_string(),
        status: cond_statuses::STATUS_TRUE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn in_cluster_net_overlap() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "networks overlap with clusterNetworks".to_string(),
        observed_generation: None,
        reason: reasons::EGRESS_NET_REASON_OVERLAP.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

pub(crate) fn resolved_refs() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::RESOLVED_REFS.to_string(),
        status: cond_statuses::STATUS_TRUE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

pub(crate) fn backend_not_found() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::BACKEND_NOT_FOUND.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

pub(crate) fn invalid_backend_kind(message: &str) -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: message.to_string(),
        observed_generation: None,
        reason: reasons::INVALID_KIND.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

fn eq_time_insensitive_route_parent_statuses(
    left: &[k8s_gateway_api::RouteParentStatus],
    right: &[k8s_gateway_api::RouteParentStatus],
) -> bool {
    if left.len() != right.len() {
        return false;
    }
    left.iter().zip(right.iter()).all(|(l, r)| {
        l.parent_ref == r.parent_ref
            && l.controller_name == r.controller_name
            && eq_time_insensitive_conditions(&l.conditions, &r.conditions)
    })
}

fn eq_time_insensitive_conditions(
    left: &[k8s_core_api::Condition],
    right: &[k8s_core_api::Condition],
) -> bool {
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
