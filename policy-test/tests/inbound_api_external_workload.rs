use std::{num::NonZeroU16, time::Duration};

use futures::prelude::*;
use k8s::policy::LocalTargetRef;
use kube::ResourceExt;
use linkerd_policy_controller_core::{Ipv4Net, Ipv6Net};
use linkerd_policy_controller_k8s_api::{self as k8s, gateway};
use linkerd_policy_test::{
    assert_default_all_unauthenticated_labels, assert_is_default_all_unauthenticated,
    assert_protocol_detect_external, create, grpc, with_temp_ns,
};
use maplit::{btreemap, convert_args, hashmap};
use tokio::time;

#[tokio::test(flavor = "current_thread")]
async fn external_workload_srv_with_authorization_policy() {
    with_temp_ns(|client, ns| async move {
        // Create an external workload object.
        let ext_workload = create(&client, mk_external_workload(&ns, "wkld-1")).await;

        tracing::trace!(
            external_workload = %ext_workload.name_any(),
            ip = ?ext_workload.spec.workload_ips.as_ref().unwrap()[0]
        );

        let mut rx = retry_watch_server(&client, &ns, &ext_workload.name_any()).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect_external!(config);

        // Create a server that selects the http port on the workload and ensure
        // the update now uses this server (which has no authorizations)
        let server = create(&client, mk_http_server(&ns, "external-http")).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_external())
        );
        assert_eq!(config.authorizations, vec![]);
        assert_eq!(
            config.labels,
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "server",
                "name" => "external-http"
            )),
        );

        // Create a server authorization that refers to the `linkerd-admin`
        // server (by name) and ensure that the update now reflects this
        // authorization.
        create(
            &client,
            k8s::policy::AuthorizationPolicy {
                metadata: kube::api::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("all-http".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::AuthorizationPolicySpec {
                    target_ref: LocalTargetRef {
                        group: Some("policy.linkerd.io".to_string()),
                        kind: "server".to_string(),
                        name: server.name_any(),
                    },
                    required_authentication_refs: vec![],
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
            Some(grpc::defaults::proxy_protocol_external())
        );
        assert_eq!(
            config.authorizations.first().unwrap().labels,
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "authorizationpolicy",
                "name" => "all-http",
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
                "name" => server.name_unchecked()
            ))
        );

        // Delete the `Server` and ensure that the update reverts to the
        // default.
        kube::Api::<k8s::policy::Server>::namespaced(client.clone(), &ns)
            .delete(
                &server.name_unchecked(),
                &kube::api::DeleteParams::default(),
            )
            .await
            .expect("Server must be deleted");
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an updated config");

        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect_external!(config);
    })
    .await
}

#[tokio::test(flavor = "current_thread")]
async fn external_workload_srv_with_http_route() {
    with_temp_ns(|client, ns| async move {
        // Create an external workload object.
        let ext_workload = create(&client, mk_external_workload(&ns, "wkld-1")).await;

        tracing::trace!(
            external_workload = %ext_workload.name_any(),
            ip = ?ext_workload.spec.workload_ips.as_ref().unwrap()[0]
        );

        let mut rx = retry_watch_server(&client, &ns, &ext_workload.name_any()).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect_external!(config);

        // Create a server that selects the http port on the workload and ensure
        // the update now uses this server (which has no authorizations)
        let server = create(&client, mk_http_server(&ns, "external-http")).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_external())
        );
        assert_eq!(config.authorizations, vec![]);
        assert_eq!(
            config.labels,
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "server",
                "name" => "external-http"
            )),
        );

        let created_route = {
            use k8s::policy::httproute as api;
            let http_route = api::HttpRoute {
                metadata: kube::api::ObjectMeta {
                    namespace: Some(ns.to_string()),
                    name: Some("http-route".to_string()),
                    ..Default::default()
                },
                spec: api::HttpRouteSpec {
                    parent_refs: Some(vec![gateway::HTTPRouteParentRefs {
                        group: Some("policy.linkerd.io".to_string()),
                        kind: Some("Server".to_string()),
                        name: server.name_any(),
                        namespace: None,
                        section_name: None,
                        port: None,
                    }]),
                    hostnames: None,
                    rules: Some(vec![api::HttpRouteRule {
                        matches: Some(vec![gateway::HTTPRouteRulesMatches {
                            path: Some(gateway::HTTPRouteRulesMatchesPath {
                                value: Some("/endpoint".to_string()),
                                r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                            }),
                            headers: None,
                            query_params: None,
                            method: Some(gateway::HTTPRouteRulesMatchesMethod::Get),
                        }]),
                        filters: None,
                        backend_refs: None,
                        timeouts: None,
                    }]),
                },
                status: None,
            };

            create(&client, http_route).await
        };

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        let kind = config
            .protocol
            .as_ref()
            .expect("must have proxy protocol")
            .kind
            .as_ref()
            .expect("must have kind");
        let routes = if let grpc::inbound::proxy_protocol::Kind::Http1(ref http1) = kind {
            &http1.routes[..]
        } else {
            panic!("proxy protocol must be 'Http1'; actually got:\n{kind:#?}");
        };

        assert_eq!(routes.len(), 1);
        let route = routes.first().expect("must have route");
        // Route should have no authz policy by default
        assert_eq!(route.authorizations, vec![]);
        assert_eq!(
            route.metadata.to_owned().expect("route must have metadata"),
            grpc::meta::Metadata {
                kind: Some(grpc::meta::metadata::Kind::Resource(grpc::meta::Resource {
                    group: "policy.linkerd.io".to_string(),
                    kind: "HTTPRoute".to_string(),
                    name: "http-route".to_string(),
                    ..Default::default()
                }))
            }
        );

        // Route has path match
        let rule_match = route
            .rules
            .first()
            .expect("must have rule")
            .matches
            .first()
            .expect("must have match");
        assert_eq!(
            rule_match
                .path
                .to_owned()
                .expect("must have path match")
                .kind
                .expect("must have kind"),
            grpc::http_route::path_match::Kind::Exact("/endpoint".to_string())
        );

        // Create a network authn and a policy that refers to the route
        let all_networks = create(
            &client,
            k8s::policy::NetworkAuthentication {
                metadata: kube::api::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("all-net".to_string()),
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
        create(
            &client,
            k8s::policy::AuthorizationPolicy {
                metadata: kube::api::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("all-net".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::AuthorizationPolicySpec {
                    target_ref: k8s::policy::LocalTargetRef::from_resource(&created_route),
                    required_authentication_refs: vec![
                        k8s::policy::NamespacedTargetRef::from_resource(&all_networks),
                    ],
                },
            },
        )
        .await;

        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        let http1 = if let grpc::inbound::proxy_protocol::Kind::Http1(http1) = config
            .protocol
            .expect("must have proxy protocol")
            .kind
            .expect("must have kind")
        {
            http1
        } else {
            panic!("proxy protocol must be HTTP1");
        };
        let h1_route = http1.routes.first().expect("must have route");
        assert_eq!(h1_route.authorizations.len(), 1, "must have authorizations");

        // Delete the `HttpRoute` and ensure that the update reverts to the
        // default.
        kube::Api::<k8s::policy::HttpRoute>::namespaced(client.clone(), &ns)
            .delete("http-route", &kube::api::DeleteParams::default())
            .await
            .expect("HttpRoute must be deleted");
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_external())
        );
    })
    .await;
}
#[tokio::test(flavor = "current_thread")]
async fn external_workload_default_http_route() {
    with_temp_ns(|client, ns| async move {
        // Create an external workload object.
        let ext_workload = create(&client, mk_external_workload(&ns, "wkld-1")).await;

        tracing::trace!(
            external_workload = %ext_workload.name_any(),
            ip = ?ext_workload.spec.workload_ips.as_ref().unwrap()[0]
        );

        let mut rx = retry_watch_server(&client, &ns, &ext_workload.name_any()).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect_external!(config);

        let kind = config
            .protocol
            .as_ref()
            .expect("must have proxy protocol")
            .kind
            .as_ref()
            .expect("must have kind");
        let routes = if let grpc::inbound::proxy_protocol::Kind::Detect(ref detect) = kind {
            &detect.http_routes[..]
        } else {
            panic!("proxy protocol must be 'Detect'; actually got:\n{kind:#?}");
        };

        assert_eq!(routes.len(), 1);
        let route_authzs = &routes[0].authorizations;
        assert_eq!(route_authzs.len(), 0);
    })
    .await
}

fn mk_external_workload(ns: &str, name: &str) -> k8s::external_workload::ExternalWorkload {
    k8s::external_workload::ExternalWorkload {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.into()),
            name: Some(name.into()),
            labels: Some(convert_args!(btreemap!(
                        "app" => "ext",
            ))),
            ..Default::default()
        },
        spec: k8s::external_workload::ExternalWorkloadSpec {
            mesh_tls: k8s::external_workload::MeshTls {
                identity: "some-identity".to_string(),
                server_name: "some-sni".to_string(),
            },
            ports: Some(vec![k8s::external_workload::PortSpec {
                name: Some("http".into()),
                port: NonZeroU16::new(80).unwrap(),
                protocol: Default::default(),
            }]),
            workload_ips: Some(vec![k8s::external_workload::WorkloadIP {
                ip: "192.0.2.0".to_string(),
            }]),
        },
        status: None,
    }
}

async fn retry_watch_server(
    client: &kube::Client,
    ns: &str,
    workload_name: &str,
) -> tonic::Streaming<grpc::inbound::Server> {
    let mut policy_api = grpc::InboundPolicyClient::port_forwarded(client).await;
    loop {
        match policy_api
            .watch_port_for_external_workload(ns, workload_name, 80)
            .await
        {
            Ok(rx) => return rx,
            Err(error) => {
                tracing::error!(
                    ?error,
                    ns,
                    workload_name,
                    "failed to watch policy for port 80"
                );
                time::sleep(Duration::from_secs(1)).await;
            }
        }
    }
}

fn mk_http_server(ns: &str, name: &str) -> k8s::policy::Server {
    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            selector: k8s::policy::server::Selector::ExternalWorkload(
                k8s::labels::Selector::default(),
            ),
            port: k8s::policy::server::Port::Name("http".to_string()),
            proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
            access_policy: None,
        },
    }
}
