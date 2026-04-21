#![cfg(feature = "gateway-api-experimental")]

use linkerd_policy_controller_k8s_api::{self as api, gateway};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid_egress_network() {
    admission::accepts(|ns| gateway::TLSRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: gateway::TLSRouteSpec {
            hostnames: None,
            parent_refs: Some(vec![gateway::TLSRouteParentRefs {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                namespace: Some(ns.to_string()),
                name: "my-egress-net".to_string(),
                section_name: None,
                port: Some(555),
            }]),
            rules: rules(1),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_egress_network_parent_with_no_port() {
    admission::rejects(|ns| gateway::TLSRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: gateway::TLSRouteSpec {
            hostnames: None,
            parent_refs: Some(vec![gateway::TLSRouteParentRefs {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                namespace: Some(ns.to_string()),
                name: "my-egress-net".to_string(),
                section_name: None,
                port: None,
            }]),
            rules: rules(1),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_if_more_than_one_rule() {
    admission::rejects(|ns| gateway::TLSRoute {
        metadata: api::ObjectMeta {
            namespace: Some(ns.clone()),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: gateway::TLSRouteSpec {
            hostnames: None,
            parent_refs: Some(vec![gateway::TLSRouteParentRefs {
                group: Some("policy.linkerd.io".to_string()),
                kind: Some("EgressNetwork".to_string()),
                namespace: Some(ns.to_string()),
                name: "my-egress-net".to_string(),
                section_name: None,
                port: Some(555),
            }]),
            rules: rules(2),
        },
        status: None,
    })
    .await;
}

fn rules(n: u16) -> Vec<gateway::TLSRouteRules> {
    let mut rules = Vec::default();
    for n in 1..=n {
        rules.push(gateway::TLSRouteRules {
            name: None,
            backend_refs: Some(vec![gateway::TLSRouteRulesBackendRefs {
                weight: None,
                name: format!("default-{n}"),
                group: Some("policy.linkerd.ip".to_string()),
                namespace: Some("root".to_string()),
                port: None,
                kind: Some("EgressNetwork".to_string()),
            }]),
        });
    }
    rules
}
