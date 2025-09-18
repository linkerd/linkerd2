use super::{create, create_ready_pod, LinkerdInject};
use kube::{api::LogParams, ResourceExt};
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

/// A handle to a running `curl` container which can perform multiple requests
/// using `kubectl exec`.
#[derive(Clone)]
pub struct Execable {
    running: Running,
    api: kube::Api<k8s::Pod>,
}

impl Runner {
    // @TODO(alpeb): point to `latest` once the fix for this bug is released:
    // https://github.com/curl/curl/issues/17554
    const CURL_IMAGE: &'static str = "docker.io/curlimages/curl:8.13.0";

    pub async fn init(client: &kube::Client, ns: &str) -> Runner {
        let runner = Runner {
            namespace: ns.to_string(),
            client: client.clone(),
        };
        tokio::time::timeout(tokio::time::Duration::from_secs(60), runner.create_rbac())
            .await
            .expect("must create RBAC within a minute");

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
        let api = kube::Api::<k8s::api::core::v1::ConfigMap>::namespaced(
            self.client.clone(),
            &self.namespace,
        );
        kube::runtime::wait::delete::delete_and_finalize(
            api,
            "curl-lock",
            &kube::api::DeleteParams::foreground(),
        )
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

    /// Runs a [`k8s::Pod`] with a `curl` container which can execute HTTP
    /// requests using `kubectl exec`.
    ///
    /// The pod:
    /// - has the `linkerd.io/inject` annotation set, based on the
    ///   `linkerd_inject` parameter;
    /// - does not execute any requests unless [`Execable::get`] or
    ///   [`Execable::exec`] are called on the returned [`Execable`] handle.
    pub async fn run_execable(&self, name: &str, inject: LinkerdInject) -> Execable {
        let pod = k8s::Pod {
            metadata: Self::curl_meta(&self.namespace, name, inject),
            spec: Some(k8s::PodSpec {
                service_account: Some("curl".to_string()),
                containers: vec![k8s::api::core::v1::Container {
                    name: "curl".to_string(),
                    image: Some(Self::CURL_IMAGE.to_string()),
                    command: Some(vec!["sleep".to_string(), "infinite".to_string()]),
                    ..Default::default()
                }],
                restart_policy: Some("Never".to_string()),
                ..Default::default()
            }),
            ..k8s::Pod::default()
        };
        create_ready_pod(&self.client, pod).await;
        let running = Running {
            client: self.client.clone(),
            namespace: self.namespace.clone(),
            name: name.to_string(),
        };
        running.inits_complete().await;
        let api = kube::Api::<k8s::Pod>::namespaced(running.client.clone(), &running.namespace);
        Execable { running, api }
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
        super::await_service_account(&self.client, &self.namespace, "curl").await;

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
            metadata: Self::curl_meta(ns, name, inject),
            spec: Some(k8s::PodSpec {
                service_account: Some("curl".to_string()),
                init_containers: Some(vec![k8s::api::core::v1::Container {
                    name: "wait-for-web".to_string(),
                    image: Some("docker.io/chainguard/kubectl:latest-dev".to_string()),
                    // In CI, we can hit failures where the watch isn't updated
                    // after the configmap is deleted, even with a long timeout.
                    // Instead, we use a relatively short timeout and retry the
                    // wait to get a better chance.
                    command: Some(vec!["sh".to_string(), "-c".to_string()]),
                    args: Some(vec![format!(
                        "for i in $(seq 12) ; do \
                            echo waiting 10s for curl-lock to be deleted ; \
                            if kubectl wait --timeout=10s --for=delete --namespace={ns} cm/curl-lock ; then \
                                echo curl-lock deleted ; \
                                exit 0 ; \
                            fi ; \
                        done ; \
                        exit 1"
                    )]),
                    ..Default::default()
                }]),
                containers: vec![k8s::api::core::v1::Container {
                    name: "curl".to_string(),
                    image: Some(Self::CURL_IMAGE.to_string()),
                    args: Some(
                        vec!["curl", "-sf", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "--retry", "10", "--retry-delay", "2", target_url]
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

    fn curl_meta(ns: &str, name: &str, inject: LinkerdInject) -> k8s::ObjectMeta {
        k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            annotations: Some(convert_args!(btreemap!(
                "linkerd.io/inject" => inject.to_string(),
                "config.linkerd.io/proxy-log-level" => "linkerd=trace,info",
            ))),
            ..Default::default()
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

    async fn finished(&self, api: &kube::Api<k8s::Pod>) -> k8s::Pod {
        tracing::debug!(ns = %self.namespace, pod = %self.name, "Waiting for exit code");

        let finished = kube::runtime::wait::await_condition(
            api.clone(),
            &self.name,
            |obj: Option<&k8s::Pod>| -> bool { obj.and_then(get_exit_code).is_some() },
        );
        let pod = match time::timeout(time::Duration::from_secs(60), finished).await {
            Ok(Ok(Some(pod))) => pod,
            Ok(Ok(None)) => unreachable!("Condition must wait for pod"),
            Ok(Err(error)) => panic!("Failed to wait for exit code: {}: {}", self.name, error),
            Err(_timeout) => {
                panic!("Timeout waiting for exit code: {}", self.name);
            }
        };

        let code = get_exit_code(&pod).expect("curl pod must have an exit code");
        tracing::debug!(pod = %self.name, %code, "Curl exited");
        for c in &pod.spec.as_ref().unwrap().containers {
            super::logs(&self.client, &self.namespace, &self.name, &c.name).await;
        }

        pod
    }

    /// Waits for the curl container to complete and returns its exit code.
    pub async fn exit_code(self) -> i32 {
        self.inits_complete().await;
        let api = kube::Api::namespaced(self.client.clone(), &self.namespace);

        let pod = self.finished(&api).await;
        let code = get_exit_code(&pod).expect("curl pod must have an exit code");

        if let Err(error) = api
            .delete(&self.name, &kube::api::DeleteParams::foreground())
            .await
        {
            tracing::trace!(%error, name = %self.name, "Failed to delete pod");
        }

        code
    }

    /// Waits for the curl container to complete and returns the http status
    /// code.
    pub async fn http_status_code(self) -> hyper::StatusCode {
        self.inits_complete().await;
        let api = kube::Api::namespaced(self.client.clone(), &self.namespace);

        let pod = self.finished(&api).await;
        let log = api
            .logs(
                &pod.name_unchecked(),
                &LogParams {
                    container: Some("curl".to_string()),
                    ..Default::default()
                },
            )
            .await
            .expect("must be able to get curl logs");

        if let Err(error) = api
            .delete(&self.name, &kube::api::DeleteParams::foreground())
            .await
        {
            tracing::trace!(%error, name = %self.name, "Failed to delete pod");
        }

        let status_code = log.lines().last().expect("curl must emit a status code");
        hyper::StatusCode::try_from(status_code).expect("curl must emit a valid status code")
    }

    async fn inits_complete(&self) {
        let api = kube::Api::namespaced(self.client.clone(), &self.namespace);
        let init_complete = kube::runtime::wait::await_condition(
            api,
            &self.name,
            |pod: Option<&k8s::Pod>| -> bool {
                if let Some(pod) = pod {
                    if let Some(status) = pod.status.as_ref() {
                        return status.init_container_statuses.iter().flatten().all(|init| {
                            init.state
                                .as_ref()
                                .map(|s| s.terminated.is_some())
                                .unwrap_or(false)
                        });
                    }
                }
                false
            },
        );

        match time::timeout(time::Duration::from_secs(120), init_complete).await {
            Ok(Ok(_pod)) => {}
            Ok(Err(error)) => panic!("Failed to watch pod status: {}: {}", self.name, error),
            Err(_timeout) => {
                panic!(
                    "Timeout waiting for init containers to complete: {}",
                    self.name
                );
            }
        };
    }
}

impl Execable {
    /// Execute the provided `command` in the `curl` pod, returning the process'
    /// stdout as a `String`.
    pub async fn exec<I, T>(&self, command: I) -> Result<String, Box<dyn std::error::Error>>
    where
        I: IntoIterator<Item = T> + std::fmt::Debug,
        T: Into<String>,
    {
        use tokio::io::AsyncReadExt;
        tracing::debug!(?command, "curl::exec");
        let mut process = self
            .api
            .exec(
                &self.running.name,
                command,
                &kube::api::AttachParams {
                    container: Some("curl".to_string()),
                    stdout: true,
                    stderr: true,
                    ..Default::default()
                },
            )
            .await
            .expect("must be able to exec");
        let mut stdout = process.stdout().expect("AttachParams should have stdout");
        let mut stderr = process.stderr().expect("AttachParams should have stderr");
        process.join().await?;
        let mut stdout_buf = String::new();
        stdout.read_to_string(&mut stdout_buf).await?;
        let mut stderr_buf = String::new();
        match stderr.read_to_string(&mut stderr_buf).await {
            Ok(_) => tracing::debug!("curl stderr:\n{stderr_buf}"),
            Err(error) => tracing::warn!("Failed to read curl stderr: {error}"),
        }
        Ok(stdout_buf)
    }

    /// Execute an HTTP GET request for the provided `target_url` in the `curl` pod, returning the
    /// status code.
    #[tracing::instrument(
        level = "debug",
        name = "curl::get",
        skip(self),
        ret(Display),
        err(Display)
    )]
    pub async fn get(
        &self,
        target_url: &str,
    ) -> Result<hyper::StatusCode, Box<dyn std::error::Error>> {
        let command = [
            "curl",
            "-sfv",
            "-o",
            "/dev/null",
            "-w",
            "%{http_code}",
            "--max-time",
            "10",
            target_url,
        ];
        self.exec(command)
            .await?
            .parse::<hyper::StatusCode>()
            .map_err(Into::into)
    }
}

fn get_exit_code(pod: &k8s::Pod) -> Option<i32> {
    let c = pod
        .status
        .as_ref()?
        .container_statuses
        .as_ref()?
        .iter()
        .find(|c| c.name == "curl")?;
    let code = c.state.as_ref()?.terminated.as_ref()?.exit_code;
    Some(code)
}
