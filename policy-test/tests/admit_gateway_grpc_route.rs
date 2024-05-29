use linkerd_policy_controller_k8s_api::{self as k8s, gateway as k8s_gateway_api};
use linkerd_policy_test::admission;

fn meta(ns: impl ToString) -> k8s::ObjectMeta {
    k8s::ObjectMeta {
        name: Some("test".to_string()),
        namespace: Some(ns.to_string()),
        ..Default::default()
    }
}

fn server_parent_ref(ns: impl ToString) -> k8s_gateway_api::ParentReference {
    k8s_gateway_api::ParentReference {
        group: Some("policy.linkerd.io".to_string()),
        kind: Some("Server".to_string()),
        namespace: Some(ns.to_string()),
        name: "my-server".to_string(),
        section_name: None,
        port: None,
    }
}

fn bare_route(namespace: impl AsRef<str>) -> k8s_gateway_api::GrpcRoute {
    let namespace = namespace.as_ref();

    k8s_gateway_api::GrpcRoute {
        status: None,
        metadata: meta(namespace),
        spec: k8s_gateway_api::GrpcRouteSpec {
            inner: k8s_gateway_api::CommonRouteSpec {
                parent_refs: Some(vec![server_parent_ref(namespace)]),
            },
            ..Default::default()
        },
    }
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_bare_route() {
    admission::accepts(bare_route).await;
}

// === `GrpcRouteFilter` tests ===

#[tokio::test(flavor = "current_thread")]
async fn accepts_request_header_modifier() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            filters: Some(vec![
                k8s_gateway_api::GrpcRouteFilter::RequestHeaderModifier {
                    request_header_modifier: k8s_gateway_api::HttpRequestHeaderFilter {
                        add: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "x-to-add".to_string(),
                            value: "added-to-response".to_string(),
                        }]),
                        set: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "x-to-set".to_string(),
                            value: "set-on-response".to_string(),
                        }]),
                        remove: Some(vec!["x-to-remove".to_string()]),
                    },
                },
            ]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_response_header_modifier() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            filters: Some(vec![
                k8s_gateway_api::GrpcRouteFilter::ResponseHeaderModifier {
                    response_header_modifier: k8s_gateway_api::HttpRequestHeaderFilter {
                        add: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "x-to-add".to_string(),
                            value: "added-to-response".to_string(),
                        }]),
                        set: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "x-to-set".to_string(),
                            value: "set-on-response".to_string(),
                        }]),
                        remove: Some(vec!["x-to-remove".to_string()]),
                    },
                },
            ]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_bidirectional_header_modifiers() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            filters: Some(vec![
                k8s_gateway_api::GrpcRouteFilter::RequestHeaderModifier {
                    request_header_modifier: k8s_gateway_api::HttpRequestHeaderFilter {
                        add: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "x-add-to-request".to_string(),
                            value: "added-to-request".to_string(),
                        }]),
                        set: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "x-set-on-request".to_string(),
                            value: "set-on-request".to_string(),
                        }]),
                        remove: Some(vec!["x-remove-from-request".to_string()]),
                    },
                },
                k8s_gateway_api::GrpcRouteFilter::ResponseHeaderModifier {
                    response_header_modifier: k8s_gateway_api::HttpRequestHeaderFilter {
                        add: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "x-add-to-response".to_string(),
                            value: "added-to-response".to_string(),
                        }]),
                        set: Some(vec![k8s_gateway_api::HttpHeader {
                            name: "x-set-on-response".to_string(),
                            value: "set-on-response".to_string(),
                        }]),
                        remove: Some(vec!["x-remove-from-response".to_string()]),
                    },
                },
            ]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_unimplemented_request_mirror() {
    admission::accepts(|namespace| {
        let mut route = bare_route(&namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            filters: Some(vec![k8s_gateway_api::GrpcRouteFilter::RequestMirror {
                request_mirror: k8s_gateway_api::HttpRequestMirrorFilter {
                    backend_ref: k8s_gateway_api::BackendObjectReference {
                        group: Some("policy.linkerd.io".to_string()),
                        kind: Some("Server".to_string()),
                        namespace: Some(namespace.to_string()),
                        name: "mirror-server".to_string(),
                        port: None,
                    },
                },
            }]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_unimplemented_extension_ref() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            filters: Some(vec![k8s_gateway_api::GrpcRouteFilter::ExtensionRef {
                extension_ref: k8s_gateway_api::LocalObjectReference {
                    kind: "Server".to_string(),
                    name: "local-server".to_string(),
                    group: "policy.linkerd.io".to_string(),
                },
            }]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

// === `GrpcRouteMatch` tests ===

#[tokio::test(flavor = "current_thread")]
async fn accepts_exact_method_match() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                method: Some(k8s_gateway_api::GrpcMethodMatch::Exact {
                    method: Some("Test".to_string()),
                    service: Some("io.linkerd.Testing".to_string()),
                }),
                ..Default::default()
            }]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_regex_method_match() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                method: Some(k8s_gateway_api::GrpcMethodMatch::RegularExpression {
                    method: Some("Test".to_string()),
                    service: Some("io.linkerd.Testing".to_string()),
                }),
                ..Default::default()
            }]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_exact_header_match() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                headers: Some(vec![k8s_gateway_api::GrpcHeaderMatch::Exact {
                    name: "x-test".to_string(),
                    value: "testing.linkerd.io".to_string(),
                }]),
                ..Default::default()
            }]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_regex_header_match() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                headers: Some(vec![k8s_gateway_api::GrpcHeaderMatch::RegularExpression {
                    name: "x-test".to_string(),
                    value: "testing.linkerd.io".to_string(),
                }]),
                ..Default::default()
            }]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_exact_header_and_method_matches() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                method: Some(k8s_gateway_api::GrpcMethodMatch::Exact {
                    method: Some("Test".to_string()),
                    service: Some("io.linkerd.Testing".to_string()),
                }),
                headers: Some(vec![k8s_gateway_api::GrpcHeaderMatch::Exact {
                    name: "x-test".to_string(),
                    value: "testing.linkerd.io".to_string(),
                }]),
            }]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_regex_header_and_method_matches() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
                method: Some(k8s_gateway_api::GrpcMethodMatch::RegularExpression {
                    method: Some("Test".to_string()),
                    service: Some("io.linkerd.Testing".to_string()),
                }),
                headers: Some(vec![k8s_gateway_api::GrpcHeaderMatch::RegularExpression {
                    name: "x-test".to_string(),
                    value: "testing.linkerd.io".to_string(),
                }]),
            }]),
            ..Default::default()
        }]);

        route
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn accepts_mixed_header_and_method_matches() {
    admission::accepts(|namespace| {
        let mut route = bare_route(namespace);

        route.spec.rules = Some(vec![k8s_gateway_api::GrpcRouteRule {
            matches: Some(vec![
                k8s_gateway_api::GrpcRouteMatch {
                    method: Some(k8s_gateway_api::GrpcMethodMatch::Exact {
                        method: Some("Test".to_string()),
                        service: Some("io.linkerd.Testing".to_string()),
                    }),
                    headers: Some(vec![k8s_gateway_api::GrpcHeaderMatch::Exact {
                        name: "x-test".to_string(),
                        value: "testing.linkerd.io".to_string(),
                    }]),
                },
                k8s_gateway_api::GrpcRouteMatch {
                    method: Some(k8s_gateway_api::GrpcMethodMatch::RegularExpression {
                        method: Some("Test".to_string()),
                        service: Some("io.linkerd.Testing".to_string()),
                    }),
                    headers: Some(vec![k8s_gateway_api::GrpcHeaderMatch::RegularExpression {
                        name: "x-test".to_string(),
                        value: "testing.linkerd.io".to_string(),
                    }]),
                },
            ]),
            ..Default::default()
        }]);

        route
    })
    .await;
}
