use crate::{
    http_route::{self, BackendReference, ParentReference},
    resource_id::ResourceId,
};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
#[cfg(not(test))]
use chrono::offset::Utc;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, ResourceExt};
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::sync::mpsc::{UnboundedReceiver, UnboundedSender};

pub(crate) const POLICY_API_GROUP: &str = "policy.linkerd.io";
const POLICY_API_VERSION: &str = "policy.linkerd.io/v1alpha1";
pub(crate) const STATUS_CONTROLLER_NAME: &str = "policy.linkerd.io/status-controller";

pub type SharedIndex = Arc<RwLock<Index>>;

pub struct Controller {
    client: k8s::Client,
    updates: UnboundedReceiver<Update>,
}

pub struct Index {
    updates: UnboundedSender<Update>,

    http_routes: HashMap<ResourceId, Vec<ParentReference>>,
    route_backends: HashMap<ResourceId, Vec<BackendReference>>,
    servers: HashSet<ResourceId>,
    services: HashSet<ResourceId>,
}

#[derive(Debug, PartialEq)]
pub struct Update {
    pub id: ResourceId,
    pub patch: k8s::Patch<serde_json::Value>,
}

impl Controller {
    pub fn new(client: k8s::Client, updates: UnboundedReceiver<Update>) -> Self {
        Self { client, updates }
    }

    pub async fn process_updates(mut self) {
        let patch_params = k8s::PatchParams::apply("policy.linkerd.io");

        // todo: If an update fails we should figure out a requeueing strategy
        while let Some(Update { id, patch }) = self.updates.recv().await {
            let api =
                k8s::Api::<k8s::policy::HttpRoute>::namespaced(self.client.clone(), &id.namespace);

            // todo: Do we need to consider a timeout here?
            if let Err(error) = api.patch_status(&id.name, &patch_params, &patch).await {
                tracing::error!(namespace = %id.namespace, name = %id.name, %error, "Failed to patch HTTPRoute");
            }
        }
    }
}

impl Index {
    pub fn shared(updates: UnboundedSender<Update>) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            updates,
            http_routes: HashMap::new(),
            route_backends: HashMap::new(),
            servers: HashSet::new(),
            services: HashSet::new(),
        }))
    }

    // If the route is new or its parents have changed, return true so that a
    // patch is generated; otherwise return false.
    fn update_http_route_parents(&mut self, id: ResourceId, parents: Vec<ParentReference>) -> bool {
        match self.http_routes.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(parents);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() == parents {
                    return false;
                }
                entry.insert(parents);
            }
        };

        true
    }

    fn update_http_route_backends(
        &mut self,
        id: ResourceId,
        backends: Vec<BackendReference>,
    ) -> bool {
        match self.route_backends.entry(id) {
            Entry::Vacant(entry) => {
                entry.insert(backends);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() == backends {
                    return false;
                }
                entry.insert(backends);
            }
        };

        true
    }

    fn make_http_route_patch(
        &self,
        id: &ResourceId,
        parents: &[ParentReference],
        backends: &[BackendReference],
    ) -> k8s::Patch<serde_json::Value> {
        let conditions = parents
            .iter()
            .map(|parent| {
                let ParentReference::Server(parent_reference_id) = parent;

                #[cfg(not(test))]
                let timestamp = Utc::now();
                #[cfg(test)]
                let timestamp = chrono::DateTime::<chrono::Utc>::MIN_UTC;

                // Is this parent in the list of parents which accept
                // the route?
                let accepted = self
                    .servers
                    .iter()
                    .any(|server| server == parent_reference_id);
                let cond = if accepted {
                    k8s::Condition {
                        last_transition_time: k8s::Time(timestamp),
                        message: "".to_string(),
                        observed_generation: None,
                        reason: "Accepted".to_string(),
                        status: "True".to_string(),
                        type_: "Accepted".to_string(),
                    }
                } else {
                    k8s::Condition {
                        last_transition_time: k8s::Time(timestamp),
                        message: "".to_string(),
                        observed_generation: None,
                        reason: "NoMatchingParent".to_string(),
                        status: "False".to_string(),
                        type_: "Accepted".to_string(),
                    }
                };
                (parent_reference_id, cond)
            })
            .collect::<Vec<(&ResourceId, k8s::Condition)>>();

        // For a route to have a successful backendRef condition, it means that
        // all backend_references in _all_ of a route's rules **must**
        // successfully resolve.
        // At the moment, this means that all of the backend_references _exist_ (in our cache).
        // https://gateway-api.sigs.k8s.io/geps/gep-1364/#all-conditions-positive
        let mut resolved_all = true;
        for backend in backends.into_iter() {
            // For each route <-> backendRef group binding
            // check if _all_ of the backendRefs exist in the cache
            // a From trait would be good here so we could contains(backend.into())
            let BackendReference::Service(backend_reference_id) = backend;
            if !self.services.contains(backend_reference_id) {
                tracing::info!(?self.services, ?backend_reference_id, "ResolvedAll false");
                resolved_all = false;
                break;
            }
        }

        #[cfg(not(test))]
        let timestamp = Utc::now();
        #[cfg(test)]
        let timestamp = chrono::DateTime::<chrono::Utc>::MIN_UTC;
        let resolved_refs_cond = if resolved_all {
            k8s::Condition {
                last_transition_time: k8s::Time(timestamp),
                type_: "ResolvedRefs".to_string(),
                status: "True".to_string(),
                reason: "ResolvedRefs".to_string(),
                observed_generation: None,
                message: "".to_string(),
            }
        } else {
            k8s::Condition {
                last_transition_time: k8s::Time(timestamp),
                type_: "ResolvedRefs".to_string(),
                status: "False".to_string(),
                reason: "BackendDoesNotExist".to_string(),
                observed_generation: None,
                message: "".to_string(),
            }
        };

        // Each parent status indicates whether the parent is valid. All parents
        // share the same backendRefs since they are part of the route itself.
        // As a result, append condition to all statuses when creating status
        let mut parent_statuses = Vec::new();
        for (p_ref_id, cond) in conditions.into_iter() {
            let status = gateway::RouteParentStatus {
                parent_ref: gateway::ParentReference {
                    group: Some(POLICY_API_GROUP.to_string()),
                    kind: Some("Server".to_string()),
                    namespace: Some(p_ref_id.namespace.clone()),
                    name: p_ref_id.name.clone(),
                    section_name: None,
                    port: None,
                },
                controller_name: STATUS_CONTROLLER_NAME.to_string(),
                conditions: vec![cond, resolved_refs_cond.clone()],
            };
            parent_statuses.push(status);
        }

        let status = gateway::HttpRouteStatus {
            inner: gateway::RouteStatus {
                parents: parent_statuses,
            },
        };
        make_patch(&id.name, status)
    }

    fn apply_server_update(&self) {
        for (id, parents) in self.http_routes.iter() {
            // Bad to unwrap but if a route is in we can hint at there being a
            // route that's been processed. We should be more flexible here and
            // handle this properly.
            let backends = self.route_backends.get(id).unwrap();
            let patch = self.make_http_route_patch(id, parents, backends);
            if let Err(error) = self.updates.send(Update {
                id: id.clone(),
                patch,
            }) {
                tracing::error!(%id.namespace, %id.name, %error, "Failed to send HTTPRoute patch")
            }
        }
    }

    fn apply_service_update(&self) {
        for (id, parents) in self.http_routes.iter() {
            let backends = self.route_backends.get(id).unwrap();
            let patch = self.make_http_route_patch(id, parents, backends);
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
        let parents = match http_route::make_parents(resource.clone()) {
            Ok(parents) => parents,
            Err(error) => {
                tracing::info!(%namespace, %name, %error, "Ignoring HTTPRoute");
                return;
            }
        };

        // Create backendRefs
        let backends = match http_route::make_backends(resource) {
            Ok(backends) => backends,
            Err(error) => {
                tracing::info!(%namespace, %name, %error, "Ignoring HTTPRoute");
                return;
            }
        };

        let should_update_parents = self.update_http_route_parents(id.clone(), parents.clone());
        let should_update_backends = self.update_http_route_backends(id.clone(), backends.clone());
        if !should_update_parents && !should_update_backends {
            return;
        }

        // Create a patch for the HTTPRoute and send it to the controller so
        // that it is applied.
        let patch = self.make_http_route_patch(&id, &parents, &backends);
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
        self.apply_server_update();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);

        self.servers.remove(&id);
        self.apply_server_update();
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
            self.apply_service_update();
        }
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);

        self.services.remove(&id);
        self.apply_service_update();
    }

    // Since apply only reindexes a single Server at a time, there's no need
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
