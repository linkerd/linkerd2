use k8s_openapi::apimachinery::pkg::util::intstr::IntOrString;
use linkerd_policy_controller_k8s_api::{self as k8s};
use maplit::{btreemap, convert_args};

pub fn pod(ns: &str) -> k8s::Pod {
    k8s::Pod {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("web".to_string()),
            annotations: Some(convert_args!(btreemap!(
                "linkerd.io/inject" => "enabled",
                "config.linkerd.io/proxy-log-level" => "linkerd=trace,info",
            ))),
            labels: Some(convert_args!(btreemap!(
                "app" => "web",
            ))),
            ..Default::default()
        },
        spec: Some(k8s::PodSpec {
            containers: vec![k8s::api::core::v1::Container {
                name: "hokay".to_string(),
                image: Some("ghcr.io/olix0r/hokay:latest".to_string()),
                ports: Some(vec![k8s::api::core::v1::ContainerPort {
                    name: Some("http".to_string()),
                    container_port: 8080,
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
            name: Some("web".to_string()),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            selector: k8s::policy::server::Selector::Pod(k8s::labels::Selector::from_iter(Some((
                "app", "web",
            )))),
            port: k8s::policy::server::Port::Name("http".to_string()),
            proxy_protocol: Some(k8s::policy::server::ProxyProtocol::Http1),
        },
    }
}

pub fn service(ns: &str) -> k8s::api::core::v1::Service {
    k8s::api::core::v1::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some("web".to_string()),
            ..Default::default()
        },
        spec: Some(k8s::api::core::v1::ServiceSpec {
            type_: Some("ClusterIP".to_string()),
            selector: Some(convert_args!(btreemap!(
                "app" => "web"
            ))),
            ports: Some(vec![k8s::api::core::v1::ServicePort {
                port: 80,
                target_port: Some(IntOrString::String("http".to_string())),
                ..Default::default()
            }]),
            ..Default::default()
        }),
        ..Default::default()
    }
}
