use super::*;
use crate::routes::ExplicitGKN;
use linkerd_policy_controller_core::{
    routes::{HttpRouteMatch, Method, PathMatch},
    POLICY_CONTROLLER_NAME,
};
use linkerd_policy_controller_k8s_api::policy;

const POLICY_API_GROUP: &str = "policy.linkerd.io";

#[test]
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
            ratelimit: None,
            protocol: ProxyProtocol::Http1,
            http_routes: mk_default_http_routes(),
            grpc_routes: mk_default_grpc_routes(),
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
        .http_routes
        .contains_key(&RouteRef::Resource("route-foo".gkn::<policy::HttpRoute>())));

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
    assert!(
        rx.borrow().http_routes[&RouteRef::Resource("route-foo".gkn::<policy::HttpRoute>())]
            .authorizations
            .contains_key(&AuthorizationRef::AuthorizationPolicy(
                "authz-foo".to_string()
            ))
    );
}

#[test]
fn routes_created_for_probes() {
    let policy = DefaultPolicy::Allow {
        authenticated_only: false,
        cluster_only: true,
    };
    let probe_networks = vec!["10.0.0.1/24".parse().unwrap()];
    let test = TestConfig::from_default_policy_with_probes(policy, probe_networks);

    // Create a pod.
    let container = k8s::Container {
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
    let liveness_match = HttpRouteMatch {
        path: Some(PathMatch::Exact("/liveness-container-1".to_string())),
        headers: vec![],
        query_params: vec![],
        method: Some(Method::GET),
    };
    let ready_match = HttpRouteMatch {
        path: Some(PathMatch::Exact("/ready-container-1".to_string())),
        headers: vec![],
        query_params: vec![],
        method: Some(Method::GET),
    };

    // No Server is configured for the port, so expect the probe paths to be
    // authorized.
    let update = rx.borrow_and_update();
    let probes = update.http_routes.get(&RouteRef::Default("probe")).unwrap();
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
    let probes = update.http_routes.get(&RouteRef::Default("probe")).unwrap();
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
        .http_routes
        .contains_key(&RouteRef::Default("probes")));
}

fn mk_route(
    ns: impl ToString,
    name: impl ToString,
    server: impl ToString,
) -> k8s::policy::HttpRoute {
    use chrono::Utc;
    use k8s::{policy::httproute::*, Time};

    HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            creation_timestamp: Some(Time(Utc::now())),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![ParentReference {
                    group: Some(POLICY_API_GROUP.to_string()),
                    kind: Some("Server".to_string()),
                    namespace: None,
                    name: server.to_string(),
                    section_name: None,
                    port: None,
                }]),
            },
            hostnames: None,
            rules: Some(vec![HttpRouteRule {
                matches: Some(vec![HttpRouteMatch {
                    path: Some(HttpPathMatch::PathPrefix {
                        value: "/foo/bar".to_string(),
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
        status: Some(k8s::policy::httproute::HttpRouteStatus {
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
fn mk_authorization_policy(
    ns: impl ToString,
    name: impl ToString,
    route: impl ToString,
    authns: impl IntoIterator<Item = NamespacedTargetRef>,
) -> k8s::policy::AuthorizationPolicy {
    k8s::policy::AuthorizationPolicy {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some(POLICY_API_GROUP.to_string()),
                kind: "HttpRoute".to_string(),
                name: route.to_string(),
            },
            required_authentication_refs: authns.into_iter().collect(),
        },
    }
}
