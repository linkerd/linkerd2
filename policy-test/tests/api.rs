use std::time::Duration;

use futures::prelude::*;
use kube::ResourceExt;
use linkerd_policy_controller_core::{Ipv4Net, Ipv6Net};
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{
    assert_is_default_all_unauthenticated, assert_protocol_detect, create, create_ready_pod, grpc,
    with_temp_ns,
};
use maplit::{btreemap, convert_args, hashmap};
use tokio::time;

/// Creates a pod, watches its policy, and updates policy resources that impact
/// the watch.
#[tokio::test(flavor = "current_thread")]
async fn server_with_server_authorization() {
    with_temp_ns(|client, ns| async move {
        // Create a pod that does nothing. It's injected with a proxy, so we can
        // attach policies to its admin server.
        let pod = create_ready_pod(&client, mk_pause(&ns, "pause")).await;
        tracing::trace!(?pod);

        let mut rx = retry_watch_server(&client, &ns, &pod.name()).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect!(config);

        // Create a server that selects the pod's proxy admin server and ensure
        // that the update now uses this server, which has no authorizations
        let server = create(&client, mk_admin_server(&ns, "linkerd-admin")).await;
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
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "server",
                "name" => "linkerd-admin"
            )),
        );

        // Create a server authorizaation that refers to the `linkerd-admin`
        // server (by name) and ensure that the update now reflects this
        // authorization.
        create(
            &client,
            k8s::policy::ServerAuthorization {
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
        .await;
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
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "serverauthorization",
                "name" => "all-admin",
            )),
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
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "server",
                "name" => server.name()
            ))
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
        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect!(config);
    })
    .await;
}

/// Creates a pod, watches its policy, and updates policy resources that impact
/// the watch.
#[tokio::test(flavor = "current_thread")]
async fn server_with_authorization_policy() {
    with_temp_ns(|client, ns| async move {
        // Create a pod that does nothing. It's injected with a proxy, so we can
        // attach policies to its admin server.
        let pod = create_ready_pod(&client, mk_pause(&ns, "pause")).await;
        tracing::trace!(?pod);

        let mut rx = retry_watch_server(&client, &ns, &pod.name()).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect!(config);

        // Create a server that selects the pod's proxy admin server and ensure
        // that the update now uses this server, which has no authorizations
        let server = create(&client, mk_admin_server(&ns, "linkerd-admin")).await;
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
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "server",
                "name" => server.name()
            ))
        );

        let all_nets = create(
            &client,
            k8s::policy::NetworkAuthentication {
                metadata: kube::api::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("all-admin".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::NetworkAuthenticationSpec {
                    networks: vec![
                        k8s::policy::network_authentication::Network {
                            cidr: Ipv4Net::default().into(),
                            except: None,
                        },
                        k8s::policy::network_authentication::Network {
                            cidr: Ipv6Net::default().into(),
                            except: None,
                        },
                    ],
                },
            },
        )
        .await;

        let authz_policy = create(
            &client,
            k8s::policy::AuthorizationPolicy {
                metadata: kube::api::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("all-admin".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::AuthorizationPolicySpec {
                    target_ref: k8s::policy::LocalTargetRef::from_resource(&server),
                    required_authentication_refs: vec![
                        k8s::policy::NamespacedTargetRef::from_resource(&all_nets),
                    ],
                },
            },
        )
        .await;

        let config = time::timeout(time::Duration::from_secs(10), rx.next())
            .await
            .expect("watch must update within 10s")
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
        assert_eq!(config.authorizations.len(), 1);
        assert_eq!(
            config.authorizations.first().unwrap().labels,
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "authorizationpolicy",
                "name" => authz_policy.name(),
            ))
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
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "server",
                "name" => server.name()
            ))
        );
    })
    .await;
}

fn mk_pause(ns: &str, name: &str) -> k8s::Pod {
    k8s::Pod {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            annotations: Some(convert_args!(btreemap!(
                "linkerd.io/inject" => "enabled",
            ))),
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

async fn retry_watch_server(
    client: &kube::Client,
    ns: &str,
    pod_name: &str,
) -> tonic::Streaming<grpc::inbound::Server> {
    // Port-forward to the control plane and start watching the pod's admin
    // server's policy and ensure that the first update uses the default
    // policy.
    let mut policy_api = grpc::PolicyClient::port_forwarded(client).await;
    loop {
        match policy_api.watch_port(ns, pod_name, 4191).await {
            Ok(rx) => return rx,
            Err(error) => {
                tracing::error!(?error);
                time::sleep(Duration::from_secs(1)).await;
            }
        }
    }
}
