use anyhow::Result;
use futures::prelude::*;
use kube::{
    runtime::wait::{await_condition, conditions},
    ResourceExt,
};
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{grpc, with_temp_ns};
use tokio::time;

/// Creates a pod, watches its policy, and updates policy resources that impact
/// the watch.
#[tokio::test(flavor = "current_thread")]
async fn grpc_watch_updates_as_resources_change() {
    with_temp_ns(|client, ns| async move {
        // Create a pod that does nothing. It's injected with a proxy, so we can
        // attach policies to its admin server.
        let pod = create_pod(&client, mk_pause(&ns, "pause"))
            .await
            .expect("failed to create pod");
        tracing::trace!(?pod);

        // Port-forward to the control plane and start watching the pod's admin
        // server's policy and ensure that the first update uses the default
        // policy.
        let mut policy_api = grpc::PolicyClient::port_forwarded(&client)
            .await
            .expect("must establish a port-forwarded client");
        let mut rx = policy_api
            .watch_port(&ns, &pod.name(), 4191)
            .await
            .expect("failed to establish watch");
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_eq!(
            config.protocol,
            Some(grpc::inbound::ProxyProtocol {
                kind: Some(grpc::inbound::proxy_protocol::Kind::Detect(
                    grpc::inbound::proxy_protocol::Detect {
                        timeout: Some(time::Duration::from_secs(10).into()),
                    }
                )),
            }),
        );
        assert!(config.authorizations.is_empty());
        assert_eq!(
            config.labels,
            Some(("name".to_string(), "default:deny".to_string()))
                .into_iter()
                .collect()
        );

        // Create a server that selects the pod's proxy admin server and ensure
        // that the update now uses this server, which has no authorizations
        let server = kube::Api::<k8s::policy::Server>::namespaced(client.clone(), &ns)
            .create(
                &kube::api::PostParams::default(),
                &mk_admin_server(&ns, "linkerd-admin"),
            )
            .await
            .expect("server must apply");
        tracing::trace!(?server);
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);
        assert_eq!(
            config.protocol,
            Some(grpc::inbound::ProxyProtocol {
                kind: Some(grpc::inbound::proxy_protocol::Kind::Http1(
                    grpc::inbound::proxy_protocol::Http1::default()
                )),
            }),
        );
        assert_eq!(config.authorizations, vec![]);
        assert_eq!(
            config.labels,
            Some(("name".to_string(), "linkerd-admin".to_string()))
                .into_iter()
                .collect()
        );

        // Create a server authorizaation that refers to the `linkerd-admin`
        // server (by name) and ensure that the update now reflects this
        // authorization.
        let server_authz =
            kube::Api::<k8s::policy::ServerAuthorization>::namespaced(client.clone(), &ns)
                .create(
                    &kube::api::PostParams::default(),
                    &k8s::policy::ServerAuthorization {
                        metadata: kube::api::ObjectMeta {
                            namespace: Some(ns.clone()),
                            name: Some("all-admin".to_string()),
                            ..Default::default()
                        },
                        spec: k8s::policy::ServerAuthorizationSpec {
                            server: k8s::policy::server_authorization::Server {
                                name: Some("linkerd-admin".to_string()),
                                selector: None,
                            },
                            client: k8s::policy::server_authorization::Client {
                                unauthenticated: true,
                                ..k8s::policy::server_authorization::Client::default()
                            },
                        },
                    },
                )
                .await
                .expect("serverauthorization must apply");
        tracing::trace!(?server_authz);
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);
        assert_eq!(
            config.protocol,
            Some(grpc::inbound::ProxyProtocol {
                kind: Some(grpc::inbound::proxy_protocol::Kind::Http1(
                    grpc::inbound::proxy_protocol::Http1::default()
                )),
            }),
        );
        assert_eq!(
            config.authorizations.first().unwrap().labels,
            Some(("name".to_string(), "all-admin".to_string()))
                .into_iter()
                .collect()
        );
        assert_eq!(
            *config
                .authorizations
                .first()
                .unwrap()
                .authentication
                .as_ref()
                .unwrap(),
            grpc::inbound::Authn {
                permit: Some(grpc::inbound::authn::Permit::Unauthenticated(
                    grpc::inbound::authn::PermitUnauthenticated {}
                )),
            }
        );
        assert_eq!(
            config.labels,
            Some(("name".to_string(), "linkerd-admin".to_string()))
                .into_iter()
                .collect()
        );

        // Delete the `Server` and ensure that the update reverts to the
        // default.
        kube::Api::<k8s::policy::Server>::namespaced(client.clone(), &ns)
            .delete(&server.name(), &kube::api::DeleteParams::default())
            .await
            .expect("Server must be deleted");
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");
        tracing::trace!(?config);
        assert_eq!(
            config.labels,
            Some(("name".to_string(), "default:deny".to_string()))
                .into_iter()
                .collect()
        );
    })
    .await;
}

async fn create_pod(client: &kube::Client, pod: k8s::Pod) -> Result<k8s::Pod> {
    let api = kube::Api::<k8s::Pod>::namespaced(client.clone(), &pod.namespace().unwrap());
    let pod = api.create(&kube::api::PostParams::default(), &pod).await?;
    time::timeout(
        time::Duration::from_secs(60),
        await_condition(api, &pod.name(), conditions::is_pod_running()),
    )
    .await??;
    Ok(pod)
}

fn mk_pause(ns: &str, name: &str) -> k8s::Pod {
    k8s::Pod {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            annotations: Some(
                vec![
                    ("linkerd.io/inject".to_string(), "enabled".to_string()),
                    (
                        "config.linkerd.io/default-inbound-policy".to_string(),
                        "deny".to_string(),
                    ),
                ]
                .into_iter()
                .collect(),
            ),
            ..Default::default()
        },
        spec: Some(k8s::PodSpec {
            containers: vec![k8s::api::core::v1::Container {
                name: "pause".to_string(),
                image: Some("gcr.io/google_containers/pause:3.2".to_string()),
                ..Default::default()
            }],
            ..Default::default()
        }),
        ..k8s::Pod::default()
    }
}

fn mk_admin_server(ns: &str, name: &str) -> k8s::policy::Server {
    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            pod_selector: k8s::labels::Selector::default(),
            port: k8s::policy::server::Port::Number(4191),
            proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
        },
    }
}
