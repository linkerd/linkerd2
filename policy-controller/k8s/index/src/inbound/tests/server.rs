use super::*;

#[test]
fn links_named_server_port() {
    let test = TestConfig::default();

    let mut pod = mk_pod(
        "ns-0",
        "pod-0",
        Some((
            "container-0",
            Some(ContainerPort {
                name: Some("admin".to_string()),
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
        "srv-admin",
        Port::Name("admin".to_string()),
        None,
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        *rx.borrow_and_update(),
        InboundServer {
            reference: ServerRef::Server("srv-admin".to_string()),
            authorizations: Default::default(),
            ratelimit: None,
            concurrency_limit: None,
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_http_routes(),
            grpc_routes: mk_default_grpc_routes(),
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
            reference: ServerRef::Server("srv-8080".to_string()),
            authorizations: Default::default(),
            ratelimit: None,
            concurrency_limit: None,
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_http_routes(),
            grpc_routes: mk_default_grpc_routes(),
        },
    );
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
            reference: ServerRef::Server("srv-0".to_string()),
            authorizations: Default::default(),
            ratelimit: None,
            concurrency_limit: None,
            protocol: ProxyProtocol::Http2,
            http_routes: mk_default_http_routes(),
            grpc_routes: mk_default_grpc_routes(),
        }
    );

    test.index.write().apply({
        srv.spec.pod_selector = Some(("label", "value")).into_iter().collect();
        srv
    });
    assert!(rx.has_changed().unwrap());
    assert_eq!(*rx.borrow(), test.default_server());
}
