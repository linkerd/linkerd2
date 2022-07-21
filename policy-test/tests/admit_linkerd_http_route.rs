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
            }]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn skips_validation_for_external_parent_ref() {
    // XXX(eliza): is this actually the desired behavior? if we get a
    // `policy.linkerd.io` version of an HTTPRoute, there's no reason for it to
    // exist unless it's targeting our `Server` type, right?
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
                filters: None,
            }]),
        },
        status: None,
    })
    .await;
}
