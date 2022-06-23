use k8s_gateway_api::{
    BackendObjectReference, BackendRef, CommonRouteSpec, HttpBackendRef, HttpPathMatch,
    HttpRequestMirrorFilter, HttpRoute, HttpRouteFilter, HttpRouteMatch, HttpRouteRule,
    HttpRouteSpec, ParentReference,
};
use linkerd_policy_controller_k8s_api::{self as api};
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
                parent_refs: Some(vec![ParentReference {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: Some("Server".to_string()),
                    namespace: Some(ns),
                    name: "my-server".to_string(),
                    section_name: None,
                    port: None,
                }]),
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
                        weight: None,
                        name: "my-svc".to_string(),
                        port: 8888,
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
async fn skips_validation_for_external_parent_ref() {
    // We test that HttpRoutes which do not have a Server as a parent_ref are
    // not validated by creating an HttpRoute with an unsupported filter
    // (RequestMirror) and ensuring that it is accepted anyway.
    admission::accepts(|ns| HttpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![ParentReference {
                    group: Some("foo.bar.bas".to_string()),
                    kind: Some("Gateway".to_string()),
                    namespace: Some(ns),
                    name: "my-gateway".to_string(),
                    section_name: None,
                    port: None,
                }]),
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
                            kind: Some("Service".to_string()),
                            name: "my-backend".to_string(),
                            namespace: None,
                            port: Some(8888),
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
async fn rejects_unsupported_filter() {
    admission::rejects(|ns| HttpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![ParentReference {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: Some("Server".to_string()),
                    namespace: Some(ns),
                    name: "my-server".to_string(),
                    section_name: None,
                    port: None,
                }]),
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
                            kind: Some("Service".to_string()),
                            name: "my-backend".to_string(),
                            namespace: None,
                            port: Some(8888),
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
async fn rejects_unsupported_backend_filter() {
    admission::rejects(|ns| HttpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: HttpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![ParentReference {
                    group: Some("policy.linkerd.io".to_string()),
                    kind: Some("Server".to_string()),
                    namespace: Some(ns),
                    name: "my-server".to_string(),
                    section_name: None,
                    port: None,
                }]),
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
                        weight: None,
                        name: "my-backend".to_string(),
                        port: 8888,
                    }),
                    filters: Some(vec![HttpRouteFilter::RequestMirror {
                        request_mirror: HttpRequestMirrorFilter {
                            backend_ref: BackendObjectReference {
                                group: None,
                                kind: Some("Service".to_string()),
                                name: "my-backend".to_string(),
                                namespace: None,
                                port: Some(8888),
                            },
                        },
                    }]),
                }]),
            }]),
        },
        status: None,
    })
    .await;
}
