use ::chigger::{create_ready_pod, delete_pod, DestinationClient};
use anyhow::{bail, Result};
use chigger::random_suffix;
use clap::Parser;
use futures::prelude::*;
use k8s_openapi::api::core::v1 as corev1;
use kube::ResourceExt;
use maplit::{btreemap, convert_args};
use std::collections::HashSet;
use tokio::time;
use tracing::Instrument;

#[cfg(all(target_os = "linux", target_arch = "x86_64", target_env = "gnu"))]
#[global_allocator]
static GLOBAL: jemallocator::Jemalloc = jemallocator::Jemalloc;

#[derive(Debug, Parser)]
#[clap(name = "policy", about = "A policy resource prototype")]
struct Args {
    #[clap(long, default_value = "chigger=info,warn", env = "CHIGGER_LOG")]
    log_level: kubert::LogFilter,

    #[clap(long, default_value = "plain")]
    log_format: kubert::LogFormat,

    #[clap(flatten)]
    client: kubert::ClientArgs,

    #[clap(flatten)]
    admin: kubert::AdminArgs,
}

#[tokio::main]
async fn main() -> Result<()> {
    let Args {
        admin,
        client,
        log_level,
        log_format,
    } = Args::parse();

    let mut admin = admin.into_builder();
    admin.with_default_prometheus();

    let runtime = kubert::Runtime::builder()
        .with_log(log_level, log_format)
        .with_admin(admin)
        .with_client(client)
        .build()
        .await?;

    let k8s = runtime.client();

    let base_name = format!("chigger-{}", random_suffix(8));
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
    tracing::info!(svc.name = svc.name_unchecked(), "Created");

    let mut dst = DestinationClient::port_forwarded(&k8s)
        .await
        .watch(&svc, 80)
        .await?;

    let init = dst.next().await;
    tracing::info!(?init);

    tokio::spawn(observe(dst).instrument(tracing::info_span!("observer")));
    tokio::spawn(
        scale_up_down(100, base_name.clone(), k8s.clone())
            .instrument(tracing::info_span!("controller")),
    );

    // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
    // complete before exiting.
    if runtime.run().await.is_err() {
        bail!("Aborted");
    }

    cleanup(base_name, k8s).await?;

    Ok(())
}

async fn scale_up_down(max: usize, base_name: String, k8s: kube::Client) -> Result<()> {
    let mut pods = Vec::with_capacity(max);
    loop {
        for _ in 0..max {
            let pod = create_ready_pod(
                &k8s,
                corev1::Pod {
                    metadata: kube::core::ObjectMeta {
                        name: Some(format!("{base_name}-{}", random_suffix(8))),
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
            tracing::info!(
                pod.name = pod.name_unchecked(),
                pods = pods.len() + 1,
                "Created"
            );
            pods.push(pod);
        }

        time::sleep(time::Duration::from_secs(10)).await;

        for (i, pod) in pods.drain(..).enumerate() {
            match delete_pod(&k8s, &pod).await {
                Ok(()) => {
                    tracing::info!(
                        pod.name = pod.name_unchecked(),
                        pods = max - i - 1,
                        "Deleted"
                    );
                }
                Err(error) => {
                    tracing::info!(?error, "Deletion failed");
                }
            }
        }
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
                    tracing::info!(ep.addr = %addr, endpoints = endpoints.len(), "Added");
                }
            }
            linkerd2_proxy_api::destination::update::Update::Remove(addrs) => {
                for a in addrs.addrs.into_iter() {
                    let addr = std::net::SocketAddr::try_from(a)?;
                    endpoints.remove(&addr);
                    tracing::info!(ep.addr = %addr, endpoints = endpoints.len(), "Removed");
                }
            }
            linkerd2_proxy_api::destination::update::Update::NoEndpoints(_) => {
                endpoints.clear();
                tracing::info!(endpoints = endpoints.len(), "Cleared");
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
        .list_metadata(&kube::api::ListParams::default().labels(&format!("app={base_name}")))
        .await?;
    for pod in pods {
        kube::Api::<corev1::Pod>::namespaced(k8s.clone(), "default")
            .delete(&pod.name_unchecked(), &kube::api::DeleteParams::default())
            .await?;
    }

    Ok(())
}
