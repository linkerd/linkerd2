use super::{super::*, *};
use crate::routes::ExplicitGKN;
use linkerd_policy_controller_core::{
    routes::{GrpcMethodMatch, GrpcRouteMatch, RouteMatch},
    POLICY_CONTROLLER_NAME,
};

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
    let policy = DefaultPolicy::Allow {
        authenticated_only: false,
        cluster_only: true,
    };

    let probe_networks = vec!["10.0.0.1/24".parse().unwrap()];
    let test = TestConfig::from_default_policy_with_probes(policy, probe_networks);

    // Create a pod.
    let container = Container {
        liveness_probe: Some(k8s::Probe {
            http_get: Some(k8s::HTTPGetAction {
                path: Some("/liveness-container-1".to_string()),
                port: k8s::IntOrString::Int(5432),
                ..Default::default()
            }),
            ..Default::default()
        }),
        readiness_probe: Some(k8s::Probe {
            http_get: Some(k8s::HTTPGetAction {
                path: Some("/ready-container-1".to_string()),
                port: k8s::IntOrString::Int(5432),
                ..Default::default()
            }),
            ..Default::default()
        }),
        ..Default::default()
    };

    let mut pod = mk_pod_with_containers("ns-0", "pod-0", Some(container));

    pod.labels_mut()
        .insert("app".to_string(), "app-0".to_string());

    test.index.write().apply(pod);

    let mut rx = test
        .index
        .write()
        .pod_server_rx("ns-0", "pod-0", 5432.try_into().unwrap())
        .expect("pod-0.ns-0 should exist");

    let mut expected_authorizations = HashMap::default();

    expected_authorizations.insert(
        AuthorizationRef::Default("probe"),
        ClientAuthorization {
            networks: vec!["10.0.0.1/24".parse::<IpNet>().unwrap().into()],
            authentication: ClientAuthentication::Unauthenticated,
        },
    );
    let liveness_match = RouteMatch::Grpc(GrpcRouteMatch {
        headers: vec![],
        method: Some(GrpcMethodMatch {
            method: Some("".to_string()),
            service: Some("".to_string()),
        }),
    });
    let ready_match = RouteMatch::Grpc(GrpcRouteMatch {
        headers: vec![],
        method: Some(GrpcMethodMatch {
            method: Some("".to_string()),
            service: Some("".to_string()),
        }),
    });

    // No Server is configured for the port, so expect the probe paths to be
    // authorized.
    let update = rx.borrow_and_update();

    let probes = update
        .routes
        .get(&InboundRouteRef::Default("probe"))
        .unwrap();

    let probes_rules = probes.rules.first().unwrap();

    assert!(
        probes_rules.matches.contains(&liveness_match),
        "matches: {:#?}",
        probes_rules.matches
    );

    assert!(
        probes_rules.matches.contains(&ready_match),
        "matches: {:#?}",
        probes_rules.matches
    );

    assert_eq!(probes.authorizations, expected_authorizations);

    drop(update);

    // // Create server.
    test.index.write().apply(mk_server(
        "ns-0",
        "srv-5432",
        Port::Number(5432.try_into().unwrap()),
        Some(("app", "app-0")),
        Some(("app", "app-0")),
        Some(k8s::policy::server::ProxyProtocol::Http1),
    ));

    assert!(rx.has_changed().unwrap());

    // // No routes are configured for the Server, so we should still expect the
    // // Pod's probe paths to be authorized.
    let update = rx.borrow_and_update();

    let probes = update
        .routes
        .get(&InboundRouteRef::Default("probe"))
        .unwrap();

    let probes_rules = probes.rules.first().unwrap();

    assert!(
        probes_rules.matches.contains(&liveness_match),
        "matches: {:#?}",
        probes_rules.matches
    );

    assert!(
        probes_rules.matches.contains(&ready_match),
        "matches: {:#?}",
        probes_rules.matches
    );

    assert_eq!(probes.authorizations, expected_authorizations);

    drop(update);

    // Create route.
    test.index
        .write()
        .apply(mk_route("ns-0", "route-foo", "srv-5432"));

    assert!(rx.has_changed().unwrap());

    // A route is now configured for the Server, so the Pod's probe paths
    // should not be automatically authorized.
    assert!(!rx
        .borrow_and_update()
        .routes
        .contains_key(&InboundRouteRef::Default("probes")));
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
