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
        spec: gateway::GRPCRouteSpec {
            parent_refs: Some(vec![gateway::GRPCRouteParentRefs {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                namespace: Some(ns.to_string()),
                name: "my-egress-net".to_string(),
                section_name: None,
                port: Some(555),
            }]),
            hostnames: None,
            rules: Some(rules()),
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
        spec: gateway::GRPCRouteSpec {
            parent_refs: Some(vec![gateway::GRPCRouteParentRefs {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                namespace: Some(ns.to_string()),
                name: "my-egress-net".to_string(),
                section_name: None,
                port: None,
            }]),
            hostnames: None,
            rules: Some(rules()),
        },
        status: None,
    })
    .await;
}

fn rules() -> Vec<gateway::GRPCRouteRules> {
    vec![gateway::GRPCRouteRules {
        name: None,
        matches: Some(vec![gateway::GRPCRouteRulesMatches {
            method: Some(gateway::GRPCRouteRulesMatchesMethod {
                method: Some("foo".to_string()),
                service: Some("boo".to_string()),
                r#type: Some(gateway::GRPCRouteRulesMatchesMethodType::Exact),
            }),
            ..Default::default()
        }]),
        filters: None,
        backend_refs: None,
        session_persistence: None,
    }]
}
