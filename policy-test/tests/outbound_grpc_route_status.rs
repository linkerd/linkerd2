use k8s::Condition;
use k8s_gateway_api::{ParentReference, RouteParentStatus, RouteStatus};
use k8s_openapi::chrono::Utc;
use kube::{Resource, ResourceExt};
use linkerd_policy_controller_core::POLICY_CONTROLLER_NAME;
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{await_condition, create, find_route_condition, with_temp_ns};

fn mk_route(
    ns: &str,
    name: &str,
    parent_refs: Option<Vec<ParentReference>>,
) -> k8s::gateway::GrpcRoute {
    k8s::gateway::GrpcRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::gateway::GrpcRouteSpec {
            inner: k8s::gateway::CommonRouteSpec { parent_refs },
            hostnames: None,
            rules: Some(vec![]),
        },
        status: None,
    }
}

// Waits until a GrpcRoute with the given namespace and name has a status set
// on it, then returns the generic route status representation.
pub async fn await_route_status(client: &kube::Client, ns: &str, name: &str) -> RouteStatus {
    let route_status = await_condition(
        client,
        ns,
        name,
        |obj: Option<&k8s::gateway::GrpcRoute>| -> bool {
            obj.and_then(|route| route.status.as_ref()).is_some()
        },
    )
    .await
    .expect("must fetch route")
    .status
    .expect("route must contain a status representation")
    .inner;

    tracing::trace!(?route_status, name, ns, "got route status");

    route_status
}

#[tokio::test(flavor = "current_thread")]
async fn accepted_parent() {
    with_temp_ns(|client, ns| async move {
        // Create a parent Service
        let svc_name = "test-service";
        let svc = k8s::Service {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.clone()),
                name: Some(svc_name.to_string()),
                ..Default::default()
            },
            spec: Some(k8s::ServiceSpec {
                type_: Some("ClusterIP".to_string()),
                ports: Some(vec![k8s::ServicePort {
                    port: 80,
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..k8s::Service::default()
        };
        let svc = create(&client, svc).await;
        let svc_ref = vec![ParentReference {
            group: Some("core".to_string()),
            kind: Some("Service".to_string()),
            namespace: svc.namespace(),
            name: svc.name_unchecked(),
            section_name: None,
            port: Some(80),
        }];

        // Create a route that references the Service resource.
        let _route = create(&client, mk_route(&ns, "test-route", Some(svc_ref))).await;
        // Wait until route is updated with a status
        let statuses = await_route_status(&client, &ns, "test-route").await.parents;

        let route_status = statuses
            .clone()
            .into_iter()
            .find(|route_status| route_status.parent_ref.name == svc_name)
            .expect("must have at least one parent status");

        // Check status references to parent we have created
        assert_eq!(route_status.parent_ref.group.as_deref(), Some("core"));
        assert_eq!(route_status.parent_ref.kind.as_deref(), Some("Service"));

        // Check status is accepted with a status of 'True'
        let cond = find_route_condition(&statuses, svc_name)
            .expect("must have at least one 'Accepted' condition for accepted service");
        assert_eq!(cond.status, "True");
        assert_eq!(cond.reason, "Accepted")
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn no_cluster_ip() {
    with_temp_ns(|client, ns| async move {
        // Create a parent Service
        let svc = k8s::Service {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.clone()),
                name: Some("test-service".to_string()),
                ..Default::default()
            },
            spec: Some(k8s::ServiceSpec {
                cluster_ip: Some("None".to_string()),
                type_: Some("ClusterIP".to_string()),
                ports: Some(vec![k8s::ServicePort {
                    port: 80,
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..k8s::Service::default()
        };
        let svc = create(&client, svc).await;
        let svc_ref = vec![ParentReference {
            group: Some("core".to_string()),
            kind: Some("Service".to_string()),
            namespace: svc.namespace(),
            name: svc.name_unchecked(),
            section_name: None,
            port: Some(80),
        }];

        // Create a route that references the Service resource.
        let _route = create(&client, mk_route(&ns, "test-route", Some(svc_ref))).await;
        // Wait until route is updated with a status
        let status = await_route_status(&client, &ns, "test-route").await;
        let cond = find_route_condition(&status.parents, "test-service")
            .expect("must have at least one 'Accepted' condition set for parent");
        // Parent with no ClusterIP should not match.
        assert_eq!(cond.status, "False");
        assert_eq!(cond.reason, "NoMatchingParent");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn external_name() {
    with_temp_ns(|client, ns| async move {
        // Create a parent Service
        let svc = k8s::Service {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.clone()),
                name: Some("test-service".to_string()),
                ..Default::default()
            },
            spec: Some(k8s::ServiceSpec {
                type_: Some("ExternalName".to_string()),
                external_name: Some("linkerd.io".to_string()),
                ports: Some(vec![k8s::ServicePort {
                    port: 80,
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..k8s::Service::default()
        };
        let svc = create(&client, svc).await;
        let svc_ref = vec![ParentReference {
            group: Some("core".to_string()),
            kind: Some("Service".to_string()),
            namespace: svc.namespace(),
            name: svc.name_unchecked(),
            section_name: None,
            port: Some(80),
        }];

        // Create a route that references the Service resource.
        let _route = create(&client, mk_route(&ns, "test-route", Some(svc_ref))).await;
        // Wait until route is updated with a status
        let status = await_route_status(&client, &ns, "test-route").await;
        let cond = find_route_condition(&status.parents, "test-service")
            .expect("must have at least one 'Accepted' condition set for parent");
        // Parent with ExternalName should not match.
        assert_eq!(cond.status, "False");
        assert_eq!(cond.reason, "NoMatchingParent");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn multiple_statuses() {
    with_temp_ns(|client, ns| async move {
        // Create a parent Service
        let svc_name = "test-service";
        let svc = k8s::Service {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.clone()),
                name: Some(svc_name.to_string()),
                ..Default::default()
            },
            spec: Some(k8s::ServiceSpec {
                type_: Some("ClusterIP".to_string()),
                ports: Some(vec![k8s::ServicePort {
                    port: 80,
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..k8s::Service::default()
        };
        let svc = create(&client, svc).await;
        let svc_ref = vec![ParentReference {
            group: Some("core".to_string()),
            kind: Some("Service".to_string()),
            namespace: svc.namespace(),
            name: svc.name_unchecked(),
            section_name: None,
            port: Some(80),
        }];

        // Create a route that references the Service resource.
        let _route = create(&client, mk_route(&ns, "test-route", Some(svc_ref))).await;

        // Patch a status onto the GrpcRoute.
        let value = serde_json::json!({
            "apiVersion": k8s::gateway::GrpcRoute::api_version(&()).as_ref(),
                "kind": k8s::gateway::GrpcRoute::kind(&()).as_ref(),
                "name": "test-route",
                "status": k8s::gateway::GrpcRouteStatus {
                    inner: RouteStatus {
                        parents: vec![RouteParentStatus {
                            conditions: vec![Condition {
                                last_transition_time: k8s::Time(Utc::now()),
                                message: "".to_string(),
                                observed_generation: None,
                                reason: "Accepted".to_string(),
                                status: "True".to_string(),
                                type_: "Accepted".to_string(),
                            }],
                            controller_name: "someone/else".to_string(),
                            parent_ref: ParentReference {
                                group: Some("gateway.networking.k8s.io".to_string()),
                                name: "foo".to_string(),
                                kind: Some("Gateway".to_string()),
                                namespace: Some("bar".to_string()),
                                port: None,
                                section_name: None,
                            },
                        }],
                    },
                },
        });
        let patch = k8s::Patch::Merge(value);
        let patch_params = k8s::PatchParams::apply("someone/else");
        let api = k8s::Api::<k8s::gateway::GrpcRoute>::namespaced(client.clone(), &ns);
        api.patch_status("test-route", &patch_params, &patch)
            .await
            .expect("failed to patch status");

        await_condition(
            &client,
            &ns,
            "test-route",
            |obj: Option<&k8s::gateway::GrpcRoute>| -> bool {
                obj.and_then(|route| route.status.as_ref())
                    .map(|status| {
                        let statuses = &status.inner.parents;

                        let other_status_found = statuses
                            .iter()
                            .any(|route_status| route_status.controller_name == "someone/else");

                        let linkerd_status_found = statuses.iter().any(|route_status| {
                            route_status.controller_name == POLICY_CONTROLLER_NAME
                        });

                        other_status_found && linkerd_status_found
                    })
                    .unwrap_or(false)
            },
        )
        .await
        .expect("must have both statuses");
    })
    .await;
}
