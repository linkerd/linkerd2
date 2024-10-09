use crate::{
    resource_id::{NamespaceGroupKindName, ResourceId},
    routes,
    service::Service,
};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use chrono::{offset::Utc, DateTime};
use kubert::lease::Claim;
use linkerd_policy_controller_core::{routes::GroupKindName, POLICY_CONTROLLER_NAME};
use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api, gateway as k8s_gateway_api, policy as linkerd_k8s_api,
    NamespaceResourceScope, Resource, ResourceExt,
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
    pub const ROUTE_REASON_CONFLICTED: &str = "RouteReasonConflicted";
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
    servers: HashSet<ResourceId>,
    services: HashMap<ResourceId, Service>,
    unmeshed_nets: HashSet<ResourceId>,

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
                        } else if id.gkn.group == k8s_gateway_api::TlsRoute::group(&()) && id.gkn.kind == k8s_gateway_api::TlsRoute::kind(&()) {
                            self.patch_status::<k8s_gateway_api::TlsRoute>(&id.gkn.name, &id.namespace, patch).await;
                        } else if id.gkn.group == k8s_gateway_api::TcpRoute::group(&()) && id.gkn.kind == k8s_gateway_api::TcpRoute::kind(&()) {
                            self.patch_status::<k8s_gateway_api::TcpRoute>(&id.gkn.name, &id.namespace, patch).await;
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
    ) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            name: name.to_string(),
            claims,
            updates,
            route_refs: HashMap::new(),
            servers: HashSet::new(),
            services: HashMap::new(),
            unmeshed_nets: HashSet::new(),
            metrics,
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

    fn parent_status(
        &self,
        id: &NamespaceGroupKindName,
        parent_ref: &routes::ParentReference,
        backend_condition: k8s_core_api::Condition,
    ) -> Option<k8s_gateway_api::RouteParentStatus> {
        match parent_ref {
            routes::ParentReference::Server(server) => {
                let condition = if self.servers.contains(server) {
                    // If this route is an HTTPRoute and there exists a GRPCRoute
                    // with the same parent, the HTTPRoute should not be accepted
                    // because it is less specific.
                    // https://gateway-api.sigs.k8s.io/geps/gep-1426/#route-types
                    if id.gkn.kind == k8s_gateway_api::HttpRoute::kind(&())
                        && self.parent_has_grpcroute_children(parent_ref)
                    {
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
                        if parent_has_conflicting_routes(
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
            routes::ParentReference::UnmeshedNetwork(unet, port) => {
                // unmeshed network is a valid parent if it exists.
                let condition = if self.unmeshed_nets.contains(unet) {
                    if parent_has_conflicting_routes(
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
                        group: Some("policy.linkerd.io".to_string()),
                        kind: Some("UnmeshedNetwork".to_string()),
                        namespace: Some(unet.namespace.clone()),
                        name: unet.name.clone(),
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

    fn parent_has_grpcroute_children(&self, parent_ref: &routes::ParentReference) -> bool {
        self.route_refs.iter().any(|(id, route)| {
            id.gkn.kind == k8s_gateway_api::GrpcRoute::kind(&())
                && route.parents.contains(parent_ref)
        })
    }

    fn backend_condition(
        &self,
        backend_refs: &[routes::BackendReference],
    ) -> k8s_core_api::Condition {
        // If even one backend has a reference to an unknown / unsupported
        // reference, return invalid backend condition
        if backend_refs
            .iter()
            .any(|reference| matches!(reference, routes::BackendReference::Unknown))
        {
            return invalid_backend_kind();
        }

        // If all references have been resolved (i.e. exist in our services cache),
        // return positive status, otherwise, one of them does not exist
        if backend_refs.iter().all(|backend_ref| match backend_ref {
            routes::BackendReference::Service(service) => self.services.contains_key(service),
            routes::BackendReference::UnmeshedNetwork(unet) => self.unmeshed_nets.contains(unet),
            _ => false,
        }) {
            resolved_refs()
        } else {
            backend_not_found()
        }
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
        let backend_condition = self.backend_condition(&route.backends);
        let parent_statuses = route
            .parents
            .iter()
            .filter_map(|parent_ref| self.parent_status(id, parent_ref, backend_condition.clone()));

        let all_statuses = unowned_statuses.chain(parent_statuses).collect::<Vec<_>>();

        if eq_time_insensitive(&all_statuses, &route.statuses) {
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
            (GATEWAY_API_GROUP, "TCPRoute") => {
                let status = k8s_gateway_api::TcpRouteStatus {
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
            _ => None,
        }
    }

    fn reconcile(&self) {
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

impl kubert::index::IndexNamespacedResource<linkerd_k8s_api::UnmeshedNetwork> for Index {
    fn apply(&mut self, resource: linkerd_k8s_api::UnmeshedNetwork) {
        let namespace = resource
            .namespace()
            .expect("UnmeshedNetwork must have a namespace");
        let name = resource.name_unchecked();
        let id = ResourceId::new(namespace, name);

        self.unmeshed_nets.insert(id);

        // If we're not the leader, skip reconciling the cluster.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }
        self.reconcile();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);

        self.unmeshed_nets.remove(&id);

        // If we're not the leader, skip reconciling the cluster.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }
        self.reconcile();
    }

    // Since apply only reindexes a single UnmeshedNetwork at a time, there's no need
    // to handle resets specially.
}

impl Index {
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
}

pub(crate) fn make_patch<RouteStatus>(
    route_id: &NamespaceGroupKindName,
    status: RouteStatus,
) -> Option<k8s_core_api::Patch<serde_json::Value>>
where
    RouteStatus: serde::Serialize,
{
    match route_id.api_version() {
        Err(error) => {
            tracing::error!(error = %error, "failed to create patch for route");
            None
        }
        Ok(api_version) => {
            let patch = serde_json::json!({
                "apiVersion": api_version,
                    "kind": &route_id.gkn.kind,
                    "name": &route_id.gkn.name,
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

// This method determines whether a parent that a route attempts to
// attach to has any routes attached that are in conflict with the one
// that we are about to attach. This is done following the logs outlined in:
// https://gateway-api.sigs.k8s.io/geps/gep-1426/#route-types
fn parent_has_conflicting_routes<'p>(
    mut existing_routes: impl Iterator<Item = (&'p NamespaceGroupKindName, &'p RouteRef)>,
    parent_ref: &routes::ParentReference,
    candidate_kind: &str,
) -> bool {
    let grpc_kind = k8s_gateway_api::GrpcRoute::kind(&());
    let http_kind = k8s_gateway_api::HttpRoute::kind(&());
    let tls_kind = k8s_gateway_api::TlsRoute::kind(&());

    let more_specific_routes: HashSet<_> = if *candidate_kind == grpc_kind {
        vec![]
    } else if *candidate_kind == http_kind {
        vec![grpc_kind]
    } else if *candidate_kind == tls_kind {
        vec![grpc_kind, http_kind]
    } else {
        vec![grpc_kind, http_kind, tls_kind]
    }
    .into_iter()
    .collect();

    existing_routes.any(|(id, route)| {
        more_specific_routes.contains(&id.gkn.kind) && route.parents.contains(parent_ref)
    })
}

fn no_matching_parent() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::NO_MATCHING_PARENT.to_string(),
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

fn route_conflicted() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::ROUTE_REASON_CONFLICTED.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

fn accepted() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: conditions::ACCEPTED.to_string(),
        status: cond_statuses::STATUS_TRUE.to_string(),
        type_: conditions::ACCEPTED.to_string(),
    }
}

fn resolved_refs() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::RESOLVED_REFS.to_string(),
        status: cond_statuses::STATUS_TRUE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

fn backend_not_found() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::BACKEND_NOT_FOUND.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

fn invalid_backend_kind() -> k8s_core_api::Condition {
    k8s_core_api::Condition {
        last_transition_time: k8s_core_api::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: reasons::INVALID_KIND.to_string(),
        status: cond_statuses::STATUS_FALSE.to_string(),
        type_: conditions::RESOLVED_REFS.to_string(),
    }
}

fn eq_time_insensitive(
    left: &[k8s_gateway_api::RouteParentStatus],
    right: &[k8s_gateway_api::RouteParentStatus],
) -> bool {
    if left.len() != right.len() {
        return false;
    }
    left.iter().zip(right.iter()).all(|(l, r)| {
        l.parent_ref == r.parent_ref
            && l.controller_name == r.controller_name
            && l.conditions.len() == r.conditions.len()
            && l.conditions.iter().zip(r.conditions.iter()).all(|(l, r)| {
                l.message == r.message
                    && l.observed_generation == r.observed_generation
                    && l.reason == r.reason
                    && l.status == r.status
                    && l.type_ == r.type_
            })
    })
}

#[cfg(test)]
mod test {
    use super::*;
    use ahash::HashMap;
    use linkerd_policy_controller_k8s_api::gateway as k8s_gateway_api;
    use std::vec;

    enum ParentRefType {
        Service,
        UnmeshedNetwork,
    }

    fn grpc_route_no_conflict(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::GrpcRoute::group(&()),
                        kind: k8s_gateway_api::GrpcRoute::kind(&()),
                        name: "grpc-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::HttpRoute::group(&()),
                        kind: k8s_gateway_api::HttpRoute::kind(&()),
                        name: "http-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TlsRoute::group(&()),
                        kind: k8s_gateway_api::TlsRoute::kind(&()),
                        name: "tls-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TcpRoute::group(&()),
                        kind: k8s_gateway_api::TcpRoute::kind(&()),
                        name: "tcp-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
        ]
        .into_iter()
        .collect();

        assert!(!parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "GRPCRoute"
        ));
    }

    fn http_route_conflict_grpc(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![(
            NamespaceGroupKindName {
                namespace: "default".to_string(),
                gkn: GroupKindName {
                    group: k8s_gateway_api::GrpcRoute::group(&()),
                    kind: k8s_gateway_api::GrpcRoute::kind(&()),
                    name: "grpc-1".into(),
                },
            },
            RouteRef {
                parents: vec![parent.clone()],
                statuses: vec![],
                backends: vec![],
            },
        )]
        .into_iter()
        .collect();

        assert!(parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "HTTPRoute"
        ));
    }

    fn http_route_no_conflict(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::HttpRoute::group(&()),
                        kind: k8s_gateway_api::HttpRoute::kind(&()),
                        name: "http-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TlsRoute::group(&()),
                        kind: k8s_gateway_api::TlsRoute::kind(&()),
                        name: "tls-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TcpRoute::group(&()),
                        kind: k8s_gateway_api::TcpRoute::kind(&()),
                        name: "tcp-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
        ]
        .into_iter()
        .collect();

        assert!(!parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "HTTPRoute"
        ));
    }

    fn tls_route_conflict_http(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![(
            NamespaceGroupKindName {
                namespace: "default".to_string(),
                gkn: GroupKindName {
                    group: k8s_gateway_api::HttpRoute::group(&()),
                    kind: k8s_gateway_api::HttpRoute::kind(&()),
                    name: "http-1".into(),
                },
            },
            RouteRef {
                parents: vec![parent.clone()],
                statuses: vec![],
                backends: vec![],
            },
        )]
        .into_iter()
        .collect();

        assert!(parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "TLSRoute"
        ));
    }

    fn tls_route_conflict_grpc(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![(
            NamespaceGroupKindName {
                namespace: "default".to_string(),
                gkn: GroupKindName {
                    group: k8s_gateway_api::GrpcRoute::group(&()),
                    kind: k8s_gateway_api::GrpcRoute::kind(&()),
                    name: "grpc-1".into(),
                },
            },
            RouteRef {
                parents: vec![parent.clone()],
                statuses: vec![],
                backends: vec![],
            },
        )]
        .into_iter()
        .collect();

        assert!(parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "TLSRoute"
        ));
    }

    fn tls_route_no_conflict(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };
        let known_routes: HashMap<_, _> = vec![
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TlsRoute::group(&()),
                        kind: k8s_gateway_api::TlsRoute::kind(&()),
                        name: "tls-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
            (
                NamespaceGroupKindName {
                    namespace: "default".to_string(),
                    gkn: GroupKindName {
                        group: k8s_gateway_api::TcpRoute::group(&()),
                        kind: k8s_gateway_api::TcpRoute::kind(&()),
                        name: "tcp-1".into(),
                    },
                },
                RouteRef {
                    parents: vec![parent.clone()],
                    statuses: vec![],
                    backends: vec![],
                },
            ),
        ]
        .into_iter()
        .collect();

        assert!(!parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "TLSRoute"
        ));
    }

    fn tcp_route_conflict_grpc(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![(
            NamespaceGroupKindName {
                namespace: "default".to_string(),
                gkn: GroupKindName {
                    group: k8s_gateway_api::GrpcRoute::group(&()),
                    kind: k8s_gateway_api::GrpcRoute::kind(&()),
                    name: "grpc-1".into(),
                },
            },
            RouteRef {
                parents: vec![parent.clone()],
                statuses: vec![],
                backends: vec![],
            },
        )]
        .into_iter()
        .collect();

        assert!(parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "TCPRoute"
        ));
    }

    fn tcp_route_conflict_http(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![(
            NamespaceGroupKindName {
                namespace: "default".to_string(),
                gkn: GroupKindName {
                    group: k8s_gateway_api::HttpRoute::group(&()),
                    kind: k8s_gateway_api::HttpRoute::kind(&()),
                    name: "http-1".into(),
                },
            },
            RouteRef {
                parents: vec![parent.clone()],
                statuses: vec![],
                backends: vec![],
            },
        )]
        .into_iter()
        .collect();

        assert!(parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "TCPRoute"
        ));
    }

    fn tcp_route_conflict_tls(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![(
            NamespaceGroupKindName {
                namespace: "default".to_string(),
                gkn: GroupKindName {
                    group: k8s_gateway_api::TlsRoute::group(&()),
                    kind: k8s_gateway_api::TlsRoute::kind(&()),
                    name: "tls-1".into(),
                },
            },
            RouteRef {
                parents: vec![parent.clone()],
                statuses: vec![],
                backends: vec![],
            },
        )]
        .into_iter()
        .collect();

        assert!(parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "TCPRoute"
        ));
    }

    fn tcp_route_no_conflict(p: ParentRefType) {
        let parent = match p {
            ParentRefType::Service => routes::ParentReference::Service(
                ResourceId::new("ns".to_string(), "service".to_string()),
                None,
            ),

            ParentRefType::UnmeshedNetwork => routes::ParentReference::UnmeshedNetwork(
                ResourceId::new("ns".to_string(), "unmeshed-net".to_string()),
                None,
            ),
        };

        let known_routes: HashMap<_, _> = vec![(
            NamespaceGroupKindName {
                namespace: "default".to_string(),
                gkn: GroupKindName {
                    group: k8s_gateway_api::TcpRoute::group(&()),
                    kind: k8s_gateway_api::TcpRoute::kind(&()),
                    name: "tcp-1".into(),
                },
            },
            RouteRef {
                parents: vec![parent.clone()],
                statuses: vec![],
                backends: vec![],
            },
        )]
        .into_iter()
        .collect();

        assert!(!parent_has_conflicting_routes(
            &mut known_routes.iter(),
            &parent,
            "TCPRoute"
        ));
    }

    #[test]
    fn grpc_route_no_conflict_service() {
        grpc_route_no_conflict(ParentRefType::Service)
    }

    #[test]
    fn http_route_conflict_grpc_service() {
        http_route_conflict_grpc(ParentRefType::Service)
    }

    #[test]
    fn http_route_no_conflict_service() {
        http_route_no_conflict(ParentRefType::Service)
    }

    #[test]
    fn tls_route_conflict_http_service() {
        tls_route_conflict_http(ParentRefType::Service)
    }

    #[test]
    fn tls_route_conflict_grpc_service() {
        tls_route_conflict_grpc(ParentRefType::Service)
    }

    #[test]
    fn tls_route_no_conflict_service() {
        tls_route_no_conflict(ParentRefType::Service)
    }

    #[test]
    fn tcp_route_conflict_grpc_service() {
        tcp_route_conflict_grpc(ParentRefType::Service)
    }

    #[test]
    fn tcp_route_conflict_http_service() {
        tcp_route_conflict_http(ParentRefType::Service)
    }

    #[test]
    fn tcp_route_conflict_tls_service() {
        tcp_route_conflict_tls(ParentRefType::Service)
    }

    #[test]
    fn tcp_route_no_conflict_service() {
        tcp_route_no_conflict(ParentRefType::Service)
    }

    #[test]
    fn grpc_route_no_conflict_unmeshed_network() {
        grpc_route_no_conflict(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn http_route_conflict_grpc_unmeshed_network() {
        http_route_conflict_grpc(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn http_route_no_conflict_unmeshed_network() {
        http_route_no_conflict(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn tls_route_conflict_http_unmeshed_network() {
        tls_route_conflict_http(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn tls_route_conflict_grpc_unmeshed_network() {
        tls_route_conflict_grpc(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn tls_route_no_conflict_unmeshed_network() {
        tls_route_no_conflict(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn tcp_route_conflict_grpc_unmeshed_network() {
        tcp_route_conflict_grpc(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn tcp_route_conflict_http_unmeshed_network() {
        tcp_route_conflict_http(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn tcp_route_conflict_tls_unmeshed_network() {
        tcp_route_conflict_tls(ParentRefType::UnmeshedNetwork)
    }

    #[test]
    fn tcp_route_no_conflict_unmeshed_network() {
        tcp_route_no_conflict(ParentRefType::UnmeshedNetwork)
    }
}
