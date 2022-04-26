use linkerd_policy_controller_k8s_api::{self as k8s};
use maplit::{btreemap, convert_args};

pub fn pod(ns: &str) -> k8s::Pod {
    k8s::Pod {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("nginx".to_string()),
            annotations: Some(convert_args!(btreemap!(
                "linkerd.io/inject" => "enabled",
                "config.linkerd.io/proxy-log-level" => "linkerd=trace,info",
            ))),
            labels: Some(convert_args!(btreemap!(
                "app" => "nginx",
            ))),
            ..Default::default()
        },
        spec: Some(k8s::PodSpec {
            containers: vec![k8s::api::core::v1::Container {
                name: "nginx".to_string(),
                image: Some("docker.io/library/nginx:latest".to_string()),
                ports: Some(vec![k8s::api::core::v1::ContainerPort {
                    container_port: 80,
                    ..Default::default()
                }]),
                ..Default::default()
            }],
            ..Default::default()
        }),
        ..k8s::Pod::default()
    }
}

pub fn server(ns: &str) -> k8s::policy::Server {
    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("nginx".to_string()),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            pod_selector: k8s::labels::Selector::from_iter(Some(("app", "nginx"))),
            port: k8s::policy::server::Port::Number(80),
            proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
        },
    }
}

pub fn service(ns: &str) -> k8s::api::core::v1::Service {
    k8s::api::core::v1::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("nginx".to_string()),
            ..Default::default()
        },
        spec: Some(k8s::api::core::v1::ServiceSpec {
            type_: Some("ClusterIP".to_string()),
            selector: Some(convert_args!(btreemap!(
                "app" => "nginx"
            ))),
            ports: Some(vec![k8s::api::core::v1::ServicePort {
                port: 80,
                ..Default::default()
            }]),
            ..Default::default()
        }),
        ..Default::default()
    }
}
