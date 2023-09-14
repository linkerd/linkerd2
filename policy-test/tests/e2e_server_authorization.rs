use linkerd_policy_controller_k8s_api::{
    self as k8s, policy::server_authorization::Client as ClientAuthz, ResourceExt,
};
use linkerd_policy_test::{
    await_condition, create, create_ready_pod, curl, endpoints_ready, web, with_temp_ns,
    LinkerdInject,
};

#[tokio::test(flavor = "current_thread")]
async fn meshtls() {
    with_temp_ns(|client, ns| async move {
        let srv = create(&client, web::server(&ns)).await;

        create(
            &client,
            server_authz(
                &ns,
                "web",
                &srv,
                ClientAuthz {
                    mesh_tls: Some(k8s::policy::server_authorization::MeshTls {
                        identities: Some(vec!["*".to_string()]),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
            ),
        )
        .await;

        // Create the web pod and wait for it to be ready.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        let curl = curl::Runner::init(&client, &ns).await;
        let (injected, uninjected) = tokio::join!(
            curl.run("curl-injected", "http://web", LinkerdInject::Enabled),
            curl.run("curl-uninjected", "http://web", LinkerdInject::Disabled),
        );
        let (injected_status, uninjected_status) =
            tokio::join!(injected.exit_code(), uninjected.exit_code());
        assert_eq!(injected_status, 0, "injected curl must succeed");
        assert_eq!(uninjected_status, 22, "uninjected curl must fail");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn network() {
    with_temp_ns(|client, ns| async move {
        let curl = curl::Runner::init(&client, &ns).await;

        // Create a lock that prevents `curl` pods from running. This allows us
        // to create a curl pod so that its IP can be obtained to be used in an
        // authorization policy.
        curl.create_lock().await;

        // Create a curl pod and wait for it to get an IP.
        let blessed = curl
            .run("curl-blessed", "http://web", LinkerdInject::Disabled)
            .await;
        let blessed_ip = blessed.ip().await;
        tracing::debug!(curl.blessed.ip = %blessed_ip);

        // Once we know the IP of the (blocked) pod, create an web
        // authorization policy that permits connections from this pod.
        let srv = create(&client, web::server(&ns)).await;
        create(
            &client,
            server_authz(
                &ns,
                "web",
                &srv,
                ClientAuthz {
                    networks: Some(vec![k8s::policy::Network {
                        cidr: blessed_ip.into(),
                        except: None,
                    }]),
                    unauthenticated: true,
                    ..Default::default()
                },
            ),
        )
        .await;

        // Start web with the policy.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        // Once the web pod is ready, delete the `curl-lock` configmap to
        // unblock curl from running.
        curl.delete_lock().await;
        tracing::info!("unblocked curl");

        // The blessed pod should be able to connect to the web pod.
        let status = blessed.exit_code().await;
        assert_eq!(status, 0, "blessed curl must succeed");

        // Create another curl pod that is not included in the authorization. It
        // should fail to connect to the web pod.
        let status = curl
            .run("curl-cursed", "http://web", LinkerdInject::Disabled)
            .await
            .exit_code()
            .await;
        assert_eq!(status, 22, "cursed curl must fail");
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn both() {
    with_temp_ns(|client, ns| async move {
        let curl = curl::Runner::init(&client, &ns).await;

        // Create a lock that prevents `curl` pods from running. This allows us
        // to create a curl pod so that its IP can be obtained to be used in an
        // authorization policy.
        curl.create_lock().await;

        let (blessed_injected, blessed_uninjected) = tokio::join!(
            curl.run(
                "curl-blessed-injected",
                "http://web",
                LinkerdInject::Enabled,
            ),
            curl.run(
                "curl-blessed-uninjected",
                "http://web",
                LinkerdInject::Disabled,
            )
        );
        let (blessed_injected_ip, blessed_uninjected_ip) =
            tokio::join!(blessed_injected.ip(), blessed_uninjected.ip(),);
        tracing::debug!(curl.blessed.injected.ip = ?blessed_injected_ip);
        tracing::debug!(curl.blessed.uninjected.ip = ?blessed_uninjected_ip);

        // Once we know the IP of the (blocked) pod, create an web
        // authorization policy that permits connections from this pod.
        let srv = create(&client, web::server(&ns)).await;
        create(
            &client,
            server_authz(
                &ns,
                "web",
                &srv,
                ClientAuthz {
                    networks: Some(vec![
                        k8s::policy::Network {
                            cidr: blessed_injected_ip.into(),
                            except: None,
                        },
                        k8s::policy::Network {
                            cidr: blessed_uninjected_ip.into(),
                            except: None,
                        },
                    ]),
                    mesh_tls: Some(k8s::policy::server_authorization::MeshTls {
                        identities: Some(vec!["*".to_string()]),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
            ),
        )
        .await;

        // Start web with the policy.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns))
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        // Once the web pod is ready, delete the `curl-lock` configmap to
        // unblock curl from running.
        curl.delete_lock().await;
        tracing::info!("unblocked curl");

        let (blessed_injected_status, blessed_uninjected_status) =
            tokio::join!(blessed_injected.exit_code(), blessed_uninjected.exit_code());
        // The blessed and injected pod should be able to connect to the web pod.
        assert_eq!(
            blessed_injected_status, 0,
            "blessed injected curl must succeed"
        );
        // The blessed and uninjected pod should NOT be able to connect to the web pod.
        assert_eq!(
            blessed_uninjected_status, 22,
            "blessed uninjected curl must fail"
        );

        let (cursed_injected, cursed_uninjected) = tokio::join!(
            curl.run("curl-cursed-injected", "http://web", LinkerdInject::Enabled,),
            curl.run(
                "curl-cursed-uninjected",
                "http://web",
                LinkerdInject::Disabled,
            )
        );
        let (cursed_injected_status, cursed_uninjected_status) =
            tokio::join!(cursed_injected.exit_code(), cursed_uninjected.exit_code(),);
        assert_eq!(cursed_injected_status, 22, "cursed injected curl must fail");
        assert_eq!(
            cursed_uninjected_status, 22,
            "cursed uninjected curl must fail"
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn either() {
    with_temp_ns(|client, ns| async move {
        let curl = curl::Runner::init(&client, &ns).await;

        tracing::info!("Blocking curl");
        curl.create_lock().await;

        let (blessed_injected, blessed_uninjected) = tokio::join!(
            curl.run(
                "curl-blessed-injected",
                "http://web",
                LinkerdInject::Enabled,
            ),
            curl.run(
                "curl-blessed-uninjected",
                "http://web",
                LinkerdInject::Disabled,
            )
        );
        let (blessed_injected_ip, blessed_uninjected_ip) =
            tokio::join!(blessed_injected.ip(), blessed_uninjected.ip());
        tracing::debug!(curl.blessed.injected.ip = ?blessed_injected_ip);
        tracing::debug!(curl.blessed.uninjected.ip = ?blessed_uninjected_ip);

        // Once we know the IP of the (blocked) pod, create an web
        // authorization policy that permits connections from this pod.
        let srv = create(&client, web::server(&ns)).await;
        tokio::join!(
            create(
                &client,
                server_authz(
                    &ns,
                    "web-from-ip",
                    &srv,
                    ClientAuthz {
                        unauthenticated: true,
                        networks: Some(vec![
                            k8s::policy::Network {
                                cidr: blessed_injected_ip.into(),
                                except: None,
                            },
                            k8s::policy::Network {
                                cidr: blessed_uninjected_ip.into(),
                                except: None,
                            },
                        ]),
                        ..Default::default()
                    },
                )
            ),
            create(
                &client,
                server_authz(
                    &ns,
                    "web-from-id",
                    &srv,
                    ClientAuthz {
                        mesh_tls: Some(k8s::policy::server_authorization::MeshTls {
                            identities: Some(vec!["*".to_string()]),
                            ..Default::default()
                        }),
                        ..Default::default()
                    },
                )
            ),
        );

        // Start web with the policy.
        tokio::join!(
            create(&client, web::service(&ns)),
            create_ready_pod(&client, web::pod(&ns)),
        );

        await_condition(&client, &ns, "web", endpoints_ready).await;

        // Once the web pod is ready, delete the `curl-lock` configmap to
        // unblock curl from running.
        curl.delete_lock().await;
        tracing::info!("unblocked curl");

        let (blessed_injected_status, blessed_uninjected_status) =
            tokio::join!(blessed_injected.exit_code(), blessed_uninjected.exit_code());
        assert_eq!(
            blessed_injected_status, 0,
            "blessed injected curl must succeed"
        );
        assert_eq!(
            blessed_uninjected_status, 0,
            "blessed uninjected curl must succeed"
        );

        let (cursed_injected, cursed_uninjected) = tokio::join!(
            curl.run("curl-cursed-injected", "http://web", LinkerdInject::Enabled,),
            curl.run(
                "curl-cursed-uninjected",
                "http://web",
                LinkerdInject::Disabled,
            ),
        );
        let (cursed_injected_status, cursed_uninjected_status) =
            tokio::join!(cursed_injected.exit_code(), cursed_uninjected.exit_code());
        assert_eq!(
            cursed_injected_status, 0,
            "cursed injected curl must succeed"
        );
        assert_eq!(
            cursed_uninjected_status, 22,
            "cursed uninjected curl must fail"
        );
    })
    .await;
}

// === helpers ===

fn server_authz(
    ns: &str,
    name: &str,
    target: &k8s::policy::Server,
    client: ClientAuthz,
) -> k8s::policy::ServerAuthorization {
    k8s::policy::ServerAuthorization {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::ServerAuthorizationSpec {
            server: k8s::policy::server_authorization::Server {
                name: Some(target.name_unchecked()),
                selector: None,
            },
            client,
        },
    }
}
