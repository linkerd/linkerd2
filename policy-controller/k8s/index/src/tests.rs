use crate::{defaults::DefaultPolicy, index::*, ClusterInfo};
use ahash::AHashMap as HashMap;
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, InboundServer, IpNet, Ipv4Net,
    Ipv6Net, NetworkMatch, ProxyProtocol,
};
use linkerd_policy_controller_k8s_api::{policy::server::Port, Labels};
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

    let mut ports = HashMap::with_capacity(1);
    ports.insert("admin-http".to_string(), Some(8080).into_iter().collect());
    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            ports,
            PodSettings::default(),
        )
        .expect("pod-0.ns-0 should apply");

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply_server(
        "ns-0".to_string(),
        "srv-admin-http".to_string(),
        Default::default(),
        Some(("app", "app-0")).into_iter().collect(),
        Port::Name("admin-http".to_string()),
        Some(ProxyProtocol::Http1),
    );
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

    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings::default(),
        )
        .expect("pod-0.ns-0 should apply");

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply_server(
        "ns-0".to_string(),
        "srv-8080".to_string(),
        Default::default(),
        Some(("app", "app-0")).into_iter().collect(),
        Port::Number(8080),
        Some(ProxyProtocol::Http1),
    );
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
fn links_server_authorization_by_name() {
    let test = TestConfig::default();

    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings::default(),
        )
        .expect("pod-0.ns-0 should not already exist");

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply_server(
        "ns-0".to_string(),
        "srv-8080".to_string(),
        Default::default(),
        Some(("app", "app-0")).into_iter().collect(),
        Port::Number(8080),
        Some(ProxyProtocol::Http1),
    );
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
    test.index.write().apply_server_authorization(
        "ns-0".to_string(),
        "authz-foo".to_string(),
        ServerSelector::Name("srv-8080".to_string()),
        authz.clone(),
    );
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow(),
        InboundServer {
            name: "srv-8080".to_string(),
            authorizations: Some(("serverauthorization:authz-foo".to_string(), authz))
                .into_iter()
                .collect(),
            protocol: ProxyProtocol::Http1,
        },
    );
}

#[test]
fn links_server_authorization_by_label() {
    let test = TestConfig::default();

    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings::default(),
        )
        .expect("pod-0.ns-0 should not already exist");

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply_server(
        "ns-0".to_string(),
        "srv-8080".to_string(),
        Some(("app", "app-0")).into_iter().collect(),
        Some(("app", "app-0")).into_iter().collect(),
        Port::Number(8080),
        Some(ProxyProtocol::Http1),
    );
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
    test.index.write().apply_server_authorization(
        "ns-0".to_string(),
        "authz-foo".to_string(),
        ServerSelector::Selector(Some(("app", "app-0")).into_iter().collect()),
        authz.clone(),
    );
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow(),
        InboundServer {
            name: "srv-8080".to_string(),
            authorizations: Some(("serverauthorization:authz-foo".to_string(), authz))
                .into_iter()
                .collect(),
            protocol: ProxyProtocol::Http1,
        },
    );
}

#[test]
fn updates_default_server() {
    let test = TestConfig::default();

    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings::default(),
        )
        .expect("pod should apply");

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings {
                default_policy: Some(DefaultPolicy::Deny),
                ..Default::default()
            },
        )
        .expect("port names are not changing");
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            name: "default:deny".to_string(),
            protocol: ProxyProtocol::Detect {
                timeout: time::Duration::from_secs(1),
            },
            authorizations: Default::default()
        }
    );
}

#[test]
fn server_update_deselects_pod() {
    let test = TestConfig::default();

    // Start with a pod selected by a server.
    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings::default(),
        )
        .expect("pod should apply");
    test.index.write().apply_server(
        "ns-0".to_string(),
        "srv-8080".to_string(),
        Labels::default(),
        Some(("app", "app-0")).into_iter().collect(),
        Port::Number(8080),
        Some(ProxyProtocol::Http1),
    );
    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(rx.borrow_and_update().name, "srv-8080");

    // Update the server to no longer select the pod.
    test.index.write().apply_server(
        "ns-0".to_string(),
        "srv-8080".to_string(),
        Labels::default(),
        Some(("app", "app-1")).into_iter().collect(),
        Port::Number(8080),
        Some(ProxyProtocol::Http1),
    );

    // Ensure the server reverts to the default.
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        rx.borrow_and_update().name,
        "default:cluster-unauthenticated"
    );
}

/// Tests that pod servers are configured with defaults based on the workload-defined `DefaultPolicy`
/// policy.
///
/// Iterates through each default policy and validates that it produces expected configurations.
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

        // Start with a pod using the cluster default policy.
        test.index
            .write()
            .apply_pod(
                "ns-0".to_string(),
                "pod-0".to_string(),
                Some(("app", "app-0")).into_iter().collect(),
                HashMap::default(),
                PodSettings::default(),
            )
            .expect("pod should apply");
        let mut rx = test
            .index
            .write()
            .pod_server_rx("ns-0", "pod-0", 8080)
            .expect("pod-0.ns-0 should exist");
        assert_eq!(
            rx.borrow_and_update().name,
            format!("default:{}", test.default_policy)
        );

        // Update the pod to use a workload-specified default policy.
        test.index
            .write()
            .apply_pod(
                "ns-0".to_string(),
                "pod-0".to_string(),
                Some(("app", "app-0")).into_iter().collect(),
                HashMap::default(),
                PodSettings {
                    default_policy: Some(*default),
                    ..Default::default()
                },
            )
            .expect("pod should apply");

        // Ensure the change is observed.
        assert!(rx.has_changed().unwrap());
        assert_eq!(rx.borrow_and_update().name, format!("default:{}", default));
    }
}

#[test]
fn opaque_annotated() {
    let test = TestConfig::default();

    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings {
                opaque_ports: Some(9090).into_iter().collect(),
                ..Default::default()
            },
        )
        .expect("pod should apply");
    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 9090)
        .expect("pod-0.ns-0 should exist");

    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            protocol: ProxyProtocol::Opaque,
            ..test.default_server()
        }
    );
}

#[test]
fn authenticated_annotated() {
    let test = TestConfig::default();

    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings {
                require_id_ports: Some(9090).into_iter().collect(),
                ..Default::default()
            },
        )
        .expect("pod should apply");
    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 9090)
        .expect("pod-0.ns-0 should exist");

    let default = DefaultPolicy::Allow {
        authenticated_only: true,
        cluster_only: true,
    };
    assert_ne!(default, test.default_policy);
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            name: format!("default:{}", default),
            authorizations: mk_default_policy(default, test.cluster.networks.clone()),
            protocol: ProxyProtocol::Detect {
                timeout: test.detect_timeout,
            },
        },
    );
}

#[test]
fn links_authorization_policy_with_mtls_name() {
    let test = TestConfig::default();

    test.index
        .write()
        .apply_pod(
            "ns-0".to_string(),
            "pod-0".to_string(),
            Some(("app", "app-0")).into_iter().collect(),
            HashMap::default(),
            PodSettings::default(),
        )
        .expect("pod-0.ns-0 should not already exist");

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080)
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply_server(
        "ns-0".to_string(),
        "srv-8080".to_string(),
        Default::default(),
        Some(("app", "app-0")).into_iter().collect(),
        Port::Number(8080),
        Some(ProxyProtocol::Http1),
    );
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
    test.index.write().apply_authorization_policy(
        "ns-0".to_string(),
        "authz-foo".to_string(),
        AuthorizationPolicyTarget::Server("srv-8080".to_string()),
        vec![
            AuthenticationTarget::Network {
                namespace: None,
                name: "net-foo".to_string(),
            },
            AuthenticationTarget::MeshTLS {
                namespace: Some("ns-1".to_string()),
                name: "mtls-bar".to_string(),
            },
        ],
    );
    test.index.write().apply_network_authentication(
        "ns-0".to_string(),
        "net-foo".to_string(),
        vec![NetworkMatch {
            net: "10.0.0.0/8".parse().unwrap(),
            except: vec![],
        }],
    );
    test.index.write().apply_meshtls_authentication(
        "ns-1".to_string(),
        "mtls-bar".to_string(),
        vec![IdentityMatch::Exact("foo.bar".to_string())],
    );
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

    fn init_tracing() -> tracing::subscriber::DefaultGuard {
        tracing::subscriber::set_default(
            tracing_subscriber::fmt()
                .with_test_writer()
                .with_max_level(tracing::Level::TRACE)
                .finish(),
        )
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
}

impl Default for TestConfig {
    fn default() -> TestConfig {
        Self::from_default_policy(DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: true,
        })
    }
}
