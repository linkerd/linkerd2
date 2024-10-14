use linkerd_policy_controller_k8s_api::{
    self as api,
    policy::{EgressNetwork, EgressNetworkSpec, Network, TrafficPolicy},
};
use linkerd_policy_test::admission;

#[tokio::test(flavor = "current_thread")]
async fn accepts_valid() {
    admission::accepts(|ns| EgressNetwork {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: EgressNetworkSpec {
            traffic_policy: TrafficPolicy::AllowAll,
            networks: Some(vec![
                Network {
                    cidr: "10.1.0.0/24".parse().unwrap(),
                    except: None,
                },
                Network {
                    cidr: "10.1.1.0/24".parse().unwrap(),
                    except: Some(vec!["10.1.1.0/28".parse().unwrap()]),
                },
            ]),
        },
        status: None,
    })
    .await;
}

#[tokio::test(flavor = "current_thread")]
async fn rejects_empty_networks() {
    admission::rejects(|ns| EgressNetwork {
        metadata: api::ObjectMeta {
            namespace: Some(ns),
            name: Some("test".to_string()),
            ..Default::default()
        },
        spec: EgressNetworkSpec {
            traffic_policy: TrafficPolicy::AllowAll,
            networks: Some(Default::default()),
        },
        status: None,
    })
    .await;
}
