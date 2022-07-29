use super::*;

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
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: Default::default(),
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_routes(),
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
        Some("srv-8080"),
        vec![
            NamespacedTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "NetworkAuthentication".to_string(),
                name: "net-foo".to_string(),
                namespace: None,
            },
            NamespacedTargetRef {
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
        "ns-1",
        "mtls-bar",
        Some("foo.bar".to_string()),
        None,
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: hashmap!(
                AuthorizationRef::AuthorizationPolicy("authz-foo".to_string()) => authz
            )
            .into_iter()
            .collect(),
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_routes(),
        },
    );
}

#[test]
fn authorization_targets_namespace() {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: Default::default(),
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_routes(),
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
        Option::<&str>::None,
        vec![
            NamespacedTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "NetworkAuthentication".to_string(),
                name: "net-foo".to_string(),
                namespace: None,
            },
            NamespacedTargetRef {
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
        "ns-1",
        "mtls-bar",
        Some("foo.bar".to_string()),
        None,
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: hashmap!(
                AuthorizationRef::AuthorizationPolicy("authz-foo".to_string()) => authz
            )
            .into_iter()
            .collect(),
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_routes(),
        },
    );
}

#[test]
fn links_authorization_policy_with_service_account() {
    let test = TestConfig::default();

    let mut pod = mk_pod("ns-0", "pod-0", Some(("container-0", None)));
    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());
    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: Default::default(),
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_routes(),
        },
    );

    let authz = ClientAuthorization {
        networks: vec!["10.0.0.0/8".parse::<IpNet>().unwrap().into()],
        authentication: ClientAuthentication::TlsAuthenticated(vec![IdentityMatch::Exact(
            "foo.ns-0.serviceaccount.identity.linkerd.cluster.example.com".to_string(),
        )]),
    };
    test.index.write().apply(mk_authorization_policy(
        "ns-0",
        "authz-foo",
        Some("srv-8080"),
        vec![
            NamespacedTargetRef {
                group: Some("policy.linkerd.io".to_string()),
                kind: "NetworkAuthentication".to_string(),
                name: "net-foo".to_string(),
                namespace: None,
            },
            NamespacedTargetRef {
                group: None,
                kind: "ServiceAccount".to_string(),
                namespace: Some("ns-0".to_string()),
                name: "foo".to_string(),
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
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: hashmap!(
                AuthorizationRef::AuthorizationPolicy("authz-foo".to_string()) => authz
            )
            .into_iter()
            .collect(),
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_routes(),
        },
    );
}

fn mk_authorization_policy(
    ns: impl ToString,
    name: impl ToString,
    server: Option<impl ToString>,
    authns: impl IntoIterator<Item = NamespacedTargetRef>,
) -> k8s::policy::AuthorizationPolicy {
    k8s::policy::AuthorizationPolicy {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::AuthorizationPolicySpec {
            target_ref: match server {
                Some(server) => LocalTargetRef {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: "Server".to_string(),
                    name: server.to_string(),
                },
                None => LocalTargetRef {
                    group: Some("core".to_string()),
                    kind: "Namespace".to_string(),
                    name: ns.to_string(),
                },
            },
            required_authentication_refs: authns.into_iter().collect(),
        },
    }
}

fn mk_meshtls_authentication(
    ns: impl ToString,
    name: impl ToString,
    identities: impl IntoIterator<Item = String>,
    refs: impl IntoIterator<Item = NamespacedTargetRef>,
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
