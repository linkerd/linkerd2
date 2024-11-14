use k8s_gateway_api::BackendObjectReference;
use k8s_gateway_api::CommonRouteSpec;
use k8s_gateway_api::HttpPathMatch;
use k8s_gateway_api::HttpPathModifier;
use k8s_gateway_api::HttpRequestMirrorFilter;
use k8s_gateway_api::HttpRequestRedirectFilter;
use k8s_gateway_api::HttpRoute;
use k8s_gateway_api::HttpRouteFilter;
use k8s_gateway_api::HttpRouteMatch;
use k8s_gateway_api::HttpRouteRule;
use k8s_gateway_api::HttpRouteSpec;
use k8s_gateway_api::HttpUrlRewriteFilter;
use k8s_gateway_api::LocalObjectReference;
use k8s_gateway_api::ParentReference;
use linkerd_policy_controller_k8s_api::{self as api};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| HttpRoute {
        metadata: meta(&ns),
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
async fn accepts_not_implemented_requestmirror() {
    admission::accepts(|ns| HttpRoute {
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
                filters: Some(vec![HttpRouteFilter::RequestMirror {
                    request_mirror: HttpRequestMirrorFilter {
                        backend_ref: BackendObjectReference {
                            group: None,
                            kind: None,
                            namespace: Some("foo".to_string()),
                            name: "foo".to_string(),
                            port: Some(80),
                        },
                    },
                }]),
                backend_refs: None,
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_not_implemented_urlrewrite() {
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
            rules: Some(vec![HttpRouteRule {
                matches: Some(vec![HttpRouteMatch {
                    path: Some(HttpPathMatch::Exact {
                        value: "/foo".to_string(),
                    }),
                    ..HttpRouteMatch::default()
                }]),
                filters: Some(vec![HttpRouteFilter::URLRewrite {
                    url_rewrite: HttpUrlRewriteFilter {
                        hostname: Some("foo".to_string()),
                        path: Some(HttpPathModifier::ReplaceFullPath {
                            replace_full_path: "baz".to_string(),
                        }),
                    },
                }]),
                backend_refs: None,
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_not_implemented_extensionref() {
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
            rules: Some(vec![HttpRouteRule {
                matches: Some(vec![HttpRouteMatch {
                    path: Some(HttpPathMatch::Exact {
                        value: "/foo".to_string(),
                    }),
                    ..HttpRouteMatch::default()
                }]),
                filters: Some(vec![HttpRouteFilter::ExtensionRef {
                    extension_ref: LocalObjectReference {
                        group: "".to_string(),
                        kind: "Service".to_string(),
                        name: "foo".to_string(),
                    },
                }]),
                backend_refs: None,
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_backend_unknown_kind() {
    admission::accepts(|ns| HttpRoute {
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
                backend_refs: Some(vec![k8s_gateway_api::HttpBackendRef {
                    backend_ref: Some(k8s_gateway_api::BackendRef {
                        weight: None,
                        inner: BackendObjectReference {
                            group: Some("alien.example.com".to_string()),
                            kind: Some("ExoService".to_string()),
                            namespace: Some("foo".to_string()),
                            name: "foo".to_string(),
                            port: None,
                        },
                    }),
                    filters: None,
                }]),
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_backend_service_with_port() {
    admission::accepts(|ns| HttpRoute {
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
                backend_refs: Some(vec![k8s_gateway_api::HttpBackendRef {
                    backend_ref: Some(k8s_gateway_api::BackendRef {
                        weight: None,
                        inner: BackendObjectReference {
                            group: Some("core".to_string()),
                            kind: Some("Service".to_string()),
                            namespace: Some("foo".to_string()),
                            name: "foo".to_string(),
                            port: Some(8080),
                        },
                    }),
                    filters: None,
                }]),
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_backend_service_implicit_with_port() {
    admission::accepts(|ns| HttpRoute {
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
                backend_refs: Some(vec![k8s_gateway_api::HttpBackendRef {
                    backend_ref: Some(k8s_gateway_api::BackendRef {
                        weight: None,
                        inner: BackendObjectReference {
                            group: None,
                            kind: None,
                            namespace: Some("foo".to_string()),
                            name: "foo".to_string(),
                            port: Some(8080),
                        },
                    }),
                    filters: None,
                }]),
            }]),
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
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_backend_service_without_port() {
    admission::accepts(|ns| HttpRoute {
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
                backend_refs: Some(vec![k8s_gateway_api::HttpBackendRef {
                    backend_ref: Some(k8s_gateway_api::BackendRef {
                        weight: None,
                        inner: BackendObjectReference {
                            group: Some("core".to_string()),
                            kind: Some("Service".to_string()),
                            namespace: Some("foo".to_string()),
                            name: "foo".to_string(),
                            port: None,
                        },
                    }),
                    filters: None,
                }]),
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_backend_service_implicit_without_port() {
    admission::accepts(|ns| HttpRoute {
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
                backend_refs: Some(vec![k8s_gateway_api::HttpBackendRef {
                    backend_ref: Some(k8s_gateway_api::BackendRef {
                        weight: None,
                        inner: BackendObjectReference {
                            group: None,
                            kind: None,
                            namespace: Some("foo".to_string()),
                            name: "foo".to_string(),
                            port: None,
                        },
                    }),
                    filters: None,
                }]),
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
    }]
}
