use k8s_gateway_api::{
    CommonRouteSpec, GrpcMethodMatch, GrpcRoute, GrpcRouteMatch, GrpcRouteRule, GrpcRouteSpec,
};
use linkerd_policy_controller_k8s_api::{self as api};
use linkerd_policy_test::{admission, egress_network_parent_ref};

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_egress_network() {
    admission::accepts(|ns| GrpcRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: GrpcRouteSpec {
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
    admission::rejects(|ns| GrpcRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: GrpcRouteSpec {
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

fn rules() -> Vec<GrpcRouteRule> {
    vec![GrpcRouteRule {
        matches: Some(vec![GrpcRouteMatch {
            method: Some(GrpcMethodMatch::Exact {
                method: Some("/foo".to_string()),
                service: Some("boo".to_string()),
            }),
            ..GrpcRouteMatch::default()
        }]),
        filters: None,
        backend_refs: None,
    }]
}
