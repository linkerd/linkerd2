use k8s_openapi::apimachinery::pkg::util::intstr::IntOrString;
use linkerd_policy_controller_k8s_api::{self as k8s};
use maplit::{btreemap, convert_args};

#[derive(Debug, Clone)]
pub struct Terminus {
    name: String,
    ns: String,
    percent_failure: usize,
}

impl Terminus {
    const PORT: i32 = 80;
    const PORT_NAME: &'static str = "http";
    const APP: &'static str = "bb-terminus";
    pub const SERVICE_NAME: &'static str = Self::APP;

    /// Builds a `bb-terminus` pod that can be configured to fail some (or all)
    /// requests.
    pub fn new(ns: impl ToString) -> Self {
        Self {
            name: "bb-terminus".to_string(),
            ns: ns.to_string(),
            percent_failure: 0,
        }
    }

    #[track_caller]
    pub fn percent_failure(self, percent_failure: usize) -> Self {
        assert!(percent_failure <= 100);
        Self {
            percent_failure,
            ..self
        }
    }

    /// Sets the `bb-terminus` pod's name. By default, the name is "bb-terminus".
    pub fn named(self, name: impl ToString) -> Self {
        Self {
            name: name.to_string(),
            ..self
        }
    }

    /// Returns the constructed [`k8s::Pod`].
    pub fn to_pod(self) -> k8s::Pod {
        let args = [
            "terminus",
            "--h1-server-port",
            "80",
            "--response-text",
            &self.name,
            "--percent-failure",
            &self.percent_failure.to_string(),
        ]
        .into_iter()
        .map(String::from)
        .collect();
        k8s::Pod {
            metadata: k8s::ObjectMeta {
                namespace: Some(self.ns),
                name: Some(self.name),
                annotations: Some(convert_args!(btreemap!(
                    "linkerd.io/inject" => "enabled",
                    "config.linkerd.io/proxy-log-level" => "linkerd=trace,info",
                ))),
                labels: Some(convert_args!(btreemap!(
                    "app" => Self::APP,
                ))),
                ..Default::default()
            },
            spec: Some(k8s::PodSpec {
                containers: vec![k8s::api::core::v1::Container {
                    name: "bb-terminus".to_string(),
                    image: Some("buoyantio/bb:latest".to_string()),
                    ports: Some(vec![k8s::api::core::v1::ContainerPort {
                        name: Some(Self::PORT_NAME.to_string()),
                        container_port: Self::PORT,
                        ..Default::default()
                    }]),
                    args: Some(args),
                    ..Default::default()
                }],
                ..Default::default()
            }),
            ..k8s::Pod::default()
        }
    }

    /// Returns a ClusterIP [`k8s::api::core::v1::Service`] that selects pods
    /// with the label `app: bb-terminus`.
    pub fn service(ns: &str) -> k8s::api::core::v1::Service {
        k8s::api::core::v1::Service {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some(Self::SERVICE_NAME.to_string()),
                ..Default::default()
            },
            spec: Some(k8s::api::core::v1::ServiceSpec {
                type_: Some("ClusterIP".to_string()),
                selector: Some(convert_args!(btreemap!(
                    "app" => Self::APP,
                ))),
                ports: Some(vec![k8s::api::core::v1::ServicePort {
                    port: Self::PORT,
                    target_port: Some(IntOrString::String(Self::PORT_NAME.to_string())),
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..Default::default()
        }
    }
}
