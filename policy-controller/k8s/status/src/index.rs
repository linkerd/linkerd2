use crate::{
    http_route::{self, BackendReference, ParentReference},
    resource_id::ResourceId,
};
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
#[cfg(not(test))]
use chrono::offset::Utc;
use kubert::lease::Claim;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, ResourceExt};
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::sync::{
    mpsc::{UnboundedReceiver, UnboundedSender},
    watch::Receiver,
};

pub(crate) const POLICY_API_GROUP: &str = "policy.linkerd.io";
const POLICY_API_VERSION: &str = "policy.linkerd.io/v1beta2";
pub const STATUS_CONTROLLER_NAME: &str = "status-controller";

pub type SharedIndex = Arc<RwLock<Index>>;

pub struct Controller {
    client: k8s::Client,
    updates: UnboundedReceiver<Update>,
}

pub struct Index {
    // todo: Will be used to compare against the current claim's claimant to
    // determine if this status controller is the leader
    _name: String,

    // todo: Will be used in the IndexNamespacedResource trait methods to
    // check who the current leader is and if updates should be sent to the
    // controller
    _claims: Receiver<Arc<Claim>>,
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
    pub fn shared(
        name: impl ToString,
        claims: Receiver<Arc<Claim>>,
        updates: UnboundedSender<Update>,
    ) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            _name: name.to_string(),
            _claims: claims,
            updates,
            http_routes: HashMap::new(),
            servers: HashSet::new(),
            services: HashSet::new(),
        }))
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
        }
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
            for backend in backends.into_iter() {
                // For each route <-> backendRef group binding
                // check if _all_ of the backendRefs exist in the cache
                // a From trait would be good here so we could contains(backend.into())
                let BackendReference::Service(backend_reference_id) = backend;
                if !self.services.contains(&backend_reference_id) {
                    tracing::info!(?self.services, ?backend_reference_id, "ResolvedAll false");
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
                let condition = ParentReference::into_status_condition(
                    &parent_reference_id,
                    accepted,
                    timestamp,
                );

                gateway::RouteParentStatus {
                    parent_ref: gateway::ParentReference {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(parent_reference_id.namespace.clone()),
                        name: parent_reference_id.name.clone(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: format!("{}/{}", POLICY_API_GROUP, STATUS_CONTROLLER_NAME),
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

    fn apply_resource_update(&self) {
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

        // Create a patch for the HTTPRoute and send it to the controller so
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
        self.apply_resource_update();
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);

        self.servers.remove(&id);
        self.apply_resource_update();
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
            self.apply_resource_update();
        }
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);

        self.services.remove(&id);
        self.apply_resource_update();
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
