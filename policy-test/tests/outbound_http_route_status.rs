use kube::ResourceExt;
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{
    await_route_status, create, find_route_condition, mk_route, with_temp_ns,
};

#[tokio::test(flavor = "current_thread")]
async fn accepted_parent() {
    with_temp_ns(|client, ns| async move {
        // Create a parent Service
        let svc = k8s::Service {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.clone()),
                name: Some("test-service".to_string()),
                ..Default::default()
            },
            spec: Some(k8s::ServiceSpec {
                cluster_ip: Some("10.96.1.1".to_string()),
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
        let svc_ref = vec![k8s::policy::httproute::ParentReference {
            group: Some("core".to_string()),
            kind: Some("Service".to_string()),
            namespace: svc.namespace(),
            name: svc.name_unchecked(),
            section_name: None,
            port: None,
        }];

        // Create a route that references the Service resource.
        let _route = create(&client, mk_route(&ns, "test-route", Some(svc_ref))).await;
        // Wait until route is updated with a status
        let statuses = await_route_status(&client, &ns, "test-route").await.parents;

        let route_status = statuses
            .clone()
            .into_iter()
            .find(|route_status| route_status.parent_ref.name == svc.name_unchecked())
            .expect("must have at least one parent status");

        // Check status references to parent we have created
        assert_eq!(route_status.parent_ref.group.as_deref(), Some("core"));
        assert_eq!(route_status.parent_ref.kind.as_deref(), Some("Service"));

        // Check status is accepted with a status of 'True'
        let cond = find_route_condition(statuses, &svc.name_unchecked())
            .expect("must have at least one 'Accepted' condition for accepted servuce");
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
                cluster_ip: None,
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
        let svc_ref = vec![k8s::policy::httproute::ParentReference {
            group: Some("core".to_string()),
            kind: Some("Service".to_string()),
            namespace: svc.namespace(),
            name: svc.name_unchecked(),
            section_name: None,
            port: None,
        }];

        // Create a route that references the Service resource.
        let _route = create(&client, mk_route(&ns, "test-route", Some(svc_ref))).await;
        // Wait until route is updated with a status
        let cond = find_route_condition(
            await_route_status(&client, &ns, "test-route").await.parents,
            "test-server",
        )
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
                cluster_ip: None,
                type_: Some("ExternalName".to_string()),
                ports: Some(vec![k8s::ServicePort {
                    port: 80,
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..k8s::Service::default()
        };
        let svc = create(&client, svc).await;
        let svc_ref = vec![k8s::policy::httproute::ParentReference {
            group: Some("core".to_string()),
            kind: Some("Service".to_string()),
            namespace: svc.namespace(),
            name: svc.name_unchecked(),
            section_name: None,
            port: None,
        }];

        // Create a route that references the Service resource.
        let _route = create(&client, mk_route(&ns, "test-route", Some(svc_ref))).await;
        // Wait until route is updated with a status
        let cond = find_route_condition(
            await_route_status(&client, &ns, "test-route").await.parents,
            "test-server",
        )
        .expect("must have at least one 'Accepted' condition set for parent");
        // Parent with ExternalName should not match.
        assert_eq!(cond.status, "False");
        assert_eq!(cond.reason, "NoMatchingParent");
    })
    .await;
}
