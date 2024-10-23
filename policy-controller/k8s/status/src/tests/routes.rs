use linkerd_policy_controller_k8s_api::{
    self as k8s_core_api, gateway as k8s_gateway_api, policy as linkerd_k8s_api,
};

use crate::index::POLICY_API_GROUP;
use chrono::{DateTime, Utc};
use linkerd_policy_controller_core::POLICY_CONTROLLER_NAME;

mod grpc;
mod http;
mod tcp;
mod tls;

fn make_parent_status(
    namespace: impl ToString,
    name: impl ToString,
    type_: impl ToString,
    status: impl ToString,
    reason: impl ToString,
) -> k8s_gateway_api::RouteParentStatus {
    let condition = k8s_core_api::Condition {
        message: "".to_string(),
        type_: type_.to_string(),
        observed_generation: None,
        reason: reason.to_string(),
        status: status.to_string(),
        last_transition_time: k8s_core_api::Time(DateTime::<Utc>::MIN_UTC),
    };
    k8s_gateway_api::RouteParentStatus {
        conditions: vec![condition],
        parent_ref: k8s_gateway_api::ParentReference {
            port: None,
            section_name: None,
            name: name.to_string(),
            kind: Some("Server".to_string()),
            namespace: Some(namespace.to_string()),
            group: Some(POLICY_API_GROUP.to_string()),
        },
        controller_name: POLICY_CONTROLLER_NAME.to_string(),
    }
}

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

fn make_server(
    namespace: impl ToString,
    name: impl ToString,
    port: u16,
    srv_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    pod_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    proxy_protocol: Option<linkerd_k8s_api::server::ProxyProtocol>,
) -> linkerd_k8s_api::Server {
    let port = linkerd_k8s_api::server::Port::Number(port.try_into().unwrap());
    linkerd_k8s_api::Server {
        metadata: k8s_core_api::ObjectMeta {
            namespace: Some(namespace.to_string()),
            name: Some(name.to_string()),
            labels: Some(
                srv_labels
                    .into_iter()
                    .map(|(k, v)| (k.to_string(), v.to_string()))
                    .collect(),
            ),
            ..Default::default()
        },
        spec: linkerd_k8s_api::ServerSpec {
            port,
            selector: linkerd_k8s_api::server::Selector::Pod(pod_labels.into_iter().collect()),
            proxy_protocol,
            access_policy: None,
        },
    }
}
