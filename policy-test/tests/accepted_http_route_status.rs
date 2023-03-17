use kube::ResourceExt;
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{await_route_status, create, with_temp_ns};

#[tokio::test(flavor = "current_thread")]
async fn inbound_accepted_parent() {
    with_temp_ns(|client, ns| async move {
        // Create a test 'Server'
        let server = k8s::policy::Server {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("test-accepted-server".to_string()),
                ..Default::default()
            },
            spec: k8s::policy::ServerSpec {
                pod_selector: k8s::labels::Selector::from_iter(Some((
                    "app",
                    "test-accepted-server",
                ))),
                port: k8s::policy::server::Port::Name("http".to_string()),
                proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
            },
        };
        let server = create(&client, server).await;
        let srv_ref = vec![k8s::policy::httproute::ParentReference {
            group: Some("policy.linkerd.io".to_string()),
            kind: Some("Server".to_string()),
            namespace: server.namespace(),
            name: server.name_unchecked(),
            section_name: None,
            port: None,
        }];

        // Create a route that references the Server resource.
        let _route = create(
            &client,
            mk_inbound_route(&ns, "test-accepted-route", Some(srv_ref)),
        )
        .await;
        // Wait until route is updated with a status
        let route_status = await_route_status(&client, &ns, "test-accepted-route")
            .await
            .parents
            .get(0)
            .expect("must have at least one parent status")
            .clone();

        // Check status references to parent we have created
        assert_eq!(
            route_status.parent_ref.group.as_deref(),
            Some("policy.linkerd.io")
        );
        assert_eq!(route_status.parent_ref.kind.as_deref(), Some("Server"));
        assert_eq!(route_status.parent_ref.name, server.name_unchecked());

        // Check status is accepted with a status of 'True'
        let cond = route_status
            .conditions
            .iter()
            .find(|cond| cond.type_ == "Accepted")
            .expect("must have at least one 'Accepted' condition");
        assert_eq!(cond.status, "True");
        assert_eq!(cond.reason, "Accepted")
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn inbound_multiple_parents() {
    with_temp_ns(|client, ns| async move {
        // Exercise accepted test with a valid, and an invalid parent reference
        let srv_refs = vec![
            k8s::policy::httproute::ParentReference {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("Server".to_string()),
                namespace: Some(ns.clone()),
                name: "test-valid-server".to_string(),
                section_name: None,
                port: None,
            },
            k8s::policy::httproute::ParentReference {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("Server".to_string()),
                namespace: Some(ns.clone()),
                name: "test-invalid-server".to_string(),
                section_name: None,
                port: None,
            },
        ];

        // Create only one of the parents
        let server = k8s::policy::Server {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some("test-valid-server".to_string()),
                ..Default::default()
            },
            spec: k8s::policy::ServerSpec {
                pod_selector: k8s::labels::Selector::from_iter(Some(("app", "test-valid-server"))),
                port: k8s::policy::server::Port::Name("http".to_string()),
                proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
            },
        };
        let _server = create(&client, server).await;

        // Create a route that references both parents.
        let _route = create(
            &client,
            mk_inbound_route(&ns, "test-multiple-parents-route", Some(srv_refs)),
        )
        .await;
        // Wait until route is updated with a status
        let parent_status = await_route_status(&client, &ns, "test-multiple-parents-route")
            .await
            .parents;

        // Find status for invalid parent and extract the condition
        let invalid_cond = parent_status
            .iter()
            .find(|route_status| route_status.parent_ref.name == "test-invalid-server")
            .expect("must have a route status set for invalid server")
            .conditions
            .clone()
            .into_iter()
            .find(|cond| cond.type_ == "Accepted")
            .expect("must have at least one 'Accepted' condition set for invalid parent");
        // Route shouldn't be accepted
        assert_eq!(invalid_cond.status, "False");
        assert_eq!(invalid_cond.reason, "NoMatchingParent");

        // Find status for valid parent and extract the condition
        let valid_cond = parent_status
            .iter()
            .find(|route_status| route_status.parent_ref.name == "test-valid-server")
            .expect("must have a route status set for valid server")
            .conditions
            .clone()
            .into_iter()
            .find(|cond| cond.type_ == "Accepted")
            .expect("must have at least one 'Accepted' condition set for valid parent");
        assert_eq!(valid_cond.status, "True");
        assert_eq!(valid_cond.reason, "Accepted")
    })
    .await
}

#[tokio::test(flavor = "current_thread")]
async fn inbound_no_parent_ref_patch() {
    with_temp_ns(|client, ns| async move {
        // A route may not include any parent references. When that's the case,
        // we expect the controller to simply ignore it.
        let _route = create(
            &client,
            mk_inbound_route(&ns, "test-no-parent-refs-route", None),
        )
        .await;

        // Status may not be set straight away. To account for that, wrap a
        // status condition watcher in a timeout.
        let api = kube::Api::namespaced(client.clone(), &ns);
        let resolved = tokio::time::timeout(
            // 5 seconds should be enough for a route to be patched
            tokio::time::Duration::from_secs(5),
            kube::runtime::wait::await_condition(
                api,
                "test-no-parent-refs-route",
                |obj: Option<&k8s::policy::HttpRoute>| -> bool {
                    obj.and_then(|route| route.status.as_ref()).is_some()
                },
            ),
        )
        .await;

        // If timeout has elapsed, then route did not receive a status patch
        assert!(
            resolved.is_err(),
            "status condition should timeout when no parent refs are specified"
        );
    })
    .await
}

#[tokio::test(flavor = "current_thread")]
async fn inbound_accepted_no_parent() {
    with_temp_ns(|client, ns| async move {
        // Create a reference to a Server that doesn't exist.
        let srv_ref = vec![k8s::policy::httproute::ParentReference {
            group: Some("policy.linkerd.io".to_string()),
            kind: Some("Server".to_string()),
            namespace: Some(ns.clone()),
            name: "test-inexistent-server".to_string(),
            section_name: None,
            port: None,
        }];

        // Create the route
        let _route = create(
            &client,
            mk_inbound_route(&ns, "test-no-parent-route", Some(srv_ref)),
        )
        .await;
        let route_status = await_route_status(&client, &ns, "test-no-parent-route")
            .await
            .parents
            .get(0)
            .expect("must have at least one parent status")
            .clone();
        let cond = route_status
            .conditions
            .iter()
            .find(|cond| cond.type_ == "Accepted")
            .expect("must have at least one 'Accepted' condition");

        // Route shouldn't be accepted
        assert_eq!(cond.status, "False");
        assert_eq!(cond.reason, "NoMatchingParent");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn inbound_accepted_reconcile_create_event() {
    with_temp_ns(|client, ns| async move {
        // Given a route with inexistent parentReference, we expect to have an
        // 'Accepted' condition with 'False' as a status.
        let srv_ref = vec![k8s::policy::httproute::ParentReference {
            group: Some("policy.linkerd.io".to_string()),
            kind: Some("Server".to_string()),
            namespace: Some(ns.clone()),
            name: "test-reconcile-create-server".to_string(),
            section_name: None,
            port: None,
        }];
        let _route = create(
            &client,
            mk_inbound_route(&ns, "test-reconcile-create-route", Some(srv_ref)),
        )
        .await;
        let cond = await_route_status(&client, &ns, "test-reconcile-create-route")
            .await
            .parents
            .get(0)
            .expect("must have at least one parent status")
            .conditions
            .iter()
            .find(|cond| cond.type_ == "Accepted")
            .expect("must have at least one 'Accepted' condition")
            .clone();
        assert_eq!(cond.status, "False");
        assert_eq!(cond.reason, "NoMatchingParent");

        // Save create_timestamp to assert status has been updated.
        let create_timestamp = &cond.last_transition_time;

        // Create the 'Server' that route references and expect it to be picked up
        // by the index. Consequently, route will have its status reconciled.
        let _server = {
            let server = k8s::policy::Server {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.to_string()),
                    name: Some("test-reconcile-create-server".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::ServerSpec {
                    pod_selector: k8s::labels::Selector::from_iter(Some((
                        "app",
                        "test-reconcile-create-server",
                    ))),
                    port: k8s::policy::server::Port::Name("http".to_string()),
                    proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
                },
            };
            create(&client, server).await
        };

        // HTTPRoute may not be patched instantly, wrap with a timeout and loop
        // until create_timestamp and observed_timestamp are different.
        let cond = tokio::time::timeout(tokio::time::Duration::from_secs(60), async move {
            loop {
                let cond = await_route_status(&client, &ns, "test-reconcile-create-route")
                    .await
                    .parents
                    .get(0)
                    .expect("must have at least one parent status")
                    .conditions
                    .iter()
                    .find(|cond| cond.type_ == "Accepted")
                    .expect("must have at least one 'Accepted' condition")
                    .clone();

                // Observe condition's current timestamp. If it differs from the
                // previously recorded timestamp, then it means the underlying
                // condition has been updated so we can check the message and
                // status.
                if &cond.last_transition_time > create_timestamp {
                    return cond;
                }
            }
        })
        .await
        .expect("Timed-out waiting for HTTPRoute status update");
        assert_eq!(cond.status, "True");
        assert_eq!(cond.reason, "Accepted");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn inbound_accepted_reconcile_delete_event() {
    with_temp_ns(|client, ns| async move {
        // Attach a route to a Server and expect the route to be patched with an
        // Accepted status.
        let _server = {
            let server = k8s::policy::Server {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.to_string()),
                    name: Some("test-reconcile-delete-server".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::ServerSpec {
                    pod_selector: k8s::labels::Selector::from_iter(Some((
                        "app",
                        "test-reconcile-delete-server",
                    ))),
                    port: k8s::policy::server::Port::Name("http".to_string()),
                    proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
                },
            };
            create(&client, server).await
        };
        // Create parentReference and route
        let srv_ref = vec![k8s::policy::httproute::ParentReference {
            group: Some("policy.linkerd.io".to_string()),
            kind: Some("Server".to_string()),
            namespace: Some(ns.clone()),
            name: "test-reconcile-delete-server".to_string(),
            section_name: None,
            port: None,
        }];
        let _route = create(
            &client,
            mk_inbound_route(&ns, "test-reconcile-delete-route", Some(srv_ref)),
        )
        .await;
        let cond = await_route_status(&client, &ns, "test-reconcile-delete-route")
            .await
            .parents
            .get(0)
            .expect("must have at least one parent status")
            .conditions
            .iter()
            .find(|cond| cond.type_ == "Accepted")
            .expect("must have at least one 'Accepted' condition")
            .clone();
        assert_eq!(cond.status, "True");
        assert_eq!(cond.reason, "Accepted");

        // Save create_timestamp to assert status has been updated.
        let create_timestamp = &cond.last_transition_time;

        // Delete Server
        let api: kube::Api<k8s::policy::Server> = kube::Api::namespaced(client.clone(), &ns);
        api.delete(
            "test-reconcile-delete-server",
            &kube::api::DeleteParams::default(),
        )
        .await
        .expect("API delete request failed");

        // HTTPRoute may not be patched instantly, wrap with a timeout and loop
        // until create_timestamp and observed_timestamp are different.
        let cond = tokio::time::timeout(tokio::time::Duration::from_secs(60), async move {
            loop {
                let cond = await_route_status(&client, &ns, "test-reconcile-delete-route")
                    .await
                    .parents
                    .get(0)
                    .expect("must have at least one parent status")
                    .conditions
                    .iter()
                    .find(|cond| cond.type_ == "Accepted")
                    .expect("must have at least one 'Accepted' condition")
                    .clone();

                // Observe condition's current timestamp. If it differs from the
                // previously recorded timestamp, then it means the underlying
                // condition has been updated so we can check the message and
                // status.
                if &cond.last_transition_time > create_timestamp {
                    return cond;
                }
            }
        })
        .await
        .expect("Timed-out waiting for HTTPRoute status update");
        assert_eq!(cond.status, "False");
        assert_eq!(cond.reason, "NoMatchingParent");
    })
    .await;
}

fn mk_inbound_route(
    ns: &str,
    name: &str,
    parent_refs: Option<Vec<k8s::policy::httproute::ParentReference>>,
) -> k8s::policy::HttpRoute {
    use k8s::policy::httproute as api;
    api::HttpRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: api::HttpRouteSpec {
            inner: api::CommonRouteSpec { parent_refs },
            hostnames: None,
            rules: Some(vec![]),
        },
        status: None,
    }
}
