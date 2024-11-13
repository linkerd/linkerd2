use linkerd_policy_controller_core::IpNet;
use linkerd_policy_controller_k8s_api::{self as k8s_core_api, policy as linkerd_k8s_api};
mod egress_network;
mod ratelimit;
mod routes;

pub fn default_cluster_networks() -> Vec<IpNet> {
    vec![
        "10.0.0.0/8".parse().unwrap(),
        "100.64.0.0/10".parse().unwrap(),
        "172.16.0.0/12".parse().unwrap(),
        "192.168.0.0/16".parse().unwrap(),
        "fd00::/8".parse().unwrap(),
    ]
}

pub fn make_server(
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
