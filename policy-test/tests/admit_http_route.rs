use linkerd_policy_controller_k8s_api::{self as api, policy::httproute::*};
use linkerd_policy_test::{admission, egress_network_parent_ref};

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| HttpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![server_parent_ref(ns)]),
            },
            hostnames: None,
            rules: Some(rules()),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_egress_network() {
    admission::accepts(|ns| HttpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![egress_network_parent_ref(ns, Some(555))]),
            },
            hostnames: None,
            rules: Some(rules()),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_egress_network_parent_with_no_port() {
    admission::rejects(|ns| HttpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![egress_network_parent_ref(ns, None)]),
            },
            hostnames: None,
            rules: Some(rules()),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_relative_path_match() {
    admission::rejects(|ns| HttpRoute {
        metadata: meta(&ns),
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![server_parent_ref(ns)]),
            },
            hostnames: None,
            rules: Some(vec![HttpRouteRule {
                matches: Some(vec![HttpRouteMatch {
                    path: Some(HttpPathMatch::Exact {
                        value: "foo/bar".to_string(),
                    }),
                    ..HttpRouteMatch::default()
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
    admission::rejects(|ns| HttpRoute {
        metadata: meta(&ns),
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![server_parent_ref(ns)]),
            },
            hostnames: None,
            rules: Some(vec![HttpRouteRule {
                matches: Some(vec![HttpRouteMatch {
                    path: Some(HttpPathMatch::Exact {
                        value: "/foo".to_string(),
                    }),
                    ..HttpRouteMatch::default()
                }]),
                filters: Some(vec![HttpRouteFilter::RequestRedirect {
                    request_redirect: HttpRequestRedirectFilter {
                        scheme: None,
                        hostname: None,
                        path: Some(HttpPathModifier::ReplaceFullPath {
                            replace_full_path: "foo/bar".to_string(),
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

fn server_parent_ref(ns: impl ToString) -> ParentReference {
    ParentReference {
        group: Some("policy.linkerd.io".to_string()),
        kind: Some("Server".to_string()),
        namespace: Some(ns.to_string()),
        name: "my-server".to_string(),
        section_name: None,
        port: None,
    }
}

fn meta(ns: impl ToString) -> api::ObjectMeta {
    api::ObjectMeta {
        namespace: Some(ns.to_string()),
        name: Some("test".to_string()),
        ..Default::default()
    }
}

fn rules() -> Vec<HttpRouteRule> {
    vec![HttpRouteRule {
        matches: Some(vec![HttpRouteMatch {
            path: Some(HttpPathMatch::Exact {
                value: "/foo".to_string(),
            }),
            ..HttpRouteMatch::default()
        }]),
        filters: None,
        backend_refs: None,
        timeouts: None,
    }]
}
