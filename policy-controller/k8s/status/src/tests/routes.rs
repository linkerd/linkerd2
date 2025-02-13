use linkerd_policy_controller_k8s_api::{self as k8s_core_api, policy as linkerd_k8s_api};

mod grpc;
mod helpers;
mod http;
mod tcp;
mod tls;

fn make_service(
    namespace: impl ToString,
    name: impl ToString,
) -> k8s_core_api::api::core::v1::Service {
    k8s_core_api::api::core::v1::Service {
        metadata: k8s_core_api::ObjectMeta {
            namespace: Some(namespace.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: Some(k8s_core_api::ServiceSpec {
            cluster_ip: Some("1.2.3.4".to_string()),
            cluster_ips: Some(vec!["1.2.3.4".to_string()]),
            ..Default::default()
        }),
        status: None,
    }
}

fn make_egress_network(
    namespace: impl ToString,
    name: impl ToString,
    condition: k8s_core_api::Condition,
) -> linkerd_k8s_api::EgressNetwork {
    linkerd_k8s_api::EgressNetwork {
        metadata: k8s_core_api::ObjectMeta {
            namespace: Some(namespace.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: linkerd_k8s_api::EgressNetworkSpec {
            networks: Some(vec![linkerd_k8s_api::Network {
                cidr: "0.0.0.0/0".parse().unwrap(),
                except: Some(vec![
                    "10.0.0.0/8".parse().unwrap(),
                    "100.64.0.0/10".parse().unwrap(),
                    "172.16.0.0/12".parse().unwrap(),
                    "192.168.0.0/16".parse().unwrap(),
                    "fd00::/8".parse().unwrap(),
                ]),
            }]),
            traffic_policy: linkerd_k8s_api::TrafficPolicy::Allow,
        },
        status: Some(linkerd_k8s_api::EgressNetworkStatus {
            conditions: vec![condition],
        }),
    }
}
