use linkerd_policy_controller_k8s_api::{self as k8s_core_api, policy as linkerd_k8s_api};

mod grpc;
mod http;

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
        },
    }
}
