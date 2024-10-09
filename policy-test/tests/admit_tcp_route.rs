use linkerd_policy_controller_k8s_api::{
    self as api,
    gateway::{
        BackendObjectReference, BackendRef, CommonRouteSpec, TcpRoute, TcpRouteRule, TcpRouteSpec,
    },
};
use linkerd_policy_test::{admission, unmeshed_network_parent_ref};

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_unmeshed_network() {
    admission::accepts(|ns| TcpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: TcpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![unmeshed_network_parent_ref(ns, Some(555))]),
            },
            rules: rules(1),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_unmeshed_network_parent_with_no_port() {
    admission::rejects(|ns| TcpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: TcpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![unmeshed_network_parent_ref(ns, None)]),
            },
            rules: rules(1),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_if_more_than_one_rule() {
    admission::rejects(|ns| TcpRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: TcpRouteSpec {
            inner: CommonRouteSpec {
                parent_refs: Some(vec![unmeshed_network_parent_ref(ns, Some(555))]),
            },
            rules: rules(2),
        },
        status: None,
    })
    .await;
}

fn rules(n: u16) -> Vec<TcpRouteRule> {
    let mut rules = Vec::default();
    for n in 1..=n {
        rules.push(TcpRouteRule {
            backend_refs: vec![BackendRef {
                weight: None,
                inner: BackendObjectReference {
                    name: format!("default-{}", n),
                    group: Some("policy.linkerd.ip".to_string()),
                    namespace: Some("root".to_string()),
                    port: None,
                    kind: Some("UnmeshedNetwork".to_string()),
                },
            }],
        });
    }
    rules
}
