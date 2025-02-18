use linkerd_policy_controller_k8s_api::{self as k8s, gateway, policy};
use linkerd_policy_test::{admission, egress_network_parent_ref};

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| policy::HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: policy::HttpRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(rules()),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_egress_network() {
    admission::accepts(|ns| policy::HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: policy::HttpRouteSpec {
            parent_refs: Some(vec![egress_network_parent_ref(ns, Some(555))]),
            hostnames: None,
            rules: Some(rules()),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_egress_network_parent_with_no_port() {
    admission::rejects(|ns| policy::HttpRoute {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: policy::HttpRouteSpec {
            parent_refs: Some(vec![egress_network_parent_ref(ns, None)]),
            hostnames: None,
            rules: Some(rules()),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_relative_path_match() {
    admission::rejects(|ns| policy::HttpRoute {
        metadata: meta(&ns),
        spec: policy::HttpRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![policy::httproute::HttpRouteRule {
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("foo/bar".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..Default::default()
                }]),
                filters: None,
                backend_refs: None,
                timeouts: None,
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_relative_redirect_path() {
    admission::rejects(|ns| policy::HttpRoute {
        metadata: meta(&ns),
        spec: policy::HttpRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![policy::httproute::HttpRouteRule {
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..Default::default()
                }]),
                filters: Some(vec![policy::httproute::HttpRouteFilter::RequestRedirect {
                    request_redirect: gateway::HTTPRouteRulesFiltersRequestRedirect {
                        scheme: None,
                        hostname: None,
                        path: Some(gateway::HTTPRouteRulesFiltersRequestRedirectPath {
                            replace_full_path: Some("foo/bar".to_string()),
                            r#type: gateway::HTTPRouteRulesFiltersRequestRedirectPathType::ReplaceFullPath,
                            ..Default::default()
                        }),
                        port: None,
                        status_code: None,
                    },
                }]),
                backend_refs: None,
                timeouts: None,
            }]),
        },
        status: None,
    })
    .await;
}

fn server_parent_ref(ns: impl ToString) -> gateway::HTTPRouteParentRefs {
    gateway::HTTPRouteParentRefs {
        group: Some("policy.linkerd.io".to_string()),
        kind: Some("Server".to_string()),
        namespace: Some(ns.to_string()),
        name: "my-server".to_string(),
        section_name: None,
        port: None,
    }
}

fn meta(ns: impl ToString) -> k8s::ObjectMeta {
    k8s::ObjectMeta {
        namespace: Some(ns.to_string()),
        name: Some("test".to_string()),
        ..Default::default()
    }
}

fn rules() -> Vec<policy::httproute::HttpRouteRule> {
    vec![policy::httproute::HttpRouteRule {
        matches: Some(vec![gateway::HTTPRouteRulesMatches {
            path: Some(gateway::HTTPRouteRulesMatchesPath {
                value: Some("/foo".to_string()),
                r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
            }),
            ..Default::default()
        }]),
        filters: None,
        backend_refs: None,
        timeouts: None,
    }]
}
