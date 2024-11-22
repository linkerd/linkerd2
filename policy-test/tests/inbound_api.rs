use std::time::Duration;

use futures::prelude::*;
use k8s_openapi::chrono;
use kube::ResourceExt;
use linkerd_policy_controller_core::{Ipv4Net, Ipv6Net};
use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_test::{
    assert_default_all_unauthenticated_labels, assert_is_default_all_unauthenticated,
    assert_protocol_detect, await_condition, create, create_ready_pod, grpc, with_temp_ns,
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

        let mut rx = retry_watch_server(&client, &ns, &pod.name_unchecked()).await;
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
        let server = create(&client, mk_admin_server(&ns, "linkerd-admin", None)).await;
        let config = next_config(&mut rx).await;
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_no_ratelimit())
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

        // Create a server authorization that refers to the `linkerd-admin`
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
            Some(grpc::defaults::proxy_protocol_no_ratelimit())
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
        let config = next_config(&mut rx).await;
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

        let mut rx = retry_watch_server(&client, &ns, &pod.name_unchecked()).await;
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
        let server = create(&client, mk_admin_server(&ns, "linkerd-admin", None)).await;
        let config = next_config(&mut rx).await;
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_no_ratelimit())
        );
        assert_eq!(config.authorizations, vec![]);
        assert_eq!(
            config.labels,
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "server",
                "name" => server.name_unchecked()
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

        let config = time::timeout(time::Duration::from_secs(10), next_config(&mut rx))
            .await
            .expect("watch must update within 10s");

        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_no_ratelimit())
        );
        assert_eq!(config.authorizations.len(), 1);
        assert_eq!(
            config.authorizations.first().unwrap().labels,
            convert_args!(hashmap!(
                "group" => "policy.linkerd.io",
                "kind" => "authorizationpolicy",
                "name" => authz_policy.name_unchecked(),
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
                "name" => server.name_unchecked()
            ))
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn server_with_audit_policy() {
    with_temp_ns(|client, ns| async move {
        // Create a pod that does nothing. It's injected with a proxy, so we can
        // attach policies to its admin server.
        let pod = create_ready_pod(&client, mk_pause(&ns, "pause")).await;

        let mut rx = retry_watch_server(&client, &ns, &pod.name_unchecked()).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect!(config);

        // Create a server with audit access policy that selects the pod's proxy admin server and
        // ensure that the update now uses this server, and an unauthenticated authorization is
        // returned
        let server = create(
            &client,
            mk_admin_server(&ns, "linkerd-admin", Some("audit".to_string())),
        )
        .await;
        let config = next_config(&mut rx).await;
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_no_ratelimit())
        );
        assert_eq!(config.authorizations.len(), 1);
        assert_eq!(
            config.authorizations.first().unwrap().labels,
            convert_args!(hashmap!(
                "group" => "",
                "kind" => "default",
                "name" => "audit",
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
                "name" => server.name_unchecked()
            ))
        );
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn http_local_rate_limit_policy() {
    with_temp_ns(|client, ns| async move {
        // Create a pod that does nothing. It's injected with a proxy, so we can
        // attach policies to its admin server.
        let pod = create_ready_pod(&client, mk_pause(&ns, "pause")).await;

        // Create a server with audit access policy that selects the pod's proxy admin server
        let server = create(
            &client,
            mk_admin_server(&ns, "linkerd-admin", Some("audit".to_string())),
        )
        .await;

        // Create a rate-limit policy associated to the server
        let rate_limit = create(
            &client,
            k8s::policy::ratelimit_policy::HttpLocalRateLimitPolicy {
                metadata: k8s::ObjectMeta {
                    namespace: Some(ns.to_string()),
                    name: Some("rl-0".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::ratelimit_policy::RateLimitPolicySpec {
                    target_ref: k8s::policy::LocalTargetRef::from_resource(&server),
                    total: Some(k8s::policy::ratelimit_policy::Limit {
                        requests_per_second: 1000,
                    }),
                    identity: None,
                    overrides: Some(vec![k8s::policy::ratelimit_policy::Override {
                        requests_per_second: 200,
                        client_refs: vec![k8s::policy::NamespacedTargetRef {
                            group: None,
                            kind: "ServiceAccount".to_string(),
                            name: "sa-0".to_string(),
                            namespace: None,
                        }],
                    }]),
                },
                status: None,
            },
        )
        .await;

        await_condition(
            &client,
            &ns,
            &rate_limit.name_unchecked(),
            |obj: Option<&k8s::policy::ratelimit_policy::HttpLocalRateLimitPolicy>| {
                obj.as_ref().map_or(false, |obj| {
                    obj.status.as_ref().map_or(false, |status| {
                        status
                            .conditions
                            .iter()
                            .any(|c| c.type_ == "Accepted" && c.status == "True")
                    })
                })
            },
        )
        .await
        .expect("rate limit must get a status");

        let client_id = format!("sa-0.{}.serviceaccount.identity.linkerd.cluster.local", ns);
        let ratelimit_overrides = vec![(200, vec![client_id])];
        let ratelimit =
            grpc::defaults::http_local_ratelimit("rl-0", Some(1000), None, ratelimit_overrides);
        let protocol = grpc::defaults::proxy_protocol(Some(ratelimit));

        let mut rx = retry_watch_server(&client, &ns, &pod.name_unchecked()).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_eq!(config.protocol, Some(protocol));
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn server_with_http_route() {
    with_temp_ns(|client, ns| async move {
        // Create a pod that does nothing. It's injected with a proxy, so we can
        // attach policies to its admin server.
        let pod = create_ready_pod(&client, mk_pause(&ns, "pause")).await;

        let mut rx = retry_watch_server(&client, &ns, &pod.name_unchecked()).await;
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
        // and no routes.
        let _server = create(&client, mk_admin_server(&ns, "linkerd-admin", None)).await;
        let config = next_config(&mut rx).await;
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_no_ratelimit())
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

        // Create an http route that refers to the `linkerd-admin` server (by
        // name) and ensure that the update now reflects this route.
        let route = create(&client, mk_admin_route(ns.as_ref(), "metrics-route")).await;
        let config = next_config(&mut rx).await;
        let h1_route = http1_routes(&config).first().expect("must have route");
        let rule_match = h1_route
            .rules
            .first()
            .expect("must have rule")
            .matches
            .first()
            .expect("must have match");
        // Route has no authorizations by default.
        assert_eq!(h1_route.authorizations, Vec::default());
        // Route has appropriate metadata.
        assert_eq!(
            h1_route
                .metadata
                .to_owned()
                .expect("route must have metadata"),
            grpc::meta::Metadata {
                kind: Some(grpc::meta::metadata::Kind::Resource(grpc::meta::Resource {
                    group: "policy.linkerd.io".to_string(),
                    kind: "HTTPRoute".to_string(),
                    name: "metrics-route".to_string(),
                    ..Default::default()
                }))
            }
        );
        // Route has path match.
        assert_eq!(
            rule_match
                .path
                .to_owned()
                .expect("must have path match")
                .kind
                .expect("must have kind"),
            grpc::http_route::path_match::Kind::Exact("/metrics".to_string()),
        );

        // Create a network authentication and an authorization policy that
        // refers to the `metrics-route` route (by name).
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
        create(
            &client,
            k8s::policy::AuthorizationPolicy {
                metadata: kube::api::ObjectMeta {
                    namespace: Some(ns.clone()),
                    name: Some("all-admin".to_string()),
                    ..Default::default()
                },
                spec: k8s::policy::AuthorizationPolicySpec {
                    target_ref: k8s::policy::LocalTargetRef::from_resource(&route),
                    required_authentication_refs: vec![
                        k8s::policy::NamespacedTargetRef::from_resource(&all_nets),
                    ],
                },
            },
        )
        .await;

        let config = next_config(&mut rx).await;
        let http1 = if let grpc::inbound::proxy_protocol::Kind::Http1(http1) = config
            .protocol
            .expect("must have proxy protocol")
            .kind
            .expect("must have kind")
        {
            http1
        } else {
            panic!("proxy protocol must be HTTP1")
        };
        let h1_route = http1.routes.first().expect("must have route");
        assert_eq!(h1_route.authorizations.len(), 1, "must have authorizations");

        // Delete the `HttpRoute` and ensure that the update reverts to the
        // default.
        kube::Api::<k8s::policy::HttpRoute>::namespaced(client.clone(), &ns)
            .delete("metrics-route", &kube::api::DeleteParams::default())
            .await
            .expect("HttpRoute must be deleted");
        let config = next_config(&mut rx).await;
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_no_ratelimit())
        );
    })
    .await
}

#[tokio::test(flavor = "current_thread")]
async fn http_routes_ordered_by_creation() {
    fn route_path(routes: &[grpc::inbound::HttpRoute], idx: usize) -> Option<&str> {
        use grpc::http_route::path_match;
        match routes
            .get(idx)?
            .rules
            .first()?
            .matches
            .first()?
            .path
            .as_ref()?
            .kind
            .as_ref()?
        {
            path_match::Kind::Exact(ref path) => Some(path.as_ref()),
            x => panic!("unexpected route match {x:?}",),
        }
    }

    with_temp_ns(|client, ns| async move {
        // Create a pod that does nothing. It's injected with a proxy, so we can
        // attach policies to its admin server.
        let pod = create_ready_pod(&client, mk_pause(&ns, "pause")).await;

        let mut rx = retry_watch_server(&client, &ns, &pod.name_unchecked()).await;
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
        // and no routes.
        let _server = create(&client, mk_admin_server(&ns, "linkerd-admin", None)).await;
        let config = next_config(&mut rx).await;
        assert_eq!(
            config.protocol,
            Some(grpc::defaults::proxy_protocol_no_ratelimit())
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

        // Create several HTTPRoute resources referencing the admin server by
        // name. These should be ordered by creation time. A different path
        // match is used for each route so that they can be distinguished to
        // ensure ordering.
        create(
            &client,
            mk_admin_route_with_path(ns.as_ref(), "d", "/metrics"),
        )
        .await;
        next_config(&mut rx).await;

        // Creation timestamps in Kubernetes only have second precision, so we
        // must wait a whole second between creating each of these routes in
        // order for them to have different creation timestamps.
        tokio::time::sleep(Duration::from_secs(1)).await;
        create(
            &client,
            mk_admin_route_with_path(ns.as_ref(), "a", "/ready"),
        )
        .await;
        next_config(&mut rx).await;

        tokio::time::sleep(Duration::from_secs(1)).await;
        create(
            &client,
            mk_admin_route_with_path(ns.as_ref(), "c", "/shutdown"),
        )
        .await;
        next_config(&mut rx).await;

        tokio::time::sleep(Duration::from_secs(1)).await;
        create(
            &client,
            mk_admin_route_with_path(ns.as_ref(), "b", "/proxy-log-level"),
        )
        .await;
        let config = next_config(&mut rx).await;
        let routes = http1_routes(&config);

        assert_eq!(routes.len(), 4);
        assert_eq!(route_path(routes, 0), Some("/metrics"));
        assert_eq!(route_path(routes, 1), Some("/ready"));
        assert_eq!(route_path(routes, 2), Some("/shutdown"));
        assert_eq!(route_path(routes, 3), Some("/proxy-log-level"));

        // Delete one of the routes and ensure that the update maintains the
        // same ordering.
        kube::Api::<k8s::policy::HttpRoute>::namespaced(client.clone(), &ns)
            .delete("c", &kube::api::DeleteParams::default())
            .await
            .expect("HttpRoute must be deleted");
        let config = next_config(&mut rx).await;
        let routes = http1_routes(&config);
        assert_eq!(routes.len(), 3);
        assert_eq!(route_path(routes, 0), Some("/metrics"));
        assert_eq!(route_path(routes, 1), Some("/ready"));
        assert_eq!(route_path(routes, 2), Some("/proxy-log-level"));
    })
    .await
}

#[tokio::test(flavor = "current_thread")]
async fn default_http_routes() {
    with_temp_ns(|client, ns| async move {
        // Create a pod that does nothing. It's injected with a proxy, so we can
        // attach policies to its admin server.
        let pod = create_ready_pod(&client, mk_pause(&ns, "pause")).await;

        let mut rx = retry_watch_server(&client, &ns, &pod.name_unchecked()).await;
        let config = rx
            .next()
            .await
            .expect("watch must not fail")
            .expect("watch must return an initial config");
        tracing::trace!(?config);
        assert_is_default_all_unauthenticated!(config);
        assert_protocol_detect!(config);

        let routes = detect_routes(&config);
        assert_eq!(routes.len(), 2);
        let route_authzs = &routes[0].authorizations;
        assert_eq!(route_authzs.len(), 0);
    })
    .await
}

/// Returns an `HttpRoute` resource in the provdied namespace and with the
/// provided name, which attaches to the `linkerd-admin` `Server` resource and
/// matches `GET` requests with the path `/metrics`.
fn mk_admin_route(ns: &str, name: &str) -> k8s::policy::HttpRoute {
    use k8s::policy::httproute as api;
    api::HttpRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: api::HttpRouteSpec {
            inner: api::CommonRouteSpec {
                parent_refs: Some(vec![api::ParentReference {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: Some("Server".to_string()),
                    namespace: None,
                    name: "linkerd-admin".to_string(),
                    section_name: None,
                    port: None,
                }]),
            },
            hostnames: None,
            rules: Some(vec![api::HttpRouteRule {
                matches: Some(vec![api::HttpRouteMatch {
                    path: Some(api::HttpPathMatch::Exact {
                        value: "/metrics".to_string(),
                    }),
                    headers: None,
                    query_params: None,
                    method: Some("GET".to_string()),
                }]),
                filters: None,
                backend_refs: None,
                timeouts: None,
            }]),
        },
        status: None,
    }
}

/// Returns an `HttpRoute` resource in the provdied namespace and with the
/// provided name, which attaches to the `linkerd-admin` `Server` resource and
/// matches `GET` requests with the provided path.
fn mk_admin_route_with_path(ns: &str, name: &str, path: &str) -> k8s::policy::HttpRoute {
    use k8s::policy::httproute as api;
    api::HttpRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: api::HttpRouteSpec {
            inner: api::CommonRouteSpec {
                parent_refs: Some(vec![api::ParentReference {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: Some("Server".to_string()),
                    namespace: None,
                    name: "linkerd-admin".to_string(),
                    section_name: None,
                    port: None,
                }]),
            },
            hostnames: None,
            rules: Some(vec![api::HttpRouteRule {
                matches: Some(vec![api::HttpRouteMatch {
                    path: Some(api::HttpPathMatch::Exact {
                        value: path.to_string(),
                    }),
                    headers: None,
                    query_params: None,
                    method: Some("GET".to_string()),
                }]),
                filters: None,
                backend_refs: None,
                timeouts: None,
            }]),
        },
        status: None,
    }
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

fn mk_admin_server(ns: &str, name: &str, access_policy: Option<String>) -> k8s::policy::Server {
    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            selector: k8s::policy::server::Selector::Pod(k8s::labels::Selector::default()),
            port: k8s::policy::server::Port::Number(4191.try_into().unwrap()),
            proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
            access_policy,
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
    let mut policy_api = grpc::InboundPolicyClient::port_forwarded(client).await;
    loop {
        match policy_api.watch_port(ns, pod_name, 4191).await {
            Ok(rx) => return rx,
            Err(error) => {
                tracing::error!(?error, ns, pod_name, "failed to watch policy for port 4191");
                time::sleep(Duration::from_secs(1)).await;
            }
        }
    }
}

async fn next_config(rx: &mut tonic::Streaming<grpc::inbound::Server>) -> grpc::inbound::Server {
    let config = rx
        .next()
        .await
        .expect("watch must not fail")
        .expect("watch must return an updated config");
    tracing::trace!(config = ?format_args!("{:#?}", config));
    config
}

fn detect_routes(config: &grpc::inbound::Server) -> &[grpc::inbound::HttpRoute] {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    let detect = if let grpc::inbound::proxy_protocol::Kind::Detect(ref detect) = kind {
        detect
    } else {
        panic!("proxy protocol must be Detect; actually got:\n{kind:#?}")
    };
    &detect.http_routes[..]
}

fn http1_routes(config: &grpc::inbound::Server) -> &[grpc::inbound::HttpRoute] {
    let kind = config
        .protocol
        .as_ref()
        .expect("must have proxy protocol")
        .kind
        .as_ref()
        .expect("must have kind");
    let http1 = if let grpc::inbound::proxy_protocol::Kind::Http1(ref http1) = kind {
        http1
    } else {
        panic!("proxy protocol must be HTTP1; actually got:\n{kind:#?}")
    };
    &http1.routes[..]
}
