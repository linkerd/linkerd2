use super::{create, LinkerdInject};
use kube::ResourceExt;
use linkerd_policy_controller_k8s_api::{self as k8s};
use maplit::{btreemap, convert_args};
use tokio::time;

#[derive(Clone)]
#[must_use]
pub struct Runner {
    namespace: String,
    client: kube::Client,
}

#[derive(Clone)]
pub struct Running {
    namespace: String,
    name: String,
    client: kube::Client,
}

impl Runner {
    pub async fn init(client: &kube::Client, ns: &str) -> Runner {
        let runner = Runner {
            namespace: ns.to_string(),
            client: client.clone(),
        };
        runner.create_rbac().await;
        runner
    }

    /// Creates a configmap that prevents curl pods from executing.
    pub async fn create_lock(&self) {
        create(
            &self.client,
            k8s::api::core::v1::ConfigMap {
                metadata: k8s::ObjectMeta {
                    namespace: Some(self.namespace.clone()),
                    name: Some("curl-lock".to_string()),
                    ..Default::default()
                },
                ..Default::default()
            },
        )
        .await;
    }

    /// Deletes the lock configmap, allowing curl pods to execute.
    pub async fn delete_lock(&self) {
        tracing::trace!(ns = %self.namespace, "Deleting curl-lock");
        kube::Api::<k8s::api::core::v1::ConfigMap>::namespaced(
            self.client.clone(),
            &self.namespace,
        )
        .delete("curl-lock", &kube::api::DeleteParams::foreground())
        .await
        .expect("curl-lock must be deleted");
        tracing::debug!(ns = %self.namespace, "Deleted curl-lock");
    }

    /// Runs a [`k8s::Pod`] that runs curl against the provided target URL.
    ///
    /// The pod:
    /// - has the `linkerd.io/inject` annotation set, based on the
    ///   `linkerd_inject` parameter;
    /// - runs under the service account `curl`;
    /// - does not actually execute curl until the `curl-lock` configmap is not
    ///   present
    pub async fn run(&self, name: &str, target_url: &str, inject: LinkerdInject) -> Running {
        create(
            &self.client,
            Self::gen_pod(&self.namespace, name, target_url, inject),
        )
        .await;
        Running {
            client: self.client.clone(),
            namespace: self.namespace.clone(),
            name: name.to_string(),
        }
    }

    /// Creates a service account and RBAC to allow curl pods to watch the
    /// curl-lock configmap.
    async fn create_rbac(&self) {
        create(
            &self.client,
            k8s::api::core::v1::ServiceAccount {
                metadata: k8s::ObjectMeta {
                    namespace: Some(self.namespace.clone()),
                    name: Some("curl".to_string()),
                    ..Default::default()
                },
                ..Default::default()
            },
        )
        .await;

        create(
            &self.client,
            k8s::api::rbac::v1::Role {
                metadata: k8s::ObjectMeta {
                    namespace: Some(self.namespace.clone()),
                    name: Some("curl-lock".to_string()),
                    ..Default::default()
                },
                rules: Some(vec![k8s::api::rbac::v1::PolicyRule {
                    api_groups: Some(vec!["".to_string()]),
                    resources: Some(vec!["configmaps".to_string()]),
                    verbs: vec!["get".to_string(), "list".to_string(), "watch".to_string()],
                    ..Default::default()
                }]),
            },
        )
        .await;

        create(
            &self.client,
            k8s::api::rbac::v1::RoleBinding {
                metadata: k8s::ObjectMeta {
                    namespace: Some(self.namespace.clone()),
                    name: Some("curl-lock".to_string()),
                    ..Default::default()
                },
                role_ref: k8s::api::rbac::v1::RoleRef {
                    api_group: "rbac.authorization.k8s.io".to_string(),
                    kind: "Role".to_string(),
                    name: "curl-lock".to_string(),
                },
                subjects: Some(vec![k8s::api::rbac::v1::Subject {
                    kind: "ServiceAccount".to_string(),
                    name: "curl".to_string(),
                    namespace: Some(self.namespace.clone()),
                    ..Default::default()
                }]),
            },
        )
        .await;
    }

    fn gen_pod(ns: &str, name: &str, target_url: &str, inject: LinkerdInject) -> k8s::Pod {
        k8s::Pod {
            metadata: k8s::ObjectMeta {
                namespace: Some(ns.to_string()),
                name: Some(name.to_string()),
                annotations: Some(convert_args!(btreemap!(
                    "linkerd.io/inject" => inject.to_string(),
                    "config.linkerd.io/proxy-log-level" => "linkerd=trace,info",
                ))),
                ..Default::default()
            },
            spec: Some(k8s::PodSpec {
                service_account: Some("curl".to_string()),
                init_containers: Some(vec![k8s::api::core::v1::Container {
                    name: "wait-for-nginx".to_string(),
                    image: Some("docker.io/bitnami/kubectl:latest".to_string()),
                    args: Some(
                        vec![
                            "wait",
                            "--timeout=60s",
                            "--for=delete",
                            "--namespace",
                            ns,
                            "cm",
                            "curl-lock",
                        ]
                        .into_iter()
                        .map(Into::into)
                        .collect(),
                    ),
                    ..Default::default()
                }]),
                containers: vec![k8s::api::core::v1::Container {
                    name: "curl".to_string(),
                    image: Some("docker.io/curlimages/curl:latest".to_string()),
                    args: Some(
                        vec!["curl", "-sSfv", target_url]
                            .into_iter()
                            .map(Into::into)
                            .collect(),
                    ),
                    ..Default::default()
                }],
                restart_policy: Some("Never".to_string()),
                ..Default::default()
            }),
            ..k8s::Pod::default()
        }
    }
}

impl Running {
    pub fn name(&self) -> &str {
        &self.name
    }

    /// Waits for the pod to have an IP address and returns it.
    pub async fn ip(&self) -> std::net::IpAddr {
        super::await_pod_ip(&self.client, &self.namespace, &self.name).await
    }

    /// Waits for the curl container to complete and returns its exit code.
    pub async fn exit_code(self) -> i32 {
        fn get_exit_code(pod: &k8s::Pod) -> Option<i32> {
            let c = pod
                .status
                .as_ref()?
                .container_statuses
                .as_ref()?
                .iter()
                .find(|c| c.name == "curl")?;
            let code = c.state.as_ref()?.terminated.as_ref()?.exit_code;
            tracing::debug!(ns = %pod.namespace().unwrap(), pod = %pod.name(), %code, "Curl exited");
            Some(code)
        }

        tracing::debug!(ns = %self.namespace, pod = %self.name, "Waiting for exit code");
        let api = kube::Api::namespaced(self.client.clone(), &self.namespace);
        let finished = kube::runtime::wait::await_condition(
            api.clone(),
            &self.name,
            |obj: Option<&k8s::Pod>| -> bool { obj.and_then(get_exit_code).is_some() },
        );
        match time::timeout(time::Duration::from_secs(60), finished).await {
            Ok(Ok(())) => {}
            Ok(Err(error)) => panic!("Failed to wait for exit code: {}: {}", self.name, error),
            Err(_timeout) => panic!("Timeout waiting for exit code: {}", self.name),
        };

        let curl_pod = api.get(&self.name).await.expect("pod must exist");
        get_exit_code(&curl_pod).expect("curl pod must have an exit code")
    }
}
