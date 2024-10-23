#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod admission;
pub mod bb;
pub mod curl;
pub mod grpc;
pub mod outbound_api;
pub mod web;

use linkerd_policy_controller_k8s_api::{
    self as k8s, policy::httproute::ParentReference, ResourceExt,
};
use maplit::{btreemap, convert_args};
use tokio::time;
use tracing::Instrument;

#[derive(Copy, Clone, Debug)]
pub enum LinkerdInject {
    Enabled,
    Disabled,
}

/// Creates a cluster-scoped resource.
pub async fn create_cluster_scoped<T>(client: &kube::Client, obj: T) -> T
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

/// Creates a cluster-scoped resource.
pub async fn delete_cluster_scoped<T>(client: &kube::Client, obj: T)
where
    T: kube::Resource<Scope = kube::core::ClusterResourceScope>,
    T: serde::Serialize + serde::de::DeserializeOwned + Clone + std::fmt::Debug,
    T::DynamicType: Default,
{
    let params = kube::api::DeleteParams {
        ..Default::default()
    };
    let api = kube::Api::<T>::all(client.clone());
    tracing::trace!(?obj, "Deleting");
    api.delete(&obj.name_unchecked(), &params).await.unwrap();
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
        .map(|ns| kube::Api::<T>::namespaced(client.clone(), &ns))
        .unwrap_or_else(|| kube::Api::<T>::default_namespaced(client.clone()));
    tracing::trace!(?obj, "Creating");
    api.create(&params, &obj)
        .await
        .expect("failed to create resource")
}

/// Updates a namespace-scoped resource.
pub async fn update<T>(client: &kube::Client, mut new: T) -> T
where
    T: kube::Resource<Scope = kube::core::NamespaceResourceScope>,
    T: serde::Serialize + serde::de::DeserializeOwned + Clone + std::fmt::Debug,
    T::DynamicType: Default,
{
    let params = kube::api::PostParams {
        field_manager: Some("linkerd-policy-test".to_string()),
        ..Default::default()
    };
    let api = new
        .namespace()
        .map(|ns| kube::Api::<T>::namespaced(client.clone(), &ns))
        .unwrap_or_else(|| kube::Api::<T>::default_namespaced(client.clone()));

    let old = api.get_metadata(&new.name_unchecked()).await.unwrap();

    new.meta_mut().resource_version = old.resource_version();
    tracing::trace!(?new, "Updating");
    api.replace(&new.name_unchecked(), &params, &new)
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

// Waits until an HttpRoute with the given namespace and name has a status set
// on it, then returns the generic route status representation.
pub async fn await_route_status(
    client: &kube::Client,
    ns: &str,
    name: &str,
) -> k8s::policy::httproute::RouteStatus {
    use k8s::policy::httproute as api;
    let route_status = await_condition(client, ns, name, |obj: Option<&api::HttpRoute>| -> bool {
        obj.and_then(|route| route.status.as_ref()).is_some()
    })
    .await
    .expect("must fetch route")
    .status
    .expect("route must contain a status representation")
    .inner;
    tracing::trace!(?route_status, name, ns, "got route status");
    route_status
}

// Wait for the endpoints controller to populate the Endpoints resource.
pub fn endpoints_ready(obj: Option<&k8s::Endpoints>) -> bool {
    if let Some(ep) = obj {
        return ep
            .subsets
            .iter()
            .flatten()
            .flat_map(|s| &s.addresses)
            .any(|a| !a.is_empty());
    }
    false
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

/// Creates a service resource.
pub async fn create_service(
    client: &kube::Client,
    ns: &str,
    name: &str,
    port: i32,
) -> k8s::Service {
    let svc = mk_service(ns, name, port);

    create(client, svc).await
}

/// Creates a service resource.
pub async fn create_opaque_service(
    client: &kube::Client,
    ns: &str,
    name: &str,
    port: i32,
) -> k8s::Service {
    let svc = mk_service(ns, name, port);
    let svc = annotate_service(
        svc,
        std::iter::once(("config.linkerd.io/opaque-ports", port)),
    );

    create(client, svc).await
}

/// Creates a service resource with given annotations.
pub async fn create_annotated_service(
    client: &kube::Client,
    ns: &str,
    name: &str,
    port: i32,
    annotations: std::collections::BTreeMap<String, String>,
) -> k8s::Service {
    let svc = annotate_service(mk_service(ns, name, port), annotations);
    create(client, svc).await
}

pub fn annotate_service<K, V>(
    mut svc: k8s::Service,
    annotations: impl IntoIterator<Item = (K, V)>,
) -> k8s::Service
where
    K: ToString,
    V: ToString,
{
    svc.annotations_mut().extend(
        annotations
            .into_iter()
            .map(|(k, v)| (k.to_string(), v.to_string())),
    );
    svc
}

pub fn mk_service(ns: &str, name: &str, port: i32) -> k8s::Service {
    k8s::Service {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: Some(k8s::ServiceSpec {
            ports: Some(vec![k8s::ServicePort {
                port,
                ..Default::default()
            }]),
            ..Default::default()
        }),
        ..k8s::Service::default()
    }
}

#[track_caller]
pub fn assert_svc_meta(meta: &Option<grpc::meta::Metadata>, svc: &k8s::Service, port: u16) {
    tracing::debug!(?meta, ?svc, port, "Asserting service metadata");
    assert_eq!(
        meta,
        &Some(grpc::meta::Metadata {
            kind: Some(grpc::meta::metadata::Kind::Resource(grpc::meta::Resource {
                group: "core".to_string(),
                kind: "Service".to_string(),
                name: svc.name_unchecked(),
                namespace: svc.namespace().unwrap(),
                section: "".to_string(),
                port: port.into()
            })),
        })
    );
}

pub fn mk_route(
    ns: &str,
    name: &str,
    parent_refs: Option<Vec<k8s::policy::httproute::ParentReference>>,
) -> k8s::policy::HttpRoute {
    use k8s::policy::httproute as api;
    api::HttpRoute {
        metadata: kube::api::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: api::HttpRouteSpec {
            inner: api::CommonRouteSpec { parent_refs },
            hostnames: None,
            rules: Some(vec![]),
        },
        status: None,
    }
}

pub fn find_route_condition<'a>(
    statuses: impl IntoIterator<Item = &'a k8s_gateway_api::RouteParentStatus>,
    parent_name: &'static str,
) -> Option<&'a k8s::Condition> {
    statuses
        .into_iter()
        .find(|route_status| route_status.parent_ref.name == parent_name)
        .expect("route must have at least one status set")
        .conditions
        .iter()
        .find(|cond| cond.type_ == "Accepted")
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

    if std::env::var("POLICY_TEST_NO_CLEANUP").is_ok() {
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
            Default::default(),
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

pub fn egress_network_parent_ref(ns: impl ToString, port: Option<u16>) -> ParentReference {
    ParentReference {
        group: Some("policy.linkerd.io".to_string()),
        kind: Some("EgressNetwork".to_string()),
        namespace: Some(ns.to_string()),
        name: "my-egress-net".to_string(),
        section_name: None,
        port,
    }
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
