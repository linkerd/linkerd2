use crate::{
    http_route::{self, BackendReference, ParentReference},
    resource_id::ResourceId,
};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
#[cfg(not(test))]
use chrono::offset::Utc;
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
const POLICY_API_VERSION: &str = "policy.linkerd.io/v1beta2";

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

    http_routes: HashMap<ResourceId, RouteReference>,
    servers: HashSet<ResourceId>,
    services: HashSet<ResourceId>,
}

#[derive(Clone, PartialEq, Eq)]
struct RouteReference {
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
                _ = self.claims.changed() => {
                    let claim = self.claims.borrow_and_update();
                    self.leader =  claim.is_current_for(&self.name)
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
            http_routes: HashMap::new(),
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
                _ = claims.changed() => {
                    tracing::debug!("Lease holder has changed")
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

    // If the route is new or its parents have changed, return true so that a
    // patch is generated; otherwise return false.
    fn update_http_route(&mut self, id: ResourceId, route_refs: RouteReference) -> bool {
        match self.http_routes.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(route_refs);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() == route_refs {
                    return false;
                }
                entry.insert(route_refs);
            }
        };

        true
    }

    fn make_http_route_patch(
        &self,
        id: &ResourceId,
        route_ref: &RouteReference,
    ) -> k8s::Patch<serde_json::Value> {
        #[cfg(not(test))]
        let timestamp = Utc::now();
        #[cfg(test)]
        let timestamp = chrono::DateTime::<chrono::Utc>::MIN_UTC;

        let RouteReference { parents, backends } = route_ref;

        let backend_condition = {
            let mut resolved_all = true;
            for backend in backends.iter() {
                let BackendReference::Service(backend_reference_id) = backend;
                if !self.services.contains(backend_reference_id) {
                    resolved_all = false;
                    break;
                }
            }

            BackendReference::into_status_condition(resolved_all, timestamp)
        };

        let parent_statuses = parents
            .iter()
            .map(|parent| {
                let ParentReference::Server(parent_reference_id) = parent;
                // Is this parent in the list of parents which accept
                // the route?
                let accepted = self
                    .servers
                    .iter()
                    .any(|server| server == parent_reference_id);
                let condition = ParentReference::into_status_condition(accepted, timestamp);

                gateway::RouteParentStatus {
                    parent_ref: gateway::ParentReference {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(parent_reference_id.namespace.clone()),
                        name: parent_reference_id.name.clone(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: vec![condition, backend_condition.clone()],
                }
            })
            .collect();

        let status = gateway::HttpRouteStatus {
            inner: gateway::RouteStatus {
                parents: parent_statuses,
            },
        };
        make_patch(&id.name, status)
    }

    fn reconcile(&self) {
        for (id, references) in self.http_routes.iter() {
            let patch = self.make_http_route_patch(id, references);
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
        let id = ResourceId::new(namespace.clone(), name.clone());

        // Create the route parents and insert it into the index. If the
        // HTTPRoute is already in the index and it hasn't changed, skip
        // creating a patch.
        let route_refs = {
            let parents = match http_route::make_parents(&resource, &namespace) {
                Ok(parents) => parents,
                Err(error) => {
                    tracing::info!(%namespace, %name, %error, "Ignoring HTTPRoute");
                    return;
                }
            };

            let backends = match http_route::make_backends(&resource, &namespace) {
                Ok(backends) => backends,
                Err(error) => {
                    tracing::info!(%namespace, %name, %error, "Ignoring HTTPRoute");
                    return;
                }
            };

            RouteReference { parents, backends }
        };

        if !self.update_http_route(id.clone(), route_refs.clone()) {
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
        let patch = self.make_http_route_patch(&id, &route_refs);
        if let Err(error) = self.updates.send(Update {
            id: id.clone(),
            patch,
        }) {
            tracing::error!(%id.namespace, %id.name, %error, "Failed to send HTTPRoute patch")
        }
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);
        self.http_routes.remove(&id);
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
        // Don't process kube-system Service objects
        let name = resource.name_unchecked();
        let id = ResourceId::new(namespace, name);

        if id.namespace != "kube-system" {
            self.services.insert(id);
            // If we're not the leader, skip reconciling the cluster.
            if !self.claims.borrow().is_current_for(&self.name) {
                tracing::debug!(%self.name, "Lease non-holder skipping controller update");
                return;
            }
            self.reconcile();
        }
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
