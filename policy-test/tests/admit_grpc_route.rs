use linkerd_policy_controller_k8s_api::{self as api, gateway};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_egress_network() {
    admission::accepts(|ns| gateway::GRPCRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: gateway::GrpcRouteSpec {
            parent_refs: Some(vec![gateway::GrpcRouteParentRefs {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                namespace: Some(ns.to_string()),
                name: "my-egress-net".to_string(),
                section_name: None,
                port: Some(555),
            }]),
            hostnames: None,
            rules: Some(rules()),
            use_default_gateways: None,
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_egress_network_parent_with_no_port() {
    admission::rejects(|ns| gateway::GRPCRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: gateway::GrpcRouteSpec {
            parent_refs: Some(vec![gateway::GrpcRouteParentRefs {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                namespace: Some(ns.to_string()),
                name: "my-egress-net".to_string(),
                section_name: None,
                port: None,
            }]),
            hostnames: None,
            rules: Some(rules()),
            use_default_gateways: None,
        },
        status: None,
    })
    .await;
}

fn rules() -> Vec<gateway::GrpcRouteRules> {
    vec![gateway::GrpcRouteRules {
        name: None,
        matches: Some(vec![gateway::GrpcRouteRulesMatches {
            method: Some(gateway::GrpcRouteRulesMatchesMethod {
                method: Some("foo".to_string()),
                service: Some("boo".to_string()),
                r#type: Some(gateway::GrpcRouteRulesMatchesMethodType::Exact),
            }),
            ..Default::default()
        }]),
        filters: None,
        backend_refs: None,
        session_persistence: None,
    }]
}
