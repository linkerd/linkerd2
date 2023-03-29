use crate::{
    http_route::{self, BackendReference, ParentReference},
    resource_id::ResourceId,
};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use chrono::offset::Utc;
use chrono::DateTime;
use kubert::lease::Claim;
use linkerd_policy_controller_core::POLICY_CONTROLLER_NAME;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, ResourceExt};
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::{
    sync::{
        mpsc::{UnboundedReceiver, UnboundedSender},
        watch::Receiver,
    },
    time::{self, Duration},
};

pub(crate) const POLICY_API_GROUP: &str = "policy.linkerd.io";
const POLICY_API_VERSION: &str = "policy.linkerd.io/v1alpha1";

mod conditions {
    pub const RESOLVED_REFS: &str = "ResolvedRefs";
    pub const ACCEPTED: &str = "Accepted";
}

// Condition reasons
const BACKEND_NOT_FOUND: &str = "BackendNotFound";
const INVALID_KIND: &str = "InvalidKind";
const NO_MATCHING_PARENT: &str = "NoMatchingParent";

// Condition status
const STATUS_TRUE: &str = "True";
const STATUS_FALSE: &str = "False";

pub type SharedIndex = Arc<RwLock<Index>>;

pub struct Controller {
    claims: Receiver<Arc<Claim>>,
    client: k8s::Client,
    name: String,
    updates: UnboundedReceiver<Update>,

    /// True if this policy controller is the leader — false otherwise.
    leader: bool,
}

pub struct Index {
    /// Used to compare against the current claim's claimant to determine if
    /// this policy controller is the leader.
    name: String,

    /// Used in the IndexNamespacedResource trait methods to check who the
    /// current leader is and if updates should be sent to the Controller.
    claims: Receiver<Arc<Claim>>,
    updates: UnboundedSender<Update>,

    /// Maps HttpRoute ids to a list of their parent and backend refs,
    /// regardless of if those parents have accepted the route.
    http_route_refs: HashMap<ResourceId, References>,
    servers: HashSet<ResourceId>,
    services: HashSet<ResourceId>,
}

#[derive(Clone, PartialEq)]
struct References {
    parents: Vec<ParentReference>,
    backends: Vec<BackendReference>,
}

#[derive(Debug, PartialEq)]
pub struct Update {
    pub id: ResourceId,
    pub patch: k8s::Patch<serde_json::Value>,
}

impl Controller {
    pub fn new(
        claims: Receiver<Arc<Claim>>,
        client: k8s::Client,
        name: String,
        updates: UnboundedReceiver<Update>,
    ) -> Self {
        Self {
            claims,
            client,
            name,
            updates,
            leader: false,
        }
    }

    /// Process updates received from the index; each update is a patch that
    /// should be applied to update the status of an HTTPRoute. A patch should
    /// only be applied if we are the holder of the write lease.
    pub async fn run(mut self) {
        let patch_params = k8s::PatchParams::apply(POLICY_API_GROUP);

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
                    tracing::debug!("Lease holder has changed");
                    let claim = self.claims.borrow_and_update();
                    self.leader = claim.is_current_for(&self.name);
                }
                // If this policy controller is not the leader, it should
                // process through the updates queue but not actually patch
                // any resources.
                Some(Update { id, patch}) = self.updates.recv(), if self.leader => {
                    let api = k8s::Api::<k8s::policy::HttpRoute>::namespaced(self.client.clone(), &id.namespace);
                    if let Err(error) = api.patch_status(&id.name, &patch_params, &patch).await {
                        tracing::error!(namespace = %id.namespace, name = %id.name, %error, "Failed to patch HTTPRoute");
                    }
                }
            }
        }
    }
}

impl Index {
    pub fn shared(
        name: impl ToString,
        claims: Receiver<Arc<Claim>>,
        updates: UnboundedSender<Update>,
    ) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            name: name.to_string(),
            claims,
            updates,
            http_route_refs: HashMap::new(),
            servers: HashSet::new(),
            services: HashSet::new(),
        }))
    }

    /// When the write lease holder changes or a time duration has elapsed,
    /// the index reconciles the statuses for all HTTPRoutes on the cluster.
    ///
    /// This reconciliation loop ensures that if errors occur when the
    /// Controller applies patches or the write lease holder changes, all
    /// HTTPRoutes have an up-to-date status.
    pub async fn run(index: Arc<RwLock<Self>>) {
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
                _ = time::sleep(Duration::from_secs(10)) => {}
            }

            // The claimant has changed, or we should attempt to reconcile all
            // HTTPRoutes to account for any errors. In either case, we should
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

    // If the route is new or its parentRefs and/or backendRefs have changed,
    // return true, so that a patch is generated; otherwise return false.
    fn update_http_route(&mut self, id: ResourceId, references: &References) -> bool {
        match self.http_route_refs.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(references.clone());
            }
            Entry::Occupied(mut entry) => {
                if entry.get() == references {
                    return false;
                }
                entry.insert(references.clone());
            }
        }
        true
    }

    fn parent_status(
        &self,
        parent_ref: &ParentReference,
        backend_condition: k8s::Condition,
    ) -> Option<gateway::RouteParentStatus> {
        match parent_ref {
            ParentReference::Server(server) => {
                let condition = if self.servers.contains(server) {
                    accepted()
                } else {
                    no_matching_parent()
                };

                Some(gateway::RouteParentStatus {
                    parent_ref: gateway::ParentReference {
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
            ParentReference::Service(service, port) => {
                let condition = if self.services.contains(service) {
                    accepted()
                } else {
                    no_matching_parent()
                };

                Some(gateway::RouteParentStatus {
                    parent_ref: gateway::ParentReference {
                        group: None,
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
            ParentReference::UnknownKind => None,
        }
    }

    fn backend_condition(&self, backend_refs: &[BackendReference]) -> k8s::Condition {
        // If even one backend has a reference to an unknown / unsupported
        // reference, return invalid backend condition
        if backend_refs
            .iter()
            .any(|reference| matches!(reference, BackendReference::Unknown))
        {
            return invalid_backend_kind();
        }

        // If all references have been resolved (i.e exist in our services cache),
        // return positive status, otherwise, one of them does not exist
        if backend_refs.iter().any(|backend_ref| match backend_ref {
            BackendReference::Service(service) => self.services.contains(service),
            _ => false,
        }) {
            resolved_refs()
        } else {
            backend_not_found()
        }
    }

    fn make_http_route_patch(
        &self,
        id: &ResourceId,
        parents: &[ParentReference],
        backends: &[BackendReference],
    ) -> k8s::Patch<serde_json::Value> {
        let backend_condition = self.backend_condition(backends);
        let parent_statuses = parents
            .iter()
            .filter_map(|parent_ref| self.parent_status(parent_ref, backend_condition.clone()))
            .collect();
        let status = gateway::HttpRouteStatus {
            inner: gateway::RouteStatus {
                parents: parent_statuses,
            },
        };
        make_patch(&id.name, status)
    }

    fn reconcile(&self) {
        for (id, references) in self.http_route_refs.iter() {
            let patch = self.make_http_route_patch(id, &references.parents, &references.backends);
            if let Err(error) = self.updates.send(Update {
                id: id.clone(),
                patch,
            }) {
                tracing::error!(%id.namespace, %id.name, %error, "Failed to send HTTPRoute patch")
            }
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s::policy::HttpRoute> for Index {
    fn apply(&mut self, resource: k8s::policy::HttpRoute) {
        let namespace = resource
            .namespace()
            .expect("HTTPRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = ResourceId::new(namespace, name);

        // Create the route parents
        let parents = http_route::make_parents(&resource);

        // Create the route backends
        let backends = http_route::make_backends(&resource);

        // Construct references and insert into the index; if the HTTPRoute is
        // already in the index and it hasn't changed, skip creating a patch.
        let references = References { parents, backends };
        if !self.update_http_route(id.clone(), &references) {
            return;
        }

        // If we're not the leader, skip creating a patch and sending an
        // update to the Controller.
        if !self.claims.borrow().is_current_for(&self.name) {
            tracing::debug!(%self.name, "Lease non-holder skipping controller update");
            return;
        }

        // Create a patch for the HTTPRoute and send it to the Controller so
        // that it is applied.
        let patch = self.make_http_route_patch(&id, &references.parents, &references.backends);
        if let Err(error) = self.updates.send(Update {
            id: id.clone(),
            patch,
        }) {
            tracing::error!(%id.namespace, %id.name, %error, "Failed to send HTTPRoute patch")
        }
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);
        self.http_route_refs.remove(&id);
    }

    // Since apply only reindexes a single HTTPRoute at a time, there's no need
    // to handle resets specially.
}

impl kubert::index::IndexNamespacedResource<k8s::policy::Server> for Index {
    fn apply(&mut self, resource: k8s::policy::Server) {
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

impl kubert::index::IndexNamespacedResource<k8s::Service> for Index {
    fn apply(&mut self, resource: k8s::Service) {
        let namespace = resource.namespace().expect("Service must have a namespace");
        let name = resource.name_unchecked();
        let id = ResourceId::new(namespace, name);

        self.services.insert(id);

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

pub(crate) fn make_patch(
    name: &str,
    status: gateway::HttpRouteStatus,
) -> k8s::Patch<serde_json::Value> {
    let value = serde_json::json!({
        "apiVersion": POLICY_API_VERSION,
            "kind": "HTTPRoute",
            "name": name,
            "status": status,
    });
    k8s::Patch::Merge(value)
}

fn now() -> DateTime<Utc> {
    #[cfg(not(test))]
    let now = Utc::now();
    #[cfg(test)]
    let now = DateTime::<Utc>::MIN_UTC;
    now
}

fn no_matching_parent() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: NO_MATCHING_PARENT.to_string(),
        status: STATUS_FALSE.to_string(),
        type_: ACCEPTED.to_string(),
    }
}

fn accepted() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: ACCEPTED.to_string(),
        status: STATUS_TRUE.to_string(),
        type_: ACCEPTED.to_string(),
    }
}

fn resolved_refs() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: RESOLVED_REFS.to_string(),
        status: STATUS_TRUE.to_string(),
        type_: RESOLVED_REFS.to_string(),
    }
}

fn backend_not_found() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: BACKEND_NOT_FOUND.to_string(),
        status: STATUS_FALSE.to_string(),
        type_: RESOLVED_REFS.to_string(),
    }
}

fn invalid_backend_kind() -> k8s::Condition {
    k8s::Condition {
        last_transition_time: k8s::Time(now()),
        message: "".to_string(),
        observed_generation: None,
        reason: INVALID_KIND.to_string(),
        status: STATUS_FALSE.to_string(),
        type_: RESOLVED_REFS.to_string(),
    }
}
