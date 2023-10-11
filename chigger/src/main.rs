use ::chigger::{create_ready_pod, delete_pod, DestinationClient};
use anyhow::{bail, Result};
use chigger::random_suffix;
use clap::Parser;
use futures::prelude::*;
use k8s_openapi::api::core::v1 as corev1;
use kube::ResourceExt;
use maplit::{btreemap, convert_args};
use rand::seq::SliceRandom;
use std::collections::HashSet;
use tokio::{task::JoinHandle, time};
use tracing::{info, info_span, Instrument};

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

#[derive(Debug, Parser)]
#[clap(name = "policy", about = "A policy resource prototype")]
struct Args {
    #[clap(long, default_value = "chigger=info,warn", env = "CHIGGER_LOG")]
    log_level: kubert::LogFilter,

    #[clap(long, default_value = "json")]
    log_format: kubert::LogFormat,

    #[clap(flatten)]
    client: kubert::ClientArgs,

    #[clap(flatten)]
    admin: kubert::AdminArgs,

    #[clap(long, default_value = "100")]
    max_endpoints: usize,

    #[clap(long, default_value = "10")]
    max_observer_lifetime: f64,

    #[clap(long, default_value = "100")]
    observers: usize,
}

#[tokio::main]
async fn main() -> Result<()> {
    let Args {
        admin,
        client,
        log_level,
        log_format,
        max_endpoints,
        observers,
        max_observer_lifetime,
    } = Args::parse();

    let runtime = kubert::Runtime::builder()
        .with_log(log_level, log_format)
        .with_client(client)
        .with_admin(admin)
        .build()
        .await?;

    let k8s = runtime.client();

    let base_name = format!("chigger-{}", random_suffix(5));
    let svc = kube::Api::<corev1::Service>::namespaced(k8s.clone(), "default")
        .create(
            &kube::api::PostParams::default(),
            &corev1::Service {
                metadata: kube::core::ObjectMeta {
                    name: Some(base_name.clone()),
                    namespace: Some("default".into()),
                    labels: Some(convert_args!(btreemap!(
                        "app" => "chigger",
                    ))),
                    ..Default::default()
                },
                spec: Some(corev1::ServiceSpec {
                    type_: Some("ClusterIP".into()),
                    selector: Some(convert_args!(btreemap!(
                        "app" => "chigger",
                        "svc" => &base_name,
                    ))),
                    ports: Some(vec![corev1::ServicePort {
                        port: 80,
                        protocol: Some("TCP".into()),
                        ..Default::default()
                    }]),
                    ..Default::default()
                }),
                status: None,
            },
        )
        .await?;
    info!(svc.name = svc.name_unchecked(), "Created");

    let mut dst_client = DestinationClient::port_forwarded(&k8s).await;

    let mut dst = dst_client.watch(&svc, 80).await?;
    let init = dst.next().await;
    info!(?init);

    // Start a task that runs a fixed number of observers, restarting the watches
    // randomly within the max lifetime.
    spawn_observers(
        observers,
        time::Duration::from_secs_f64(max_observer_lifetime),
        svc,
        dst_client,
    );

    //
    tokio::spawn(
        scale_endpoints(max_endpoints, base_name.clone(), k8s.clone())
            .instrument(info_span!("endpoints")),
    );

    // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
    // complete before exiting.
    if runtime.run().await.is_err() {
        bail!("Aborted");
    }

    cleanup(base_name, k8s).await?;

    Ok(())
}

async fn scale_endpoints(max: usize, base_name: String, k8s: kube::Client) -> Result<()> {
    let mut pods = Vec::with_capacity(max);
    loop {
        for _ in 0..max {
            let pod = create_ready_pod(
                &k8s,
                corev1::Pod {
                    metadata: kube::core::ObjectMeta {
                        name: Some(format!("{base_name}-{}", random_suffix(6))),
                        namespace: Some("default".into()),
                        labels: Some(convert_args!(btreemap!(
                            "app" => "chigger",
                            "svc" => &base_name,
                        ))),
                        ..Default::default()
                    },
                    spec: Some(corev1::PodSpec {
                        containers: vec![corev1::Container {
                            name: "chigger".into(),
                            image: Some("gcr.io/google_containers/pause:3.2".into()),
                            ..Default::default()
                        }],
                        ..Default::default()
                    }),
                    status: None,
                },
            )
            .await;
            info!(
                pod.name = %pod.name_unchecked(),
                pod.ip = %pod
                    .status
                    .as_ref()
                    .expect("pod must have a status")
                    .pod_ips
                    .as_ref()
                    .unwrap()[0]
                    .ip
                    .as_deref()
                    .expect("pod ip must be set"),
                pods = pods.len() + 1,
                "Created"
            );
            pods.push(pod);
        }

        // Give the observer(s) a chance to observe.
        tokio::task::yield_now().await;

        pods.shuffle(&mut rand::thread_rng());
        while let Some(pod) = pods.pop() {
            let pod_ip = pod
                .status
                .as_ref()
                .expect("pod must have a status")
                .pod_ips
                .as_ref()
                .unwrap()[0]
                .ip
                .as_deref()
                .expect("pod ip must be set");
            info!(
                pod.name = %pod.name_unchecked(),
                pod.ip = %pod_ip,
                pods = pods.len(),
                "Deleting"
            );
            if let Err(error) = delete_pod(&k8s, &pod).await {
                info!(
                    %error,
                    pod.name = %pod.name_unchecked(),
                    pod.ip = %pod_ip,
                    pods = pods.len(),
                    "Deletion failed"
                );
            }
        }
    }
}

fn spawn_observers(
    max: usize,
    max_lifetime: time::Duration,
    svc: corev1::Service,
    dst: DestinationClient,
) -> Vec<JoinHandle<Result<()>>> {
    let mut handles = Vec::with_capacity(max);
    for id in 0..max {
        handles.push(tokio::spawn(
            client(svc.clone(), dst.clone(), max_lifetime).instrument(info_span!("client", %id)),
        ));
    }
    handles
}

async fn client(
    svc: corev1::Service,
    mut dst: DestinationClient,
    max_lifetime: time::Duration,
) -> Result<()> {
    use rand::Rng;

    loop {
        let lifetime = time::Duration::from_secs_f64(
            rand::thread_rng().gen_range(0.0..=max_lifetime.as_secs_f64()),
        );
        info!(?lifetime, "Resetting");

        let rx = dst.watch(&svc, 80).await?;
        let _ = time::timeout(lifetime, observe(rx)).await;
    }
}

async fn observe(mut dst: tonic::Streaming<linkerd2_proxy_api::destination::Update>) -> Result<()> {
    let mut endpoints = HashSet::new();
    while let Some(up) = dst.try_next().await? {
        match up.update.unwrap() {
            linkerd2_proxy_api::destination::update::Update::Add(addrs) => {
                for a in addrs.addrs.into_iter() {
                    let addr = std::net::SocketAddr::try_from(a.addr.unwrap())?;
                    endpoints.insert(addr);
                    info!(ep.ip = %addr.ip(), endpoints = endpoints.len(), "Added");
                }
            }
            linkerd2_proxy_api::destination::update::Update::Remove(addrs) => {
                for a in addrs.addrs.into_iter() {
                    let addr = std::net::SocketAddr::try_from(a)?;
                    endpoints.remove(&addr);
                    info!(ep.ip = %addr.ip(), endpoints = endpoints.len(), "Removed");
                }
            }
            linkerd2_proxy_api::destination::update::Update::NoEndpoints(_) => {
                endpoints.clear();
                info!(endpoints = endpoints.len(), "Cleared");
            }
        }
    }
    Ok(())
}

async fn cleanup(base_name: String, k8s: kube::Client) -> Result<()> {
    kube::Api::<corev1::Service>::namespaced(k8s.clone(), "default")
        .delete(&base_name, &kube::api::DeleteParams::default())
        .await?;

    let pods = kube::Api::<corev1::Pod>::namespaced(k8s.clone(), "default")
        .list_metadata(&kube::api::ListParams::default().labels(&format!("svc={base_name}")))
        .await?;
    for pod in pods {
        kube::Api::<corev1::Pod>::namespaced(k8s.clone(), "default")
            .delete(&pod.name_unchecked(), &kube::api::DeleteParams::default())
            .await?;
    }

    Ok(())
}
