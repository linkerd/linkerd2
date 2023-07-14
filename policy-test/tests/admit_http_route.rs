use k8s_gateway_api::{BackendRef, LocalObjectReference};
use linkerd_policy_controller_k8s_api::{self as api, policy::httproute::*};
use linkerd_policy_test::admission;

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

#[tokio::test(flavor = "current_thread")]
async fn rejects_retry_filter_on_backend() {
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
                filters: None,
                backend_refs: Some(vec![HttpBackendRef {
                    backend_ref: Some(BackendRef {
                        inner: BackendObjectReference {
                            group: Some("core".to_string()),
                            kind: Some("Service".to_string()),
                            name: "foo".to_string(),
                            namespace: Some("bar".to_string()),
                            port: Some(666),
                        },
                        weight: Some(1),
                    }),
                    filters: Some(vec![k8s_gateway_api::HttpRouteFilter::ExtensionRef {
                        extension_ref: retry_filter(),
                    }]),
                }]),
                timeouts: None,
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_retry_filter() {
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
                filters: Some(vec![HttpRouteFilter::ExtensionRef {
                    extension_ref: retry_filter(),
                }]),
                backend_refs: Some(vec![HttpBackendRef {
                    backend_ref: Some(BackendRef {
                        inner: BackendObjectReference {
                            group: Some("core".to_string()),
                            kind: Some("Service".to_string()),
                            name: "foo".to_string(),
                            namespace: Some("bar".to_string()),
                            port: Some(666),
                        },
                        weight: Some(1),
                    }),
                    filters: None,
                }]),
                timeouts: None,
            }]),
        },
        status: None,
    })
    .await;
}

fn retry_filter() -> LocalObjectReference {
    LocalObjectReference {
        group: "policy.linkerd.io".to_string(),
        kind: "HTTPRetryFilter".to_string(),
        name: "my-great-retry-filter".to_string(),
    }
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
