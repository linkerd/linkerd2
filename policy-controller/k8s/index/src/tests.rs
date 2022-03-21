use crate::{defaults::DefaultPolicy, index::*, server_authorization::ServerSelector, ClusterInfo};
use ahash::AHashMap as HashMap;
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, InboundServer, IpNet, Ipv4Net,
    Ipv6Net, NetworkMatch, ProxyProtocol,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    api::core::v1::ContainerPort,
    policy::{server::Port, TargetRef},
    ResourceExt,
};
use tokio::time;

#[test]
fn pod_must_exist_for_lookup() {
    let test = TestConfig::default();
    test.index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect_err("pod-0.ns-0 must not exist");
}

#[test]
fn links_named_server_port() {
    let test = TestConfig::default();

    let mut pod = mk_pod(
        "ns-0",
        "pod-0",
        Some((
            "container-0",
            Some(ContainerPort {
                name: Some("admin-http".to_string()),
                container_port: 8080,
                protocol: Some("TCP".to_string()),
                ..ContainerPort::default()
            }),
        )),
    );
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-admin-http",
        Port::Name("admin-http".to_string()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            name: "srv-admin-http".to_string(),
            authorizations: Default::default(),
            protocol: ProxyProtocol::Http1,
        },
    );
}

#[test]
fn links_unnamed_server_port() {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            name: "srv-8080".to_string(),
            authorizations: Default::default(),
            protocol: ProxyProtocol::Http1,
        },
    );
}

#[test]
fn links_server_authz_by_name() {
    link_server_authz(ServerSelector::Name("srv-8080".to_string()))
}

#[test]
fn links_server_authz_by_label() {
    link_server_authz(ServerSelector::Selector(
        Some(("app", "app-0")).into_iter().collect(),
    ));
}

#[inline]
fn link_server_authz(selector: ServerSelector) {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            name: "srv-8080".to_string(),
            authorizations: Default::default(),
            protocol: ProxyProtocol::Http1,
        },
    );
    test.index.write().apply(mk_server_authz(
        "ns-0",
        "authz-foo",
        selector,
        k8s::policy::server_authorization::Client {
            networks: Some(vec![k8s::policy::server_authorization::Network {
                cidr: "10.0.0.0/8".parse::<IpNet>().unwrap(),
                except: None,
            }]),
            unauthenticated: false,
            mesh_tls: Some(k8s::policy::server_authorization::MeshTls {
                identities: Some(vec!["foo.bar".to_string()]),
                ..Default::default()
            }),
        },
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(rx.borrow().name, "srv-8080");
    assert_eq!(rx.borrow().protocol, ProxyProtocol::Http1,);
    assert!(rx
        .borrow()
        .authorizations
        .contains_key("serverauthorization:authz-foo"));
}

#[test]
fn server_update_deselects_pod() {
    let test = TestConfig::default();

    test.index.write().reset(
        vec![mk_pod("ns-0", "pod-0", Some(("container-0", None)))],
        Default::default(),
    );

    let mut srv = mk_server(
        "ns-0",
        "srv-0",
        Port::Number(2222),
        None,
        None,
        Some(k8s::policy::server::ProxyProtocol::Http2),
    );
    test.index
        .write()
        .reset(vec![srv.clone()], Default::default());

    // The default policy applies for all ports.
    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 2222)
        .unwrap();
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            name: "srv-0".into(),
            protocol: ProxyProtocol::Http2,
            authorizations: Default::default(),
        }
    );

    test.index.write().apply({
        srv.spec.pod_selector = Some(("label", "value")).into_iter().collect();
        srv
    });
    assert!(rx.has_changed().unwrap());
    assert_eq!(*rx.borrow(), test.default_server());
}

/// Tests that pod servers are configured with defaults based on the
/// workload-defined `DefaultPolicy` policy.
///
/// Iterates through each default policy and validates that it produces expected
/// configurations.
#[test]
fn default_policy_annotated() {
    for default in &DEFAULTS {
        let test = TestConfig::from_default_policy(match *default {
            // Invert default to ensure override applies.
            DefaultPolicy::Deny => DefaultPolicy::Allow {
                authenticated_only: false,
                cluster_only: false,
            },
            _ => DefaultPolicy::Deny,
        });

        // Initially create the pod without an annotation and check that it gets
        // the global default.
        let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
        test.index
            .write()
            .reset(vec![pod.clone()], Default::default());

        let mut rx = test
            .index
            .write()
            .pod_server_rx("ns-0", "pod-0", 2222)
            .expect("pod-0.ns-0 should exist");
        assert_eq!(
            rx.borrow_and_update().name,
            format!("default:{}", test.default_policy)
        );

        // Update the annotation on the pod and check that the watch is updated
        // with the new default.
        pod.annotations_mut().insert(
            "config.linkerd.io/default-inbound-policy".into(),
            default.to_string(),
        );
        test.index.write().apply(pod);
        assert!(rx.has_changed().unwrap());
        assert_eq!(rx.borrow().name, format!("default:{}", default));
    }
}

/// Tests that an invalid workload annotation is ignored in favor of the global
/// default.
#[tokio::test]
async fn default_policy_annotated_invalid() {
    let test = TestConfig::default();

    let mut p = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    p.annotations_mut().insert(
        "config.linkerd.io/default-inbound-policy".into(),
        "bogus".into(),
    );
    test.index.write().reset(vec![p], Default::default());

    // Lookup port 2222 -> default config.
    let rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 2222)
        .expect("pod must exist in lookups");
    assert_eq!(*rx.borrow(), test.default_server());
}

#[test]
fn opaque_annotated() {
    for default in &DEFAULTS {
        let test = TestConfig::from_default_policy(*default);

        let mut p = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
        p.annotations_mut()
            .insert("config.linkerd.io/opaque-ports".into(), "2222".into());
        test.index.write().reset(vec![p], Default::default());

        let mut server = test.default_server();
        server.protocol = ProxyProtocol::Opaque;

        let rx = test
            .index
            .write()
            .pod_server_rx("ns-0", "pod-0", 2222)
            .expect("pod-0.ns-0 should exist");
        assert_eq!(*rx.borrow(), server);
    }
}

#[test]
fn authenticated_annotated() {
    for default in &DEFAULTS {
        let test = TestConfig::from_default_policy(*default);

        let mut p = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
        p.annotations_mut().insert(
            "config.linkerd.io/proxy-require-identity-inbound-ports".into(),
            "2222".into(),
        );
        test.index.write().reset(vec![p], Default::default());

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
                authorizations: mk_default_policy(policy, test.cluster.networks),
                protocol: ProxyProtocol::Detect {
                    timeout: test.detect_timeout,
                },
            }
        };

        let rx = test
            .index
            .write()
            .pod_server_rx("ns-0", "pod-0", 2222)
            .expect("pod-0.ns-0 should exist");
        assert_eq!(*rx.borrow(), config);
    }
}

#[test]
fn links_authorization_policy_with_mtls_name() {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            name: "srv-8080".to_string(),
            authorizations: Default::default(),
            protocol: ProxyProtocol::Http1,
        },
    );

    let authz = ClientAuthorization {
        networks: vec!["10.0.0.0/8".parse::<IpNet>().unwrap().into()],
        authentication: ClientAuthentication::TlsAuthenticated(vec![IdentityMatch::Exact(
            "foo.bar".to_string(),
        )]),
    };
    test.index.write().apply(mk_authorization_policy(
        "ns-0",
        "authz-foo",
        "srv-8080",
        vec![
            TargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "NetworkAuthentication".to_string(),
                namespace: None,
                name: "net-foo".to_string(),
            },
            TargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "MeshTLSAuthentication".to_string(),
                namespace: Some("ns-1".to_string()),
                name: "mtls-bar".to_string(),
            },
        ],
    ));
    test.index.write().apply(mk_network_authentication(
        "ns-0".to_string(),
        "net-foo".to_string(),
        vec![k8s::policy::network_authentication::Network {
            cidr: "10.0.0.0/8".parse().unwrap(),
            except: None,
        }],
    ));
    test.index.write().apply(mk_meshtls_authentication(
        "ns-1".to_string(),
        "mtls-bar".to_string(),
        Some("foo.bar".to_string()),
        None,
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow(),
        InboundServer {
            name: "srv-8080".to_string(),
            authorizations: Some(("authorizationpolicy:authz-foo".to_string(), authz))
                .into_iter()
                .collect(),
            protocol: ProxyProtocol::Http1,
        },
    );
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
    ns: impl ToString,
    name: impl ToString,
    containers: impl IntoIterator<Item = (impl ToString, impl IntoIterator<Item = ContainerPort>)>,
) -> k8s::Pod {
    k8s::Pod {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: Some(k8s::api::core::v1::PodSpec {
            containers: containers
                .into_iter()
                .map(|(name, ports)| k8s::api::core::v1::Container {
                    name: name.to_string(),
                    ports: Some(ports.into_iter().collect()),
                    ..Default::default()
                })
                .collect(),
            ..Default::default()
        }),
        ..k8s::Pod::default()
    }
}

fn mk_server(
    ns: impl ToString,
    name: impl ToString,
    port: Port,
    srv_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    pod_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    proxy_protocol: Option<k8s::policy::server::ProxyProtocol>,
) -> k8s::policy::Server {
    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
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
            proxy_protocol,
        },
    }
}

fn mk_server_authz(
    ns: impl ToString,
    name: impl ToString,
    selector: ServerSelector,
    client: k8s::policy::server_authorization::Client,
) -> k8s::policy::ServerAuthorization {
    k8s::policy::ServerAuthorization {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::ServerAuthorizationSpec {
            server: match selector {
                ServerSelector::Name(n) => k8s::policy::server_authorization::Server {
                    name: Some(n),
                    selector: None,
                },
                ServerSelector::Selector(s) => k8s::policy::server_authorization::Server {
                    selector: Some(s),
                    name: None,
                },
            },
            client,
        },
    }
}

fn mk_authorization_policy(
    ns: impl ToString,
    name: impl ToString,
    server: impl ToString,
    authns: impl IntoIterator<Item = TargetRef>,
) -> k8s::policy::AuthorizationPolicy {
    k8s::policy::AuthorizationPolicy {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::AuthorizationPolicySpec {
            target_ref: TargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "Server".to_string(),
                name: server.to_string(),
                namespace: None,
            },
            required_authentication_refs: authns.into_iter().collect(),
        },
    }
}

fn mk_meshtls_authentication(
    ns: impl ToString,
    name: impl ToString,
    identities: impl IntoIterator<Item = String>,
    refs: impl IntoIterator<Item = TargetRef>,
) -> k8s::policy::MeshTLSAuthentication {
    let identities = identities.into_iter().collect::<Vec<_>>();
    let identity_refs = refs.into_iter().collect::<Vec<_>>();
    k8s::policy::MeshTLSAuthentication {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::MeshTLSAuthenticationSpec {
            identities: if identities.is_empty() {
                None
            } else {
                Some(identities)
            },
            identity_refs: if identity_refs.is_empty() {
                None
            } else {
                Some(identity_refs)
            },
        },
    }
}

fn mk_network_authentication(
    ns: impl ToString,
    name: impl ToString,
    networks: impl IntoIterator<Item = k8s::policy::network_authentication::Network>,
) -> k8s::policy::NetworkAuthentication {
    k8s::policy::NetworkAuthentication {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::NetworkAuthenticationSpec {
            networks: networks.into_iter().collect(),
        },
    }
}

fn mk_default_policy(
    da: DefaultPolicy,
    cluster_nets: Vec<IpNet>,
) -> HashMap<String, ClientAuthorization> {
    let all_nets = vec![Ipv4Net::default().into(), Ipv6Net::default().into()];

    let cluster_nets = cluster_nets.into_iter().map(NetworkMatch::from).collect();

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

struct TestConfig {
    index: SharedIndex,
    detect_timeout: time::Duration,
    default_policy: DefaultPolicy,
    cluster: ClusterInfo,
    _tracing: tracing::subscriber::DefaultGuard,
}

impl TestConfig {
    fn from_default_policy(default_policy: DefaultPolicy) -> Self {
        let _tracing = Self::init_tracing();
        let cluster_net = "192.0.2.0/24".parse().unwrap();
        let detect_timeout = time::Duration::from_secs(1);
        let cluster = ClusterInfo {
            networks: vec![cluster_net],
            control_plane_ns: "linkerd".to_string(),
            identity_domain: "cluster.example.com".into(),
            default_policy,
            default_detect_timeout: detect_timeout,
        };
        let index = Index::shared(cluster.clone());
        Self {
            index,
            cluster,
            detect_timeout,
            default_policy,
            _tracing,
        }
    }

    fn default_server(&self) -> InboundServer {
        InboundServer {
            name: format!("default:{}", self.default_policy),
            authorizations: mk_default_policy(self.default_policy, self.cluster.networks.clone()),
            protocol: ProxyProtocol::Detect {
                timeout: self.detect_timeout,
            },
        }
    }

    fn init_tracing() -> tracing::subscriber::DefaultGuard {
        tracing::subscriber::set_default(
            tracing_subscriber::fmt()
                .with_test_writer()
                .with_max_level(tracing::Level::TRACE)
                .finish(),
        )
    }
}

impl Default for TestConfig {
    fn default() -> TestConfig {
        Self::from_default_policy(DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: true,
        })
    }
}
