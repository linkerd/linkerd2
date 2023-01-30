use crate::http_route::RouteBinding;
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use chrono::offset::Utc;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, ResourceExt};
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, sync::Arc};
use tokio::sync::mpsc::{UnboundedReceiver, UnboundedSender};

const POLICY_API_GROUP: &str = "policy.linkerd.io";
const POLICY_API_VERSION: &str = "policy.linkerd.io/v1alpha1";
const STATUS_CONTROLLER_NAME: &str = "policy.linkerd.io/status-controller";

pub type SharedIndex = Arc<RwLock<Index>>;

pub struct Controller {
    client: k8s::Client,
    index: SharedIndex,
    updates: UnboundedReceiver<Update>,
}

pub struct Index {
    updates: UnboundedSender<Update>,

    http_routes: HashMap<ResourceId, RouteBinding>,
    servers: HashSet<ResourceId>,
}

pub enum Update {
    HttpRoute(ResourceId),
    Server,
}

#[derive(Clone, Eq, Hash, PartialEq)]
pub struct ResourceId {
    namespace: String,
    name: String,
}

impl Controller {
    pub fn new(
        client: k8s::Client,
        index: SharedIndex,
        updates: UnboundedReceiver<Update>,
    ) -> Self {
        Self {
            client,
            index,
            updates,
        }
    }

    pub async fn process_updates(mut self) {
        let patch_params = k8s::PatchParams::apply("policy.linkerd.io");

        while let Some(update) = self.updates.recv().await {
            match update {
                Update::HttpRoute(route) => {
                    self.process_http_route_update(route, patch_params.clone())
                        .await;
                }
                Update::Server => {
                    self.process_server_update(patch_params.clone()).await;
                }
            }
        }
    }

    async fn process_http_route_update(
        &mut self,
        route_id: ResourceId,
        patch_params: k8s::PatchParams,
    ) {
        let route_binding = match self.index.read().http_routes.get(&route_id) {
            Some(route) => route.clone(),
            None => {
                tracing::info!(%route_id.namespace, %route_id.name, "Failed to find HTTPRoute in index");
                return;
            }
        };
        let accepting_servers: Vec<ResourceId> = self
            .index
            .read()
            .servers
            .iter()
            // todo: we should allow cross-namespace references; confirm this
            // would allow that
            .filter(|server| route_binding.selects_server(&server.name))
            .cloned()
            .collect();

        // Create the namespace API client and patch the HTTPRoute.
        let api = k8s::Api::<k8s::policy::HttpRoute>::namespaced(
            self.client.clone(),
            &route_id.namespace,
        );
        let route = match api.get(&route_id.name).await {
            Ok(route) => route,
            Err(error) => {
                tracing::info!(%route_id.namespace, %route_id.name, %error, "Failed to find HTTPRoute");
                return;
            }
        };
        let parent_statuses = route
            .spec
            .inner
            .parent_refs
            .iter()
            .flatten()
            .filter(|parent| parent.kind.as_deref() == Some("Server"))
            .map(|parent| {
                // Is this parent in the list of parents which accept
                // the route?
                let accepted = accepting_servers
                    .iter()
                    .any(|accepting_parent| accepting_parent.name == parent.name);
                let condition = if accepted {
                    k8s::Condition {
                        last_transition_time: k8s::Time(Utc::now()),
                        message: "".to_string(),
                        observed_generation: None,
                        reason: "Accepted".to_string(),
                        status: "True".to_string(),
                        type_: "Accepted".to_string(),
                    }
                } else {
                    k8s::Condition {
                        last_transition_time: k8s::Time(Utc::now()),
                        message: "".to_string(),
                        observed_generation: None,
                        reason: "NoMatchingParent".to_string(),
                        status: "False".to_string(),
                        type_: "Accepted".to_string(),
                    }
                };
                gateway::RouteParentStatus {
                    parent_ref: gateway::ParentReference {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(route_id.namespace.clone()),
                        name: parent.name.clone(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: STATUS_CONTROLLER_NAME.to_string(),
                    conditions: vec![condition],
                }
            })
            .collect();
        let status = gateway::HttpRouteStatus {
            inner: gateway::RouteStatus {
                parents: parent_statuses,
            },
        };
        let patch = serde_json::json!({
            "apiVersion": POLICY_API_VERSION,
            "kind": "HTTPRoute",
            "name": route_id.name,
            "status": status,
        });
        if let Err(error) = api
            .patch_status(&route_id.name, &patch_params, &k8s::Patch::Merge(patch))
            .await
        {
            tracing::error!(namespace = %route_id.namespace, name = %route_id.name, %error, "Failed to patch HTTPRoute");
        }
    }

    async fn process_server_update(&mut self, patch_params: k8s::PatchParams) {
        let route_ids: Vec<ResourceId> = self.index.read().http_routes.keys().cloned().collect();
        for route_id in route_ids {
            self.process_http_route_update(route_id, patch_params.clone())
                .await;
        }
    }
}

impl Index {
    pub fn shared(updates: UnboundedSender<Update>) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            updates,
            http_routes: HashMap::new(),
            servers: HashSet::new(),
        }))
    }
}

impl kubert::index::IndexNamespacedResource<k8s::policy::HttpRoute> for Index {
    fn apply(&mut self, resource: k8s::policy::HttpRoute) {
        let namespace = resource
            .namespace()
            .expect("HTTPRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = ResourceId::new(namespace.clone(), name.clone());

        let route_binding = match RouteBinding::try_from(resource) {
            Ok(binding) => binding,
            Err(error) => {
                tracing::info!(%namespace, %name, %error, "Ignoring HTTPRoute");
                return;
            }
        };

        // todo: turn into var since we may not always need to update the
        // status
        // todo: remove `route_binding.clone()`s
        match self.http_routes.entry(id.clone()) {
            Entry::Vacant(entry) => {
                entry.insert(route_binding);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() != route_binding {
                    entry.insert(route_binding);
                }
            }
        };

        if let Err(error) = self.updates.send(Update::HttpRoute(id.clone())) {
            tracing::error!(%id.namespace, %id.name, %error, "Failed to send HTTPRoute update")
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
        let namespace = resource
            .namespace()
            .expect("HTTPRoute must have a namespace");
        let name = resource.name_unchecked();
        let id = ResourceId::new(namespace, name);

        self.servers.insert(id.clone());

        if let Err(error) = self.updates.send(Update::Server) {
            tracing::error!(%id.namespace, %id.name, %error, "Failed to send Server apply update")
        }
    }

    fn delete(&mut self, namespace: String, name: String) {
        let id = ResourceId::new(namespace, name);
        self.servers.remove(&id);

        if let Err(error) = self.updates.send(Update::Server) {
            tracing::error!(%id.namespace, %id.name, %error, "Failed to send Server delete update")
        }
    }

    fn reset(
        &mut self,
        _resources: Vec<k8s::policy::Server>,
        _removed: kubert::index::NamespacedRemoved,
    ) {
        // todo: make sure server is in index; update status for all routes
        // since routes in any namespace could reference this server
        // todo: can we skip the impl here similar to HTTPRoutes?
    }
}

impl ResourceId {
    fn new(namespace: String, name: String) -> Self {
        Self { namespace, name }
    }
}
