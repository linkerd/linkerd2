#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod admission;
pub mod curl;
pub mod grpc;
pub mod web;

use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};
use maplit::{btreemap, convert_args};
use tokio::time;
use tracing::Instrument;

#[derive(Copy, Clone, Debug)]
pub enum LinkerdInject {
    Enabled,
    Disabled,
}

/// Creates a cluster-scoped resource.
async fn create_cluster_scoped<T>(client: &kube::Client, obj: T) -> T
where
    T: kube::Resource<Scope = kube::core::ClusterResourceScope>,
    T: serde::Serialize + serde::de::DeserializeOwned + Clone + std::fmt::Debug,
    T::DynamicType: Default,
{
    let params = kube::api::PostParams {
        field_manager: Some("linkerd-policy-test".to_string()),
        ..Default::default()
    };
    let api = kube::Api::<T>::all(client.clone());
    tracing::trace!(?obj, "Creating");
    api.create(&params, &obj)
        .await
        .expect("failed to create resource")
}

/// Creates a namespace-scoped resource.
pub async fn create<T>(client: &kube::Client, obj: T) -> T
where
    T: kube::Resource<Scope = kube::core::NamespaceResourceScope>,
    T: serde::Serialize + serde::de::DeserializeOwned + Clone + std::fmt::Debug,
    T::DynamicType: Default,
{
    let params = kube::api::PostParams {
        field_manager: Some("linkerd-policy-test".to_string()),
        ..Default::default()
    };
    let api = obj
        .namespace()
        .map(|ns| kube::Api::<T>::namespaced(client.clone(), &*ns))
        .unwrap_or_else(|| kube::Api::<T>::default_namespaced(client.clone()));
    tracing::trace!(?obj, "Creating");
    api.create(&params, &obj)
        .await
        .expect("failed to create resource")
}

pub async fn await_condition<T>(
    client: &kube::Client,
    ns: &str,
    name: &str,
    cond: impl kube::runtime::wait::Condition<T>,
) -> Option<T>
where
    T: kube::Resource<Scope = kube::core::NamespaceResourceScope>,
    T: serde::Serialize + serde::de::DeserializeOwned + Clone + std::fmt::Debug + Send + 'static,
    T::DynamicType: Default,
{
    let api = kube::Api::namespaced(client.clone(), ns);
    time::timeout(
        time::Duration::from_secs(60),
        kube::runtime::wait::await_condition(api, name, cond),
    )
    .await
    .expect("condition timed out")
    .expect("API call failed")
}

/// Creates a pod and waits for all of its containers to be ready.
pub async fn create_ready_pod(client: &kube::Client, pod: k8s::Pod) -> k8s::Pod {
    let pod_ready = |obj: Option<&k8s::Pod>| -> bool {
        if let Some(pod) = obj {
            if let Some(status) = &pod.status {
                if let Some(containers) = &status.container_statuses {
                    return containers.iter().all(|c| c.ready);
                }
            }
        }
        false
    };

    let pod = create(client, pod).await;
    let pod = await_condition(
        client,
        &pod.namespace().unwrap(),
        &pod.name_unchecked(),
        pod_ready,
    )
    .await
    .unwrap();

    tracing::trace!(
        pod = %pod.name_any(),
        ip = %pod
            .status.as_ref().expect("pod must have a status")
            .pod_ips.as_ref().unwrap()[0]
            .ip.as_deref().expect("pod ip must be set"),
        containers = ?pod
            .spec.as_ref().expect("pod must have a spec")
            .containers.iter().map(|c| &*c.name).collect::<Vec<_>>(),
        "Ready",
    );

    pod
}

pub async fn await_pod_ip(client: &kube::Client, ns: &str, name: &str) -> std::net::IpAddr {
    fn get_ip(pod: &k8s::Pod) -> Option<String> {
        pod.status.as_ref()?.pod_ip.clone()
    }

    let pod = await_condition(client, ns, name, |obj: Option<&k8s::Pod>| -> bool {
        if let Some(pod) = obj {
            return get_ip(pod).is_some();
        }
        false
    })
    .await
    .expect("must fetch pod");
    get_ip(&pod)
        .expect("pod must have an IP")
        .parse()
        .expect("pod IP must be valid")
}

#[tracing::instrument(skip_all, fields(%pod, %container))]
pub async fn logs(client: &kube::Client, ns: &str, pod: &str, container: &str) {
    let params = kube::api::LogParams {
        container: Some(container.to_string()),
        ..kube::api::LogParams::default()
    };
    let log = kube::Api::<k8s::Pod>::namespaced(client.clone(), ns)
        .logs(pod, &params)
        .await
        .expect("must fetch logs");
    for message in log.lines() {
        tracing::trace!(%message);
    }
}

/// Runs a test with a random namespace that is deleted on test completion
pub async fn with_temp_ns<F, Fut>(test: F)
where
    F: FnOnce(kube::Client, String) -> Fut,
    Fut: std::future::Future<Output = ()> + Send + 'static,
{
    let _tracing = init_tracing();

    let context = std::env::var("POLICY_TEST_CONTEXT").ok();
    tracing::trace!(?context, "Initializing client");
    let client = match context {
        None => kube::Client::try_default()
            .await
            .expect("must initialize kubernetes client"),
        Some(context) => {
            let opts = kube::config::KubeConfigOptions {
                context: Some(context),
                cluster: None,
                user: None,
            };
            kube::Config::from_kubeconfig(&opts)
                .await
                .expect("must initialize kubernetes client config")
                .try_into()
                .expect("must initialize kubernetes client")
        }
    };

    let api = kube::Api::<k8s::Namespace>::all(client.clone());

    let name = format!("linkerd-policy-test-{}", random_suffix(6));
    tracing::debug!(namespace = %name, "Creating");
    let ns = create_cluster_scoped(
        &client,
        k8s::Namespace {
            metadata: k8s::ObjectMeta {
                name: Some(name),
                labels: Some(convert_args!(btreemap!(
                    "linkerd-policy-test" => std::thread::current().name().unwrap_or(""),
                ))),
                ..Default::default()
            },
            ..Default::default()
        },
    )
    .await;
    tracing::trace!(?ns);
    tokio::time::timeout(
        tokio::time::Duration::from_secs(60),
        await_service_account(&client, &ns.name_unchecked(), "default"),
    )
    .await
    .expect("Timed out waiting for a serviceaccount");

    tracing::trace!("Spawning");
    let ns_name = ns.name_unchecked();
    let test = test(client.clone(), ns_name.clone());
    let res = tokio::spawn(test.instrument(tracing::info_span!("test", ns = %ns_name))).await;
    if res.is_err() {
        // If the test failed, list the state of all pods/containers in the namespace.
        let pods = kube::Api::<k8s::Pod>::namespaced(client.clone(), &ns_name)
            .list(&Default::default())
            .await
            .expect("Failed to get pod status");
        for p in pods.items {
            let pod = p.name_unchecked();
            if let Some(status) = p.status {
                let _span = tracing::info_span!("pod", ns = %ns_name, name = %pod).entered();
                tracing::trace!(reason = ?status.reason, message = ?status.message);
                for c in status.init_container_statuses.into_iter().flatten() {
                    tracing::trace!(init_container = %c.name, ready = %c.ready, state = ?c.state);
                }
                for c in status.container_statuses.into_iter().flatten() {
                    tracing::trace!(container = %c.name, ready = %c.ready, state = ?c.state);
                }
            }
        }

        // Then stop tracing so the log is not further polluted with more
        // information about cleanup after the failure was printed.
        drop(_tracing);
    }

    if std::env::var("POLICY_TEST_NO_CLEANUP").is_err() {
        tracing::debug!(ns = %ns.name_unchecked(), "Deleting");
        api.delete(&ns.name_unchecked(), &kube::api::DeleteParams::background())
            .await
            .expect("failed to delete Namespace");
    }
    if let Err(err) = res {
        std::panic::resume_unwind(err.into_panic());
    }
}

async fn await_service_account(client: &kube::Client, ns: &str, name: &str) {
    use futures::StreamExt;

    tracing::trace!(%name, %ns, "Waiting for serviceaccount");
    tokio::pin! {
        let sas = kube::runtime::watcher(
            kube::Api::<k8s::ServiceAccount>::namespaced(client.clone(), ns),
            kube::api::ListParams::default(),
        );
    }
    loop {
        let ev = sas
            .next()
            .await
            .expect("serviceaccounts watch must not end")
            .expect("serviceaccounts watch must not fail");
        tracing::info!(?ev);
        match ev {
            kube::runtime::watcher::Event::Restarted(sas) => {
                if sas.iter().any(|sa| sa.name_unchecked() == name) {
                    return;
                }
            }
            kube::runtime::watcher::Event::Applied(sa) => {
                if sa.name_unchecked() == name {
                    return;
                }
            }
            _ => {}
        }
    }

    // XXX In some versions of k8s, it may be appropriate to wait for the
    // ServiceAccount's secret to be created, but, as of v1.24,
    // ServiceAccounts don't have secrets.
}

pub fn random_suffix(len: usize) -> String {
    use rand::Rng;

    rand::thread_rng()
        .sample_iter(&LowercaseAlphanumeric)
        .take(len)
        .map(char::from)
        .collect()
}

fn init_tracing() -> tracing::subscriber::DefaultGuard {
    tracing::subscriber::set_default(
        tracing_subscriber::fmt()
            .with_test_writer()
            .with_thread_names(true)
            .without_time()
            .with_env_filter(
                tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                    "trace,tower=info,hyper=info,kube=info,h2=info"
                        .parse()
                        .unwrap()
                }),
            )
            .finish(),
    )
}

struct LowercaseAlphanumeric;

// Modified from `rand::distributions::Alphanumeric`
//
// Copyright 2018 Developers of the Rand project
// Copyright (c) 2014 The Rust Project Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
impl rand::distributions::Distribution<u8> for LowercaseAlphanumeric {
    fn sample<R: rand::Rng + ?Sized>(&self, rng: &mut R) -> u8 {
        const RANGE: u32 = 26 + 10;
        const CHARSET: &[u8] = b"abcdefghijklmnopqrstuvwxyz0123456789";
        loop {
            let var = rng.next_u32() >> (32 - 6);
            if var < RANGE {
                return CHARSET[var as usize];
            }
        }
    }
}

// === imp LinkerdInject ===

impl std::fmt::Display for LinkerdInject {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            LinkerdInject::Enabled => write!(f, "enabled"),
            LinkerdInject::Disabled => write!(f, "disabled"),
        }
    }
}
