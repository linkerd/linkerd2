use k8s_openapi::api::{apps::v1 as appsv1, core::v1 as corev1};
use kube::ResourceExt;

mod destination;

pub use destination::DestinationClient;

pub fn random_suffix(len: usize) -> String {
    use rand::Rng;

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

    rand::thread_rng()
        .sample_iter(&LowercaseAlphanumeric)
        .take(len)
        .map(char::from)
        .collect()
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
    kube::runtime::wait::await_condition(api, name, cond)
        .await
        .expect("API call failed")
}

pub async fn scale_replicaset(
    k8s: &kube::Client,
    ns: &str,
    name: &str,
    replicas: usize,
) -> Result<appsv1::ReplicaSet, kube::Error> {
    let rs = kube::Api::<appsv1::ReplicaSet>::namespaced(k8s.clone(), ns)
        .patch(
            name,
            &kube::api::PatchParams::default(),
            &kube::api::Patch::Merge(serde_json::json!({"spec": { "replicas": replicas }})),
        )
        .await?;

    tracing::debug!(replicas, %ns, %name, ?rs);

    Ok(rs)
}

pub async fn await_ready_replicaset(
    k8s: &kube::Client,
    ns: &str,
    name: &str,
) -> Option<appsv1::ReplicaSet> {
    let replicaset_ready = |obj: Option<&appsv1::ReplicaSet>| -> bool {
        if let Some(replicaset) = obj {
            if let Some(status) = &replicaset.status {
                return status.ready_replicas == Some(status.replicas);
            }
        }
        false
    };

    await_condition(k8s, ns, name, replicaset_ready).await
}

pub async fn create_ready_pod(k8s: &kube::Client, pod: corev1::Pod) -> corev1::Pod {
    let pod_ready = |obj: Option<&corev1::Pod>| -> bool {
        if let Some(pod) = obj {
            if let Some(status) = &pod.status {
                if let Some(containers) = &status.container_statuses {
                    return containers.iter().all(|c| c.ready);
                }
            }
        }
        false
    };

    let pod = kube::Api::<corev1::Pod>::namespaced(k8s.clone(), "default")
        .create(&kube::api::PostParams::default(), &pod)
        .await
        .expect("failed to create Pod");
    let pod = await_condition(
        k8s,
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

pub async fn delete_pod(k8s: &kube::Client, pod: &corev1::Pod) -> Result<(), kube::Error> {
    let pod_deleted = |obj: Option<&corev1::Pod>| -> bool { obj.is_none() };

    let ns = pod.namespace().unwrap();
    let name = pod.name_unchecked();
    kube::Api::<corev1::Pod>::namespaced(k8s.clone(), &ns)
        .delete(&name, &kube::api::DeleteParams::default())
        .await?;

    await_condition(k8s, &ns, &name, pod_deleted).await;

    Ok(())
}

pub async fn delete_replicaset(
    k8s: &kube::Client,
    ns: &str,
    name: &str,
) -> Result<(), kube::Error> {
    let rs_deleted = |obj: Option<&appsv1::ReplicaSet>| -> bool { obj.is_none() };

    kube::Api::<appsv1::ReplicaSet>::namespaced(k8s.clone(), ns)
        .delete(name, &kube::api::DeleteParams::default())
        .await?;

    await_condition(k8s, ns, name, rs_deleted).await;

    Ok(())
}
