use super::*;
use ahash::AHashMap as HashMap;
use futures::prelude::*;
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, IpNet, Ipv4Net, Ipv6Net,
    NetworkMatch, ProxyProtocol,
};
use linkerd_policy_controller_k8s_api::{policy::server::Port, ResourceExt};
use std::{net::IpAddr, str::FromStr};
use tokio::time;

/// Creates a pod, then a server, then an authorization--then deletes these resources in the reverse
/// order--checking the server watch is updated at each step.
#[tokio::test]
async fn incrementally_configure_server() {
    let cluster_net = IpNet::from_str("192.0.2.0/24").unwrap();
    let cluster = ClusterInfo {
        networks: vec![cluster_net],
        control_plane_ns: "linkerd".to_string(),
        identity_domain: "cluster.example.com".into(),
    };
    let pod_net = IpNet::from_str("192.0.2.2/28").unwrap();
    let detect_timeout = time::Duration::from_secs(1);
    let (lookup_rx, idx) = Index::new(
        cluster,
        DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: true,
        },
        detect_timeout,
    );

    let pod = mk_pod(
        "ns-0",
        "pod-0",
        "node-0",
        pod_net.hosts().next().unwrap(),
        Some(("container-0", vec![2222, 9999])),
    );
    idx.write().apply_pod(pod.clone());

    let default = DefaultPolicy::Allow {
        authenticated_only: false,
        cluster_only: true,
    };
    let default_config = InboundServer {
        name: format!("default:{}", default),
        authorizations: mk_default_policy(default, cluster_net),
        protocol: ProxyProtocol::Detect {
            timeout: detect_timeout,
        },
    };

    // A port that's not exposed by the pod is not found.
    assert!(lookup_rx.lookup("ns-0", "pod-0", 7000).is_none());

    // The default policy applies for all exposed ports.
    let port2222 = lookup_rx.lookup("ns-0", "pod-0", 2222).unwrap();
    assert_eq!(port2222.get(), default_config);

    // In fact, both port resolutions should point to the same data structures (rather than being
    // duplicated for each pod).
    let port9999 = lookup_rx.lookup("ns-0", "pod-0", 9999).unwrap();
    assert_eq!(port9999.get(), default_config);

    // Update the server on port 2222 to have a configured protocol.
    let srv = {
        let mut srv = mk_server("ns-0", "srv-0", Port::Number(2222), None, None);
        srv.spec.proxy_protocol = Some(k8s::policy::server::ProxyProtocol::Http1);
        srv
    };
    idx.write().apply_server(srv.clone());

    // Check that the watch has been updated to reflect the above change and that this change _only_
    // applies to the correct port.
    let basic_config = InboundServer {
        name: "srv-0".into(),
        protocol: ProxyProtocol::Http1,
        authorizations: Default::default(),
    };
    assert_eq!(port2222.get(), basic_config);
    assert_eq!(port9999.get(), default_config);

    // Add an authorization policy that selects the server by name.
    let authz = mk_authz(
        "ns-0",
        "authz-0",
        "srv-0",
        k8s::policy::authz::Client {
            mesh_tls: Some(k8s::policy::authz::MeshTls {
                unauthenticated_tls: true,
                ..Default::default()
            }),
            ..Default::default()
        },
    );
    idx.write().apply_serverauthorization(authz.clone());

    // Check that the watch now has authorized traffic as described above.
    let mut rx = port2222.into_stream();
    assert_eq!(
        time::timeout(time::Duration::from_secs(1), rx.next()).await,
        Ok(Some(InboundServer {
            name: "srv-0".into(),
            protocol: ProxyProtocol::Http1,
            authorizations: vec![(
                "authz-0".into(),
                ClientAuthorization {
                    authentication: ClientAuthentication::TlsUnauthenticated,
                    networks: vec!["192.0.2.0/24".parse::<IpNet>().unwrap().into()]
                }
            ),]
            .into_iter()
            .collect(),
        }))
    );

    // Delete the authorization and check that the watch has reverted to its prior state.
    idx.write().delete_serverauthorization(authz);
    assert_eq!(
        time::timeout(time::Duration::from_secs(1), rx.next()).await,
        Ok(Some(basic_config)),
    );

    // Delete the server and check that the watch has reverted the default state.
    idx.write().delete_server(srv);
    assert_eq!(
        time::timeout(time::Duration::from_secs(1), rx.next()).await,
        Ok(Some(default_config))
    );

    // Delete the pod and check that the watch recognizes that the watch has been closed.
    idx.write().delete_pod(pod);
    assert_eq!(
        time::timeout(time::Duration::from_secs(1), rx.next()).await,
        Ok(None)
    );
}

#[test]
fn server_update_deselects_pod() {
    let cluster_net = IpNet::from_str("192.0.2.0/24").unwrap();
    let cluster = ClusterInfo {
        networks: vec![cluster_net],
        control_plane_ns: "linkerd".to_string(),
        identity_domain: "cluster.example.com".into(),
    };
    let pod_net = IpNet::from_str("192.0.2.2/28").unwrap();
    let detect_timeout = time::Duration::from_secs(1);
    let default = DefaultPolicy::Allow {
        authenticated_only: false,
        cluster_only: true,
    };
    let (lookup_rx, idx) = Index::new(cluster, default, detect_timeout);

    let p = mk_pod(
        "ns-0",
        "pod-0",
        "node-0",
        pod_net.hosts().next().unwrap(),
        Some(("container-0", vec![2222])),
    );
    idx.write().apply_pod(p);

    let srv = {
        let mut srv = mk_server("ns-0", "srv-0", Port::Number(2222), None, None);
        srv.spec.proxy_protocol = Some(k8s::policy::server::ProxyProtocol::Http2);
        srv
    };
    idx.write().apply_server(srv.clone());

    // The default policy applies for all exposed ports.
    let port2222 = lookup_rx.lookup("ns-0", "pod-0", 2222).unwrap();
    assert_eq!(
        port2222.get(),
        InboundServer {
            name: "srv-0".into(),
            protocol: ProxyProtocol::Http2,
            authorizations: Default::default(),
        }
    );

    idx.write().apply_server({
        let mut srv = srv;
        srv.spec.pod_selector = Some(("label", "value")).into_iter().collect();
        srv
    });
    assert_eq!(
        port2222.get(),
        InboundServer {
            name: format!("default:{}", default),
            authorizations: mk_default_policy(default, cluster_net),
            protocol: ProxyProtocol::Detect {
                timeout: detect_timeout,
            },
        }
    );
}

/// Tests that pod servers are configured with defaults based on the global `DefaultPolicy` policy.
///
/// Iterates through each default policy and validates that it produces expected configurations.
#[test]
fn default_policy_global() {
    let cluster_net = IpNet::from_str("192.0.2.0/24").unwrap();
    let cluster = ClusterInfo {
        networks: vec![cluster_net],
        control_plane_ns: "linkerd".to_string(),
        identity_domain: "cluster.example.com".into(),
    };
    let pod_net = IpNet::from_str("192.0.2.2/28").unwrap();
    let detect_timeout = time::Duration::from_secs(1);

    for default in &DEFAULTS {
        let (lookup_rx, idx) = Index::new(cluster.clone(), *default, detect_timeout);

        let p = mk_pod(
            "ns-0",
            "pod-0",
            "node-0",
            pod_net.hosts().next().unwrap(),
            Some(("container-0", vec![2222])),
        );
        idx.write().reset_pods(vec![p]);

        let config = InboundServer {
            name: format!("default:{}", default),
            authorizations: mk_default_policy(*default, cluster_net),
            protocol: ProxyProtocol::Detect {
                timeout: detect_timeout,
            },
        };

        // Lookup port 2222 -> default config.
        let port2222 = lookup_rx
            .lookup("ns-0", "pod-0", 2222)
            .expect("pod must exist in lookups");
        assert_eq!(port2222.get(), config);
    }
}

/// Tests that pod servers are configured with defaults based on the workload-defined `DefaultPolicy`
/// policy.
///
/// Iterates through each default policy and validates that it produces expected configurations.
#[test]
fn default_policy_annotated() {
    let cluster_net = IpNet::from_str("192.0.2.0/24").unwrap();
    let cluster = ClusterInfo {
        networks: vec![cluster_net],
        control_plane_ns: "linkerd".to_string(),
        identity_domain: "cluster.example.com".into(),
    };
    let pod_net = IpNet::from_str("192.0.2.2/28").unwrap();
    let detect_timeout = time::Duration::from_secs(1);

    for default in &DEFAULTS {
        let (lookup_rx, idx) = Index::new(
            cluster.clone(),
            // Invert default to ensure override applies.
            match *default {
                DefaultPolicy::Deny => DefaultPolicy::Allow {
                    authenticated_only: false,
                    cluster_only: false,
                },
                _ => DefaultPolicy::Deny,
            },
            detect_timeout,
        );

        let mut p = mk_pod(
            "ns-0",
            "pod-0",
            "node-0",
            pod_net.hosts().next().unwrap(),
            Some(("container-0", vec![2222])),
        );
        p.annotations_mut()
            .insert(DefaultPolicy::ANNOTATION.into(), default.to_string());
        idx.write().reset_pods(vec![p]);

        let config = InboundServer {
            name: format!("default:{}", default),
            authorizations: mk_default_policy(*default, cluster_net),
            protocol: ProxyProtocol::Detect {
                timeout: detect_timeout,
            },
        };

        let port2222 = lookup_rx
            .lookup("ns-0", "pod-0", 2222)
            .expect("pod must exist in lookups");
        assert_eq!(port2222.get(), config);
    }
}
/// Tests that an invalid workload annotation is ignored in favor of the global default.
#[test]
fn default_policy_annotated_invalid() {
    let cluster_net = IpNet::from_str("192.0.2.0/24").unwrap();
    let cluster = ClusterInfo {
        networks: vec![cluster_net],
        control_plane_ns: "linkerd".to_string(),
        identity_domain: "cluster.example.com".into(),
    };
    let pod_net = IpNet::from_str("192.0.2.2/28").unwrap();
    let detect_timeout = time::Duration::from_secs(1);

    let default = DefaultPolicy::Allow {
        authenticated_only: false,
        cluster_only: false,
    };
    let (lookup_rx, idx) = Index::new(cluster, default, detect_timeout);

    let mut p = mk_pod(
        "ns-0",
        "pod-0",
        "node-0",
        pod_net.hosts().next().unwrap(),
        Some(("container-0", vec![2222])),
    );
    p.annotations_mut()
        .insert(DefaultPolicy::ANNOTATION.into(), "bogus".into());
    idx.write().reset_pods(vec![p]);

    // Lookup port 2222 -> default config.
    let port2222 = lookup_rx
        .lookup("ns-0", "pod-0", 2222)
        .expect("pod must exist in lookups");
    assert_eq!(
        port2222.get(),
        InboundServer {
            name: format!("default:{}", default),
            authorizations: mk_default_policy(
                DefaultPolicy::Allow {
                    authenticated_only: false,
                    cluster_only: false,
                },
                cluster_net,
            ),
            protocol: ProxyProtocol::Detect {
                timeout: detect_timeout,
            },
        }
    );
}

#[test]
fn opaque_annotated() {
    let cluster_net = IpNet::from_str("192.0.2.0/24").unwrap();
    let cluster = ClusterInfo {
        networks: vec![cluster_net],
        control_plane_ns: "linkerd".to_string(),
        identity_domain: "cluster.example.com".into(),
    };
    let pod_net = IpNet::from_str("192.0.2.2/28").unwrap();
    let detect_timeout = time::Duration::from_secs(1);

    for default in &DEFAULTS {
        let (lookup_rx, idx) = Index::new(cluster.clone(), *default, detect_timeout);

        let mut p = mk_pod(
            "ns-0",
            "pod-0",
            "node-0",
            pod_net.hosts().next().unwrap(),
            Some(("container-0", vec![2222])),
        );
        p.annotations_mut()
            .insert("config.linkerd.io/opaque-ports".into(), "2222".into());
        idx.write().reset_pods(vec![p]);

        let config = InboundServer {
            name: format!("default:{}", default),
            authorizations: mk_default_policy(*default, cluster_net),
            protocol: ProxyProtocol::Opaque,
        };

        let port2222 = lookup_rx
            .lookup("ns-0", "pod-0", 2222)
            .expect("pod must exist in lookups");
        assert_eq!(port2222.get(), config);
    }
}

#[test]
fn authenticated_annotated() {
    let cluster_net = IpNet::from_str("192.0.2.0/24").unwrap();
    let cluster = ClusterInfo {
        networks: vec![cluster_net],
        control_plane_ns: "linkerd".to_string(),
        identity_domain: "cluster.example.com".into(),
    };
    let pod_net = IpNet::from_str("192.0.2.2/28").unwrap();
    let detect_timeout = time::Duration::from_secs(1);

    for default in &DEFAULTS {
        let (lookup_rx, idx) = Index::new(cluster.clone(), *default, detect_timeout);

        let mut p = mk_pod(
            "ns-0",
            "pod-0",
            "node-0",
            pod_net.hosts().next().unwrap(),
            Some(("container-0", vec![2222])),
        );
        p.annotations_mut().insert(
            "config.linkerd.io/proxy-require-identity-inbound-ports".into(),
            "2222".into(),
        );
        idx.write().reset_pods(vec![p]);

        let config = {
            let policy = match *default {
                DefaultPolicy::Allow { cluster_only, .. } => DefaultPolicy::Allow {
                    cluster_only,
                    authenticated_only: true,
                },
                DefaultPolicy::Deny => DefaultPolicy::Deny,
            };
            InboundServer {
                name: format!("default:{}", policy),
                authorizations: mk_default_policy(policy, cluster_net),
                protocol: ProxyProtocol::Detect {
                    timeout: detect_timeout,
                },
            }
        };

        let port2222 = lookup_rx
            .lookup("ns-0", "pod-0", 2222)
            .expect("pod must exist in lookups");
        assert_eq!(port2222.get().protocol, config.protocol);
        assert_eq!(port2222.get().authorizations, config.authorizations);
    }
}

// === Helpers ===

const DEFAULTS: [DefaultPolicy; 5] = [
    DefaultPolicy::Deny,
    DefaultPolicy::Allow {
        authenticated_only: true,
        cluster_only: false,
    },
    DefaultPolicy::Allow {
        authenticated_only: false,
        cluster_only: false,
    },
    DefaultPolicy::Allow {
        authenticated_only: true,
        cluster_only: true,
    },
    DefaultPolicy::Allow {
        authenticated_only: false,
        cluster_only: true,
    },
];

fn mk_pod(
    ns: impl Into<String>,
    name: impl Into<String>,
    node: impl Into<String>,
    pod_ip: IpAddr,
    containers: impl IntoIterator<Item = (impl Into<String>, impl IntoIterator<Item = u16>)>,
) -> k8s::Pod {
    k8s::Pod {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.into()),
            name: Some(name.into()),
            ..Default::default()
        },
        spec: Some(k8s::api::core::v1::PodSpec {
            node_name: Some(node.into()),
            containers: containers
                .into_iter()
                .map(|(name, ports)| k8s::api::core::v1::Container {
                    name: name.into(),
                    ports: Some(
                        ports
                            .into_iter()
                            .map(|p| k8s::api::core::v1::ContainerPort {
                                container_port: p as i32,
                                ..Default::default()
                            })
                            .collect(),
                    ),
                    ..Default::default()
                })
                .collect(),
            ..Default::default()
        }),
        status: Some(k8s::api::core::v1::PodStatus {
            pod_ips: Some(vec![k8s::api::core::v1::PodIP {
                ip: Some(pod_ip.to_string()),
            }]),
            ..Default::default()
        }),
    }
}

fn mk_server(
    ns: impl Into<String>,
    name: impl Into<String>,
    port: Port,
    srv_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    pod_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
) -> k8s::policy::Server {
    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.into()),
            name: Some(name.into()),
            labels: Some(
                srv_labels
                    .into_iter()
                    .map(|(k, v)| (k.to_string(), v.to_string()))
                    .collect(),
            ),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            port,
            pod_selector: pod_labels.into_iter().collect(),
            proxy_protocol: None,
        },
    }
}

fn mk_authz(
    ns: impl Into<String>,
    name: impl Into<String>,
    server: impl Into<String>,
    client: k8s::policy::authz::Client,
) -> k8s::policy::ServerAuthorization {
    k8s::policy::ServerAuthorization {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.into()),
            name: Some(name.into()),
            ..Default::default()
        },
        spec: k8s::policy::ServerAuthorizationSpec {
            server: k8s::policy::authz::Server {
                name: Some(server.into()),
                selector: None,
            },
            client,
        },
    }
}

fn mk_default_policy(
    da: DefaultPolicy,
    cluster_net: IpNet,
) -> HashMap<String, ClientAuthorization> {
    let all_nets = vec![Ipv4Net::default().into(), Ipv6Net::default().into()];

    let cluster_nets = vec![NetworkMatch::from(cluster_net)];

    let authed = ClientAuthentication::TlsAuthenticated(vec![IdentityMatch::Suffix(vec![])]);

    match da {
        DefaultPolicy::Deny => None,
        DefaultPolicy::Allow {
            authenticated_only: true,
            cluster_only: false,
        } => Some((
            "default:all-authenticated".into(),
            ClientAuthorization {
                authentication: authed,
                networks: all_nets,
            },
        )),
        DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: false,
        } => Some((
            "default:all-unauthenticated".into(),
            ClientAuthorization {
                authentication: ClientAuthentication::Unauthenticated,
                networks: all_nets,
            },
        )),
        DefaultPolicy::Allow {
            authenticated_only: true,
            cluster_only: true,
        } => Some((
            "default:cluster-authenticated".into(),
            ClientAuthorization {
                authentication: authed,
                networks: cluster_nets,
            },
        )),
        DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: true,
        } => Some((
            "default:cluster-unauthenticated".into(),
            ClientAuthorization {
                authentication: ClientAuthentication::Unauthenticated,
                networks: cluster_nets,
            },
        )),
    }
    .into_iter()
    .collect()
}
