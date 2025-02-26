use linkerd_policy_controller_k8s_api::{self as api, gateway};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(rules()),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_not_implemented_requestmirror() {
    admission::accepts(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: Some(vec![gateway::HTTPRouteRulesFilters {
                    request_mirror: Some(gateway::HTTPRouteRulesFiltersRequestMirror {
                        backend_ref: gateway::HTTPRouteRulesFiltersRequestMirrorBackendRef {
                            group: None,
                            kind: None,
                            namespace: Some("foo".to_string()),
                            name: "foo".to_string(),
                            port: Some(80),
                        },
                        percent: Some(100),
                        ..Default::default()
                    }),
                    r#type: gateway::HTTPRouteRulesFiltersType::RequestMirror,
                    ..Default::default()
                }]),
                backend_refs: None,
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_not_implemented_urlrewrite() {
    admission::accepts(|ns| gateway::HTTPRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: Some(vec![gateway::HTTPRouteRulesFilters {
                    url_rewrite: Some(gateway::HTTPRouteRulesFiltersUrlRewrite {
                        hostname: Some("foo".to_string()),
                        path: Some(gateway::HTTPRouteRulesFiltersUrlRewritePath {
                            replace_full_path: Some("baz".to_string()),
                            r#type:
                                gateway::HTTPRouteRulesFiltersUrlRewritePathType::ReplaceFullPath,
                            ..Default::default()
                        }),
                    }),
                    r#type: gateway::HTTPRouteRulesFiltersType::UrlRewrite,
                    ..Default::default()
                }]),
                backend_refs: None,
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_not_implemented_extensionref() {
    admission::accepts(|ns| gateway::HTTPRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: Some(vec![gateway::HTTPRouteRulesFilters {
                    extension_ref: Some(gateway::HTTPRouteRulesFiltersExtensionRef {
                        group: "".to_string(),
                        kind: "Service".to_string(),
                        name: "foo".to_string(),
                    }),
                    r#type: gateway::HTTPRouteRulesFiltersType::ExtensionRef,
                    ..Default::default()
                }]),
                backend_refs: None,
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_backend_unknown_kind() {
    admission::accepts(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: None,
                backend_refs: Some(vec![gateway::HTTPRouteRulesBackendRefs {
                    weight: None,
                    group: Some("alien.example.com".to_string()),
                    kind: Some("ExoService".to_string()),
                    namespace: Some("foo".to_string()),
                    name: "foo".to_string(),
                    port: None,
                    filters: None,
                }]),
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_backend_service_with_port() {
    admission::accepts(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: None,
                backend_refs: Some(vec![gateway::HTTPRouteRulesBackendRefs {
                    weight: None,
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    namespace: Some("foo".to_string()),
                    name: "foo".to_string(),
                    port: Some(8080),
                    filters: None,
                }]),
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_backend_service_implicit_with_port() {
    admission::accepts(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: None,
                backend_refs: Some(vec![gateway::HTTPRouteRulesBackendRefs {
                    weight: None,
                    group: None,
                    kind: None,
                    namespace: Some("foo".to_string()),
                    name: "foo".to_string(),
                    port: Some(8080),
                    filters: None,
                }]),
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_relative_path_match() {
    admission::rejects(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: None,
                backend_refs: None,
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_relative_redirect_path() {
    admission::rejects(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),

            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: Some(vec![gateway::HTTPRouteRulesFilters {
                    request_redirect: Some(gateway::HTTPRouteRulesFiltersRequestRedirect {
                        scheme: None,
                        hostname: None,
                        path: Some(gateway::HTTPRouteRulesFiltersRequestRedirectPath {
                            replace_full_path: Some("foo/bar".to_string()),
                            r#type:
                                gateway::HTTPRouteRulesFiltersRequestRedirectPathType::ReplaceFullPath,
                            ..Default::default()
                        }),
                        port: None,
                        status_code: None,
                    }),
                    r#type: gateway::HTTPRouteRulesFiltersType::RequestRedirect,
                    ..Default::default()
                }]),
                backend_refs: None,
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_backend_service_without_port() {
    admission::rejects(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),
            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: None,
                backend_refs: Some(vec![gateway::HTTPRouteRulesBackendRefs {
                    weight: None,
                    group: Some("core".to_string()),
                    kind: Some("Service".to_string()),
                    namespace: Some("foo".to_string()),
                    name: "foo".to_string(),
                    port: None,
                    filters: None,
                }]),
                ..Default::default()
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_backend_service_implicit_without_port() {
    admission::rejects(|ns| gateway::HTTPRoute {
        metadata: meta(&ns),
        spec: gateway::HTTPRouteSpec {
            parent_refs: Some(vec![server_parent_ref(ns)]),

            hostnames: None,
            rules: Some(vec![gateway::HTTPRouteRules {
                name: None,
                matches: Some(vec![gateway::HTTPRouteRulesMatches {
                    path: Some(gateway::HTTPRouteRulesMatchesPath {
                        value: Some("/foo".to_string()),
                        r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
                    }),
                    ..gateway::HTTPRouteRulesMatches::default()
                }]),
                filters: None,
                backend_refs: Some(vec![gateway::HTTPRouteRulesBackendRefs {
                    weight: None,
                    group: None,
                    kind: None,
                    namespace: Some("foo".to_string()),
                    name: "foo".to_string(),
                    port: None,
                    filters: None,
                }]),
                ..Default::default()
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

fn meta(ns: impl ToString) -> api::ObjectMeta {
    api::ObjectMeta {
        namespace: Some(ns.to_string()),
        name: Some("test".to_string()),
        ..Default::default()
    }
}

fn rules() -> Vec<gateway::HTTPRouteRules> {
    vec![gateway::HTTPRouteRules {
        name: None,
        matches: Some(vec![gateway::HTTPRouteRulesMatches {
            path: Some(gateway::HTTPRouteRulesMatchesPath {
                value: Some("/foo".to_string()),
                r#type: Some(gateway::HTTPRouteRulesMatchesPathType::Exact),
            }),
            ..gateway::HTTPRouteRulesMatches::default()
        }]),
        filters: None,
        backend_refs: None,
        ..Default::default()
    }]
}
