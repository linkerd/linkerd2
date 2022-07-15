use super::*;

#[test]
fn route_attaches_to_server() {
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
            protocol: ProxyProtocol::Http1,
            http_routes: HashMap::default(),
        },
    );
    test.index
        .write()
        .apply(mk_http_route("ns-0", "route-foo", "srv-8080"));
    assert!(rx.has_changed().unwrap());
    assert_eq!(
        rx.borrow().reference,
        ServerRef::Server("srv-8080".to_string())
    );
    assert!(rx.borrow().http_routes.contains_key("route-foo"));
}

fn mk_http_route(
    ns: impl ToString,
    name: impl ToString,
    server: impl ToString,
) -> k8s_gateway_api::HttpRoute {
    k8s_gateway_api::HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s_gateway_api::HttpRouteSpec {
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![k8s_gateway_api::ParentReference {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: Some("Server".to_string()),
                    namespace: None,
                    name: server.to_string(),
                    section_name: None,
                    port: None,
                }]),
            },
            hostnames: None,
            rules: Some(vec![k8s_gateway_api::HttpRouteRule {
                matches: Some(vec![k8s_gateway_api::HttpRouteMatch {
                    path: Some(k8s_gateway_api::HttpPathMatch::PathPrefix {
                        value: "/foo/bar".to_string(),
                    }),
                    headers: None,
                    query_params: None,
                    method: Some("GET".to_string()),
                }]),
                filters: None,
                backend_refs: None,
            }]),
        },
        status: None,
    }
}
