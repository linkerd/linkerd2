use ahash::AHashMap as HashMap;
use chrono::offset::Utc;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway, ResourceExt};
use tokio::sync::mpsc::UnboundedReceiver;

// todo
//   - impl fn new to remove pub fields
//   - is debug derive needed?
#[derive(Debug)]
pub struct Update {
    pub namespace: String,
    pub accepted_routes: HashMap<String, Vec<String>>,
}

// todo: impl fn new to remove pub fields
pub struct Controller {
    pub updates: UnboundedReceiver<Update>,
    pub client: k8s::Client,
}

impl Controller {
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
                    // TODO: We log error and skip this update. This leaves us
                    // stuck with the statuses in the previous state until
                    // another update happens.  Instead, we should requeue the
                    // update so that we can try again.
                    tracing::error!(%error, "failed to list HTTPRoutes");
                    continue;
                }
            };
            for route in routes {
                let name = route.name_unchecked();

                // let current_servers: Vec<&String> = route
                //     .status
                //     .iter()
                //     .flat_map(|s| s.inner.parents.iter())
                //     .filter(|parent| {
                //         parent
                //             .conditions
                //             .iter()
                //             .any(|c| c.type_ == "Accepted" && c.status == "True")
                //     })
                //     .map(|parent| &parent.parent_ref.name)
                //     .collect();

                let mut parent_statuses: Vec<k8s::gateway::RouteParentStatus> = vec![];

                let parent_refs = route
                    .spec
                    .inner
                    .parent_refs
                    .iter()
                    .flatten()
                    .filter(|parent| parent.kind.as_deref() == Some("Server"))
                    .map(|parent| &parent.name)
                    .collect::<Vec<&String>>();
                tracing::info!(?parent_refs);

                match accepted_routes.get(&name) {
                    Some(accepting_parents) => {
                        for server in accepting_parents {
                            let parent = gateway::RouteParentStatus {
                                parent_ref: gateway::ParentReference {
                                    group: Some("policy.linkerd.io".to_string()),
                                    kind: Some("Server".to_string()),
                                    namespace: Some(namespace.clone()),
                                    name: server.to_string(),
                                    section_name: None,
                                    port: None,
                                },
                                controller_name: "policy.linkerd.io/status-controller".to_string(),
                                conditions: vec![k8s::Condition {
                                    last_transition_time: k8s::Time(Utc::now()),
                                    message: "".to_string(),
                                    observed_generation: None,
                                    reason: "Accepted".to_string(),
                                    status: "True".to_string(), // accepted is true
                                    type_: "Accepted".to_string(),
                                }],
                            };
                            parent_statuses.push(parent);
                        }

                        let unaccepting_parents = parent_refs
                            .iter()
                            .filter(|parent| !accepting_parents.contains(parent));
                        for server in unaccepting_parents {
                            let parent = gateway::RouteParentStatus {
                                parent_ref: gateway::ParentReference {
                                    group: Some("policy.linkerd.io".to_string()),
                                    kind: Some("Server".to_string()),
                                    namespace: Some(namespace.clone()),
                                    name: server.to_string(),
                                    section_name: None,
                                    port: None,
                                },
                                controller_name: "policy.linkerd.io/status-controller".to_string(),
                                conditions: vec![k8s::Condition {
                                    last_transition_time: k8s::Time(Utc::now()),
                                    message: "".to_string(),
                                    observed_generation: None,
                                    reason: "NoMatchingParent".to_string(),
                                    status: "False".to_string(), // accepted is false
                                    type_: "Accepted".to_string(),
                                }],
                            };
                            parent_statuses.push(parent);
                        }
                    }
                    None => {
                        for server in parent_refs {
                            let parent = gateway::RouteParentStatus {
                                parent_ref: gateway::ParentReference {
                                    group: Some("policy.linkerd.io".to_string()),
                                    kind: Some("Server".to_string()),
                                    namespace: Some(namespace.clone()),
                                    name: server.to_string(),
                                    section_name: None,
                                    port: None,
                                },
                                controller_name: "policy.linkerd.io/status-controller".to_string(),
                                conditions: vec![k8s::Condition {
                                    last_transition_time: k8s::Time(Utc::now()),
                                    message: "".to_string(),
                                    observed_generation: None,
                                    reason: "NoMatchingParent".to_string(),
                                    status: "False".to_string(), // accepted is false
                                    type_: "Accepted".to_string(),
                                }],
                            };
                            parent_statuses.push(parent);
                        }
                    }
                }

                // let parent_statuses2: Vec<k8s::gateway::RouteParentStatus> =
                //     match accepted_routes.get(&name) {
                //         Some(servers) => servers
                //             .iter()
                //             .map(|server| gateway::RouteParentStatus {
                //                 parent_ref: gateway::ParentReference {
                //                     group: Some("policy.linkerd.io".to_string()),
                //                     kind: Some("Server".to_string()),
                //                     namespace: Some(namespace.clone()),
                //                     name: server.to_string(),
                //                     section_name: None,
                //                     port: None,
                //                 },
                //                 controller_name: "policy.linkerd.io/status-controller".to_string(),
                //                 conditions: vec![k8s::Condition {
                //                     last_transition_time: k8s::Time(Utc::now()),
                //                     message: "".to_string(),
                //                     observed_generation: None,
                //                     reason: "".to_string(),
                //                     status: "True".to_string(),
                //                     type_: "Accepted".to_string(),
                //                 }],
                //             })
                //             .collect(),
                //         None => vec![],
                //     };

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
                    tracing::error!(%error, "failed to patch HTTPRoute");
                }
            }
        }
    }
}
