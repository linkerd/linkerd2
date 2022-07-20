use kube::ResourceExt;
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{LocalTargetRef, NamespacedTargetRef},
};
use linkerd_policy_test::{create, create_ready_pod, curl, nginx, with_temp_ns, LinkerdInject};
use std::num::NonZeroU16;

#[tokio::test(flavor = "current_thread")]
async fn meshtls() {
    with_temp_ns(|client, ns| async move {
        // First create all of the policies we'll need so that the nginx pod
        // starts up with the correct policy (to prevent races).
        //
        // The policy requires that all connections are authenticated with MeshTLS.
        let (srv, all_mtls) = tokio::join!(
            create(&client, nginx::server(&ns)),
            create(&client, all_authenticated(&ns))
        );
        create(
            &client,
            authz_policy(
                &ns,
                "nginx",
                LocalTargetRef::from_resource(&srv),
                Some(NamespacedTargetRef::from_resource(&all_mtls)),
            ),
        )
        .await;

        // Create the nginx pod and wait for it to be ready.
        tokio::join!(
            create(&client, nginx::service(&ns)),
            create_ready_pod(&client, nginx::pod(&ns))
        );

        let curl = curl::Runner::init(&client, &ns).await;
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-injected", "http://nginx", LinkerdInject::Enabled),
            curl.run("curl-uninjected", "http://nginx", LinkerdInject::Disabled),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_eq!(
            injected_status, 0,
            "uninjected curl must fail to contact nginx"
        );
        assert_ne!(uninjected_status, 0, "injected curl must contact nginx");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn targets_route() {
    with_temp_ns(|client, ns| async move {
        // First create all of the policies we'll need so that the nginx pod
        // starts up with the correct policy (to prevent races).
        //
        // The policy requires that all connections are authenticated with MeshTLS.
        let (srv, all_mtls) = tokio::join!(
            create(&client, nginx::server(&ns)),
            create(&client, all_authenticated(&ns)),
        );
        // Create a route which matches the /allowed path.
        let route = create(
            &client,
            http_route(&ns, &srv.name_unchecked(), NonZeroU16::new(80).unwrap()),
        )
        .await;
        // Create a policy which allows all authenticated clients
        create(
            &client,
            authz_policy(
                &ns,
                "nginx",
                LocalTargetRef::from_resource(&route),
                Some(NamespacedTargetRef::from_resource(&all_mtls)),
            ),
        )
        .await;

        // Create the nginx pod and wait for it to be ready.
        tokio::join!(
            create(&client, nginx::service(&ns)),
            create_ready_pod(&client, nginx::pod(&ns))
        );

        let curl = curl::Runner::init(&client, &ns).await;
        // TODO: Add a test case for a route which has no authorizations.
        // TODO: Add a test case for when there is an authorization on the server
        // but not on the matching route.
        let (allowed, no_route, unauth) = tokio::join!(
            curl.run(
                "curl-allowed",
                "http://nginx/allowed",
                LinkerdInject::Enabled
            ),
            curl.run(
                "curl-no-route",
                "http://nginx/noroute",
                LinkerdInject::Enabled
            ),
            curl.run(
                "curl-unauth",
                "http://nginx/allowed",
                LinkerdInject::Disabled
            )
        );
        let (allowed_status, no_route_status, unauth_status) = tokio::join!(
            allowed.exit_code(),
            no_route.exit_code(),
            unauth.exit_code()
        );
        assert_eq!(
            allowed_status, 0,
            "curling allowed route must contact nginx"
        );
        assert_ne!(
            no_route_status, 0,
            "curl which does not match route must not contact nginx"
        );
        assert_ne!(
            unauth_status, 0,
            "curl which is not authenticated must not contact nginx"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn targets_namespace() {
    with_temp_ns(|client, ns| async move {
        // First create all of the policies we'll need so that the nginx pod
        // starts up with the correct policy (to prevent races).
        //
        // The policy requires that all connections are authenticated with MeshTLS.
        let (_srv, all_mtls) = tokio::join!(
            create(&client, nginx::server(&ns)),
            create(&client, all_authenticated(&ns))
        );
        create(
            &client,
            authz_policy(
                &ns,
                "nginx",
                LocalTargetRef {
                    group: None,
                    kind: "Namespace".to_string(),
                    name: ns.clone(),
                },
                Some(NamespacedTargetRef::from_resource(&all_mtls)),
            ),
        )
        .await;

        // Create the nginx pod and wait for it to be ready.
        tokio::join!(
            create(&client, nginx::service(&ns)),
            create_ready_pod(&client, nginx::pod(&ns))
        );

        let curl = curl::Runner::init(&client, &ns).await;
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-injected", "http://nginx", LinkerdInject::Enabled),
            curl.run("curl-uninjected", "http://nginx", LinkerdInject::Disabled),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_eq!(injected_status, 0, "injected curl must contact nginx");
        assert_ne!(
            uninjected_status, 0,
            "uninjected curl must fail to contact nginx"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn meshtls_namespace() {
    with_temp_ns(|client, ns| async move {
        // First create all of the policies we'll need so that the nginx pod
        // starts up with the correct policy (to prevent races).
        //
        // The policy requires that all connections are authenticated with MeshTLS
        // and come from service accounts in the given namespace.
        let (srv, mtls_ns) = tokio::join!(
            create(&client, nginx::server(&ns)),
            create(&client, ns_authenticated(&ns))
        );
        create(
            &client,
            authz_policy(
                &ns,
                "nginx",
                LocalTargetRef::from_resource(&srv),
                Some(NamespacedTargetRef::from_resource(&mtls_ns)),
            ),
        )
        .await;

        // Create the nginx pod and wait for it to be ready.
        tokio::join!(
            create(&client, nginx::service(&ns)),
            create_ready_pod(&client, nginx::pod(&ns))
        );

        let curl = curl::Runner::init(&client, &ns).await;
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-injected", "http://nginx", LinkerdInject::Enabled),
            curl.run("curl-uninjected", "http://nginx", LinkerdInject::Disabled),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_eq!(injected_status, 0, "injected curl must contact nginx");
        assert_ne!(
            uninjected_status, 0,
            "uninjected curl must fail to contact nginx"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn network() {
    // In order to test the network policy, we need to create the client pod
    // before creating the authorization policy. To avoid races, we do this by
    // creating a `curl-lock` configmap that prevents curl from actually being
    // executed. Once nginx is running with the correct policy, the configmap is
    // deleted to unblock the curl pods.
    with_temp_ns(|client, ns| async move {
        let curl = curl::Runner::init(&client, &ns).await;
        curl.create_lock().await;

        // Create a curl pod and wait for it to get an IP.
        let blessed = curl
            .run("curl-blessed", "http://nginx", LinkerdInject::Disabled)
            .await;
        let blessed_ip = blessed.ip().await;
        tracing::debug!(curl.blessed.ip = %blessed_ip);

        // Once we know the IP of the (blocked) pod, create an nginx
        // authorization policy that permits connections from this pod.
        let (srv, allow_ips) = tokio::join!(
            create(&client, nginx::server(&ns)),
            create(&client, allow_ips(&ns, Some(blessed_ip)))
        );
        create(
            &client,
            authz_policy(
                &ns,
                "nginx",
                LocalTargetRef::from_resource(&srv),
                Some(NamespacedTargetRef::from_resource(&allow_ips)),
            ),
        )
        .await;

        // Start nginx with the policy.
        tokio::join!(
            create(&client, nginx::service(&ns)),
            create_ready_pod(&client, nginx::pod(&ns))
        );

        // Once the nginx pod is ready, delete the `curl-lock` configmap to
        // unblock curl from running.
        curl.delete_lock().await;

        // The blessed pod should be able to connect to the nginx pod.
        let status = blessed.exit_code().await;
        assert_eq!(status, 0, "blessed curl pod must succeed");

        // Create another curl pod that is not included in the authorization. It
        // should fail to connect to the nginx pod.
        let status = curl
            .run("curl-cursed", "http://nginx", LinkerdInject::Disabled)
            .await
            .exit_code()
            .await;
        assert_ne!(status, 0, "cursed curl pod must fail");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn both() {
    // In order to test the network policy, we need to create the client pod
    // before creating the authorization policy. To avoid races, we do this by
    // creating a `curl-lock` configmap that prevents curl from actually being
    // executed. Once nginx is running with the correct policy, the configmap is
    // deleted to unblock the curl pods.
    with_temp_ns(|client, ns| async move {
        let curl = curl::Runner::init(&client, &ns).await;
        curl.create_lock().await;

        let (blessed_injected, blessed_uninjected) = tokio::join!(
            curl.run(
                "curl-blessed-injected",
                "http://nginx",
                LinkerdInject::Enabled,
            ),
            curl.run(
                "curl-blessed-uninjected",
                "http://nginx",
                LinkerdInject::Disabled,
            )
        );
        let (blessed_injected_ip, blessed_uninjected_ip) =
            tokio::join!(blessed_injected.ip(), blessed_uninjected.ip(),);
        tracing::debug!(curl.blessed.injected.ip = ?blessed_injected_ip);
        tracing::debug!(curl.blessed.uninjected.ip = ?blessed_uninjected_ip);

        // Once we know the IP of the (blocked) pod, create an nginx
        // authorization policy that permits connections from this pod.
        let (srv, allow_ips, all_mtls) = tokio::join!(
            create(&client, nginx::server(&ns)),
            create(
                &client,
                allow_ips(&ns, vec![blessed_injected_ip, blessed_uninjected_ip]),
            ),
            create(&client, all_authenticated(&ns))
        );
        create(
            &client,
            authz_policy(
                &ns,
                "nginx",
                LocalTargetRef::from_resource(&srv),
                vec![
                    NamespacedTargetRef::from_resource(&allow_ips),
                    NamespacedTargetRef::from_resource(&all_mtls),
                ],
            ),
        )
        .await;

        // Start nginx with the policy.
        tokio::join!(
            create(&client, nginx::service(&ns)),
            create_ready_pod(&client, nginx::pod(&ns))
        );

        // Once the nginx pod is ready, delete the `curl-lock` configmap to
        // unblock curl from running.
        curl.delete_lock().await;
        tracing::info!("unblocked curl");

        let (blessed_injected_status, blessed_uninjected_status) =
            tokio::join!(blessed_injected.exit_code(), blessed_uninjected.exit_code());
        // The blessed and injected pod should be able to connect to the nginx pod.
        assert_eq!(
            blessed_injected_status, 0,
            "blessed injected curl pod must succeed"
        );
        // The blessed and uninjected pod should NOT be able to connect to the nginx pod.
        assert_ne!(
            blessed_uninjected_status, 0,
            "blessed uninjected curl pod must NOT succeed"
        );

        let (cursed_injected, cursed_uninjected) = tokio::join!(
            curl.run(
                "curl-cursed-injected",
                "http://nginx",
                LinkerdInject::Enabled,
            ),
            curl.run(
                "curl-cursed-uninjected",
                "http://nginx",
                LinkerdInject::Disabled,
            )
        );
        let (cursed_injected_status, cursed_uninjected_status) =
            tokio::join!(cursed_injected.exit_code(), cursed_uninjected.exit_code(),);
        assert_ne!(
            cursed_injected_status, 0,
            "cursed injected curl pod must fail"
        );
        assert_ne!(
            cursed_uninjected_status, 0,
            "cursed uninjected curl pod must fail"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn either() {
    // In order to test the network policy, we need to create the client pod
    // before creating the authorization policy. To avoid races, we do this by
    // creating a `curl-lock` configmap that prevents curl from actually being
    // executed. Once nginx is running with the correct policy, the configmap is
    // deleted to unblock the curl pods.
    with_temp_ns(|client, ns| async move {
        let curl = curl::Runner::init(&client, &ns).await;
        curl.create_lock().await;

        let (blessed_injected, blessed_uninjected) = tokio::join!(
            curl.run(
                "curl-blessed-injected",
                "http://nginx",
                LinkerdInject::Enabled,
            ),
            curl.run(
                "curl-blessed-uninjected",
                "http://nginx",
                LinkerdInject::Disabled,
            )
        );
        let (blessed_injected_ip, blessed_uninjected_ip) =
            tokio::join!(blessed_injected.ip(), blessed_uninjected.ip());
        tracing::debug!(curl.blessed.injected.ip = ?blessed_injected_ip);
        tracing::debug!(curl.blessed.uninjected.ip = ?blessed_uninjected_ip);

        // Once we know the IP of the (blocked) pod, create an nginx
        // authorization policy that permits connections from this pod.
        let (srv, allow_ips, all_mtls) = tokio::join!(
            create(&client, nginx::server(&ns)),
            create(&client, allow_ips(&ns, vec![blessed_uninjected_ip])),
            create(&client, all_authenticated(&ns))
        );
        tokio::join!(
            create(
                &client,
                authz_policy(
                    &ns,
                    "nginx-from-ip",
                    LocalTargetRef::from_resource(&srv),
                    vec![NamespacedTargetRef::from_resource(&allow_ips)],
                ),
            ),
            create(
                &client,
                authz_policy(
                    &ns,
                    "nginx-from-id",
                    LocalTargetRef::from_resource(&srv),
                    vec![NamespacedTargetRef::from_resource(&all_mtls)],
                ),
            )
        );

        // Start nginx with the policy.
        tokio::join!(
            create(&client, nginx::service(&ns)),
            create_ready_pod(&client, nginx::pod(&ns)),
        );

        // Once the nginx pod is ready, delete the `curl-lock` configmap to
        // unblock curl from running.
        curl.delete_lock().await;
        tracing::info!("unblocking curl");

        let (blessed_injected_status, blessed_uninjected_status) =
            tokio::join!(blessed_injected.exit_code(), blessed_uninjected.exit_code());
        // The blessed and injected pod should be able to connect to the nginx pod.
        assert_eq!(
            blessed_injected_status, 0,
            "blessed injected curl pod must succeed"
        );
        // The blessed and uninjected pod should NOT be able to connect to the nginx pod.
        assert_eq!(
            blessed_uninjected_status, 0,
            "blessed uninjected curl pod must succeed"
        );

        let (cursed_injected, cursed_uninjected) = tokio::join!(
            curl.run(
                "curl-cursed-injected",
                "http://nginx",
                LinkerdInject::Enabled,
            ),
            curl.run(
                "curl-cursed-uninjected",
                "http://nginx",
                LinkerdInject::Disabled,
            ),
        );
        let (cursed_injected_status, cursed_uninjected_status) =
            tokio::join!(cursed_injected.exit_code(), cursed_uninjected.exit_code());
        assert_eq!(
            cursed_injected_status, 0,
            "cursed injected curl pod must succeed"
        );
        assert_ne!(
            cursed_uninjected_status, 0,
            "cursed uninjected curl pod must fail"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn empty_authentications() {
    with_temp_ns(|client, ns| async move {
        // Create a policy that does not require any authentications.
        let srv = create(&client, nginx::server(&ns)).await;
        create(
            &client,
            authz_policy(&ns, "nginx", LocalTargetRef::from_resource(&srv), None),
        )
        .await;

        // Create the nginx pod and wait for it to be ready.
        tokio::join!(
            create(&client, nginx::service(&ns)),
            create_ready_pod(&client, nginx::pod(&ns))
        );

        // All requests should work.
        let curl = curl::Runner::init(&client, &ns).await;
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-injected", "http://nginx", LinkerdInject::Enabled),
            curl.run("curl-uninjected", "http://nginx", LinkerdInject::Disabled),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_eq!(injected_status, 0, "injected curl must contact nginx");
        assert_eq!(uninjected_status, 0, "uninjected curl must contact nginx");
    })
    .await;
}

// === helpers ===

fn authz_policy(
    ns: &str,
    name: &str,
    target: LocalTargetRef,
    authns: impl IntoIterator<Item = NamespacedTargetRef>,
) -> k8s::policy::AuthorizationPolicy {
    k8s::policy::AuthorizationPolicy {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::AuthorizationPolicySpec {
            target_ref: target,
            required_authentication_refs: authns.into_iter().collect(),
        },
    }
}

fn all_authenticated(ns: &str) -> k8s::policy::MeshTLSAuthentication {
    k8s::policy::MeshTLSAuthentication {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("all-authenticated".to_string()),
            ..Default::default()
        },
        spec: k8s::policy::MeshTLSAuthenticationSpec {
            identity_refs: None,
            identities: Some(vec!["*".to_string()]),
        },
    }
}

fn ns_authenticated(ns: &str) -> k8s::policy::MeshTLSAuthentication {
    k8s::policy::MeshTLSAuthentication {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("all-authenticated".to_string()),
            ..Default::default()
        },
        spec: k8s::policy::MeshTLSAuthenticationSpec {
            identity_refs: Some(vec![NamespacedTargetRef {
                group: None,
                kind: "Namespace".to_string(),
                name: ns.to_string(),
                namespace: None,
            }]),
            identities: None,
        },
    }
}

fn allow_ips(
    ns: &str,
    ips: impl IntoIterator<Item = std::net::IpAddr>,
) -> k8s::policy::NetworkAuthentication {
    k8s::policy::NetworkAuthentication {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("allow-pod".to_string()),
            ..Default::default()
        },
        spec: k8s::policy::NetworkAuthenticationSpec {
            networks: ips
                .into_iter()
                .map(|ip| k8s::policy::Network {
                    cidr: ip.into(),
                    except: None,
                })
                .collect(),
        },
    }
}

fn http_route(ns: &str, server_name: &str, port: NonZeroU16) -> k8s_gateway_api::HttpRoute {
    k8s_gateway_api::HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("allowed-route".to_string()),
            ..Default::default()
        },
        spec: k8s_gateway_api::HttpRouteSpec {
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: Some("Server".to_string()),
                    namespace: Some(ns.to_string()),
                    name: server_name.to_string(),
                    section_name: None,
                    port: Some(port.into()),
                }]),
            },
            hostnames: None,
            rules: Some(vec![k8s_gateway_api::HttpRouteRule {
                matches: Some(vec![k8s_gateway_api::HttpRouteMatch {
                    path: Some(k8s_gateway_api::HttpPathMatch::Exact {
                        value: "/allowed".to_string(),
                    }),
                    ..Default::default()
                }]),
                filters: None,
                backend_refs: None,
            }]),
        },
        status: None,
    }
}
