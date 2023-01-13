use ahash::AHashMap as HashMap;
use chrono::offset::Utc;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, ResourceExt};
use tokio::sync::mpsc::UnboundedReceiver;

pub struct Update {
    namespace: String,
    accepted_routes: HashMap<String, Vec<String>>,
}

pub struct Controller {
    updates: UnboundedReceiver<Update>,
    client: k8s::Client,
}

impl Update {
    pub fn new(namespace: String, accepted_routes: HashMap<String, Vec<String>>) -> Self {
        Self {
            namespace,
            accepted_routes,
        }
    }
}

impl Controller {
    pub fn new(updates: UnboundedReceiver<Update>, client: k8s::Client) -> Self {
        Self { updates, client }
    }

    pub async fn process_updates(mut self) {
        let patch_params = k8s::PatchParams::apply("policy.linkerd.io");

        while let Some(Update {
            namespace,
            accepted_routes,
        }) = self.updates.recv().await
        {
            let api =
                k8s::Api::<k8s::policy::HttpRoute>::namespaced(self.client.clone(), &namespace);
            let routes = match api.list(&k8s::ListParams::default()).await {
                Ok(routes) => routes,
                Err(error) => {
                    // todo: We log error and skip this update. This leaves us
                    // stuck with the statuses in the previous state until
                    // another update happens.  Instead, we should requeue the
                    // update so that we can try again.
                    tracing::error!(%error, "failed to list HTTPRoutes");
                    continue;
                }
            };
            for route in routes {
                let name = route.name_unchecked();
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
                        let accepted = accepted_routes
                            .get(&name)
                            .into_iter()
                            .flatten()
                            .any(|accepting_parent| accepting_parent == &parent.name);
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
                                group: Some("policy.linkerd.io".to_string()),
                                kind: Some("Server".to_string()),
                                namespace: Some(namespace.clone()),
                                name: parent.name.clone(),
                                section_name: None,
                                port: None,
                            },
                            controller_name: "policy.linkerd.io/status-controller".to_string(),
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
                    "apiVersion": "policy.linkerd.io/v1alpha1",
                    "kind": "HTTPRoute",
                    "name": name,
                    "status": status,
                });
                if let Err(error) = api
                    .patch_status(&name, &patch_params, &k8s::Patch::Merge(patch))
                    .await
                {
                    tracing::error!(%error, "Failed to patch HTTPRoute");
                }
            }
        }
    }
}
