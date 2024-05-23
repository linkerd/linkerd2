use super::{super::*, *};
use crate::routes::ExplicitGKN;
use linkerd_policy_controller_core::POLICY_CONTROLLER_NAME;

#[test]
#[ignore = "not implemented yet"]
fn route_attaches_to_server() {
    let test = TestConfig::default();

    // Create pod.
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

    // Create server.
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
            routes: mk_default_routes(),
        },
    );

    // Create route.
    test.index
        .write()
        .apply(mk_route("ns-0", "route-foo", "srv-8080"));

    assert!(rx.has_changed().unwrap());

    assert_eq!(
        rx.borrow().reference,
        ServerRef::Server("srv-8080".to_string())
    );

    assert!(rx
        .borrow_and_update()
        .routes
        .contains_key(&InboundRouteRef::Linkerd(
            "route-foo".gkn::<k8s::gateway::GrpcRoute>()
        )));

    // Create authz policy.
    test.index.write().apply(mk_authorization_policy(
        "ns-0",
        "authz-foo",
        "route-foo",
        vec![NamespacedTargetRef {
            group: None,
            kind: "ServiceAccount".to_string(),
            namespace: Some("ns-0".to_string()),
            name: "foo".to_string(),
        }],
    ));

    assert!(rx.has_changed().unwrap());

    assert!(rx.borrow().routes
        [&InboundRouteRef::Linkerd("route-foo".gkn::<k8s::gateway::GrpcRoute>())]
        .authorizations
        .contains_key(&AuthorizationRef::AuthorizationPolicy(
            "authz-foo".to_string()
        )));
}

#[test]
#[ignore = "not implemented yet"]
fn routes_created_for_probes() {
    todo!()
}

fn mk_route(
    ns: impl ToString,
    name: impl ToString,
    server: impl ToString,
) -> k8s::gateway::GrpcRoute {
    k8s::gateway::GrpcRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            creation_timestamp: Some(k8s::Time(chrono::Utc::now())),
            ..Default::default()
        },
        spec: k8s::gateway::GrpcRouteSpec {
            inner: k8s::gateway::CommonRouteSpec {
                parent_refs: Some(vec![k8s::gateway::ParentReference {
                    group: Some(POLICY_API_GROUP.to_string()),
                    kind: Some("Server".to_string()),
                    namespace: None,
                    name: server.to_string(),
                    section_name: None,
                    port: None,
                }]),
            },
            hostnames: None,
            rules: Some(vec![k8s::gateway::GrpcRouteRule {
                matches: Some(vec![k8s::gateway::GrpcRouteMatch {
                    headers: None,
                    method: Some(k8s::gateway::GrpcMethodMatch::Exact {
                        method: Some("Test".to_string()),
                        service: Some("io.linkerd.testing".to_string()),
                    }),
                }]),
                filters: None,
                backend_refs: None,
            }]),
        },
        status: Some(k8s::gateway::GrpcRouteStatus {
            inner: k8s::gateway::RouteStatus {
                parents: vec![k8s::gateway::RouteParentStatus {
                    parent_ref: k8s::gateway::ParentReference {
                        group: Some(POLICY_API_GROUP.to_string()),
                        kind: Some("Server".to_string()),
                        namespace: None,
                        name: server.to_string(),
                        section_name: None,
                        port: None,
                    },
                    controller_name: POLICY_CONTROLLER_NAME.to_string(),
                    conditions: vec![k8s::Condition {
                        last_transition_time: k8s::Time(chrono::DateTime::<chrono::Utc>::MIN_UTC),
                        message: "".to_string(),
                        observed_generation: None,
                        reason: "Accepted".to_string(),
                        status: "True".to_string(),
                        type_: "Accepted".to_string(),
                    }],
                }],
            },
        }),
    }
}
