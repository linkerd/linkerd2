use super::*;

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
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");
    assert_eq!(*rx.borrow_and_update(), test.default_server());

    test.index.write().apply(mk_server(
        "ns-0",
        "srv-8080",
        Port::Number(8080.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: Default::default(),
            ratelimit: None,
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_http_routes(),
            grpc_routes: mk_default_grpc_routes(),
        },
    );
    test.index.write().apply(mk_server_authz(
        "ns-0",
        "authz-foo",
        selector,
        k8s::policy::server_authorization::Client {
            networks: Some(vec![k8s::policy::server_authorization::Network {
                cidr: "10.0.0.0/8".parse().unwrap(),
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
    assert_eq!(
        rx.borrow().reference,
        ServerRef::Server("srv-8080".to_string())
    );
    assert_eq!(rx.borrow().protocol, ProxyProtocol::Http1,);
    assert!(rx
        .borrow()
        .authorizations
        .contains_key(&AuthorizationRef::ServerAuthorization(
            "authz-foo".to_string()
        )));
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
