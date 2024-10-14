use linkerd_policy_controller_k8s_api::{
    self as api,
    gateway::{
        BackendObjectReference, BackendRef, CommonRouteSpec, TlsRoute, TlsRouteRule, TlsRouteSpec,
    },
};
use linkerd_policy_test::{admission, egress_network_parent_ref};

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_egress_network() {
    admission::accepts(|ns| TlsRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: TlsRouteSpec {
            hostnames: None,
            inner: CommonRouteSpec {
                parent_refs: Some(vec![egress_network_parent_ref(ns, Some(555))]),
            },
            rules: rules(1),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_egress_network_parent_with_no_port() {
    admission::rejects(|ns| TlsRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: TlsRouteSpec {
            hostnames: None,
            inner: CommonRouteSpec {
                parent_refs: Some(vec![egress_network_parent_ref(ns, None)]),
            },
            rules: rules(1),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_if_more_than_one_rule() {
    admission::rejects(|ns| TlsRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: TlsRouteSpec {
            hostnames: None,
            inner: CommonRouteSpec {
                parent_refs: Some(vec![egress_network_parent_ref(ns, Some(555))]),
            },
            rules: rules(2),
        },
        status: None,
    })
    .await;
}

fn rules(n: u16) -> Vec<TlsRouteRule> {
    let mut rules = Vec::default();
    for n in 1..=n {
        rules.push(TlsRouteRule {
            backend_refs: vec![BackendRef {
                weight: None,
                inner: BackendObjectReference {
                    name: format!("default-{}", n),
                    group: Some("policy.linkerd.ip".to_string()),
                    namespace: Some("root".to_string()),
                    port: None,
                    kind: Some("EgressNetwork".to_string()),
                },
            }],
        });
    }
    rules
}
