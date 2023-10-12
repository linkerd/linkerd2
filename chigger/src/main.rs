use ::chigger::{random_suffix, scale_replicaset, DestinationClient};
use chigger::delete_replicaset;

use anyhow::{bail, Result};
use clap::Parser;
use futures::prelude::*;
use k8s_openapi::{
    api::{apps::v1 as appsv1, core::v1 as corev1},
    apimachinery::pkg::apis::meta::v1 as metav1,
};
use kube::ResourceExt;
use maplit::{btreemap, convert_args};
use rand::Rng;
use std::collections::HashSet;
use tokio::time;
use tracing::{error, info, info_span, Instrument};

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

    #[clap(long, default_value = "50")]
    min_endpoints: usize,

    #[clap(long, default_value = "100")]
    max_endpoints: usize,

    #[clap(long, default_value = "1")]
    min_endpoints_stabletime: f64,

    #[clap(long, default_value = "300")]
    max_endpoints_stabletime: f64,

    #[clap(long, default_value = "10")]
    min_observer_lifetime: f64,

    #[clap(long, default_value = "300")]
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
        min_endpoints,
        max_endpoints,
        min_endpoints_stabletime,
        max_endpoints_stabletime,
        observers,
        min_observer_lifetime,
        max_observer_lifetime,
    } = Args::parse();

    let runtime = kubert::Runtime::builder()
        .with_log(log_level, log_format)
        .with_client(client)
        .with_admin(admin)
        .build()
        .await?;

    info!(
        min_endpoints,
        max_endpoints,
        min_endpoints_stabletime,
        max_endpoints_stabletime,
        observers,
        min_observer_lifetime,
        max_observer_lifetime,
        "Starting",
    );

    let k8s = runtime.client();

    let name = format!("chigger-{}", random_suffix(5));
    let svc = kube::Api::<corev1::Service>::namespaced(k8s.clone(), "default")
        .create(
            &kube::api::PostParams::default(),
            &corev1::Service {
                metadata: kube::core::ObjectMeta {
                    name: Some(name.clone()),
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
                        "svc" => &name,
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

    let mut dst = DestinationClient::port_forwarded(&k8s).await;
    let mut idle_observation = dst.watch(&svc, 80).await?;
    info!(init = ?idle_observation.next().await);

    // Start a task that runs a fixed number of observers, restarting the watches
    // randomly within the max lifetime.
    spawn_observers(
        observers,
        time::Duration::from_secs_f64(min_observer_lifetime),
        time::Duration::from_secs_f64(max_observer_lifetime),
        svc,
        &k8s,
    );

    //
    tokio::spawn(
        scale_endpoints(
            min_endpoints,
            max_endpoints,
            time::Duration::from_secs_f64(min_endpoints_stabletime),
            time::Duration::from_secs_f64(max_endpoints_stabletime),
            name.clone(),
            k8s.clone(),
        )
        .instrument(info_span!("endpoints")),
    );

    // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
    // complete before exiting.
    if runtime.run().await.is_err() {
        bail!("Aborted");
    }

    cleanup(&k8s, &name).await?;

    drop(idle_observation);

    Ok(())
}

async fn scale_endpoints(
    min: usize,
    max: usize,
    min_stable: time::Duration,
    max_stable: time::Duration,
    name: String,
    k8s: kube::Client,
) -> Result<()> {
    kube::Api::namespaced(k8s.clone(), "default")
        .create(
            &kube::api::PostParams::default(),
            &appsv1::ReplicaSet {
                metadata: kube::core::ObjectMeta {
                    name: Some(name.clone()),
                    namespace: Some("default".into()),
                    labels: Some(convert_args!(btreemap!(
                        "app" => "chigger",
                        "svc" => &name,
                    ))),
                    ..Default::default()
                },
                spec: Some(appsv1::ReplicaSetSpec {
                    replicas: Some(0),
                    min_ready_seconds: Some(0),
                    selector: metav1::LabelSelector {
                        match_labels: Some(convert_args!(btreemap!(
                            "app" => "chigger",
                            "svc" => &name,
                        ))),
                        ..Default::default()
                    },
                    template: Some(corev1::PodTemplateSpec {
                        metadata: Some(kube::core::ObjectMeta {
                            labels: Some(convert_args!(btreemap!(
                                "app" => "chigger",
                                "svc" => &name,
                            ))),
                            ..Default::default()
                        }),
                        spec: Some(corev1::PodSpec {
                            containers: vec![corev1::Container {
                                name: "pause".into(),
                                image: Some("gcr.io/google_containers/pause:3.2".into()),
                                ..Default::default()
                            }],
                            ..Default::default()
                        }),
                    }),
                }),
                status: None,
            },
        )
        .await?;

    let mut replicas = 0;
    loop {
        let stable = time::Duration::from_secs_f64(
            rand::thread_rng().gen_range(min_stable.as_secs_f64()..=max_stable.as_secs_f64()),
        );

        let desired = rand::thread_rng().gen_range(min..=max);
        info!(rs.ready = replicas, rs.replicas = desired, "Scaling");

        scale_replicaset(&k8s, "default", &name, desired).await?;
        replicas = desired;
        info!(rs.ready = %replicas, ?stable, "Scaled");

        time::sleep(stable).await;
    }
}

fn spawn_observers(
    count: usize,
    min_lifetime: time::Duration,
    max_lifetime: time::Duration,
    svc: corev1::Service,
    k8s: &kube::Client,
) {
    async fn observe(
        mut dst: tonic::Streaming<linkerd2_proxy_api::destination::Update>,
    ) -> Result<()> {
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
    for id in 0..count {
        let svc = svc.clone();
        let k8s = k8s.clone();
        tokio::spawn(
            async move {
                let mut dst = DestinationClient::port_forwarded(&k8s).await;
                loop {
                    let lifetime = time::Duration::from_secs_f64(
                        rand::thread_rng()
                            .gen_range(min_lifetime.as_secs_f64()..=max_lifetime.as_secs_f64()),
                    );
                    info!(?lifetime, "Resetting");
                    let rx = match dst.watch(&svc, 80).await {
                        Ok(rx) => rx,
                        Err(error) => {
                            error!(%error, "Watch failed");
                            dst = DestinationClient::port_forwarded(&k8s).await;
                            continue;
                        }
                    };
                    let _ = time::timeout(lifetime, observe(rx)).await;
                }
            }
            .instrument(info_span!("observer", id)),
        );
    }
}

async fn cleanup(k8s: &kube::Client, name: &str) -> Result<()> {
    kube::Api::<corev1::Service>::namespaced(k8s.clone(), "default")
        .delete(name, &kube::api::DeleteParams::default())
        .await?;

    delete_replicaset(k8s, "default", name).await?;

    Ok(())
}
