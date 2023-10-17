use ::chigger::{random_suffix, scale_replicaset, DestinationClient};
use chigger::{await_condition, delete_replicaset};

use anyhow::{bail, Result};
use clap::Parser;
use futures::prelude::*;
use k8s_openapi::{
    api::{apps::v1 as appsv1, core::v1 as corev1},
    apimachinery::pkg::apis::meta::v1 as metav1,
};
use kube::ResourceExt;
use linkerd2_proxy_api::destination as api;
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

    let id = random_suffix(5);
    let svc = kube::Api::<corev1::Service>::namespaced(k8s.clone(), "default")
        .create(
            &kube::api::PostParams::default(),
            &corev1::Service {
                metadata: kube::core::ObjectMeta {
                    name: Some(format!("chigger-{id}")),
                    namespace: Some("default".into()),
                    labels: Some(convert_args!(btreemap!(
                        "app" => "chigger",
                        "id" => &id,
                    ))),
                    ..Default::default()
                },
                spec: Some(corev1::ServiceSpec {
                    type_: Some("ClusterIP".into()),
                    selector: Some(convert_args!(btreemap!(
                        "app" => "chigger",
                        "role" => "server",
                        "id" => &id,
                    ))),
                    ports: Some(vec![corev1::ServicePort {
                        port: 4444,
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

    let mut dst = DestinationClient::port_forwarded(&k8s)
        .instrument(info_span!("idle"))
        .await;
    let idle_observation = dst.watch(&svc, 4444).instrument(info_span!("idle")).await?;

    // Start a task that runs a fixed number of observers, restarting the watches
    // randomly within the max lifetime.
    spawn_observers(
        observers,
        time::Duration::from_secs_f64(min_observer_lifetime),
        time::Duration::from_secs_f64(max_observer_lifetime),
        svc,
        &k8s,
    );

    ready_stable_endpoints(1, id.clone(), k8s.clone())
        .instrument(info_span!("eps.serve"))
        .await?;

    ready_client(id.clone(), k8s.clone())
        .instrument(info_span!("client"))
        .await?;

    tokio::spawn(
        scale_endpoints(
            min_endpoints,
            max_endpoints,
            time::Duration::from_secs_f64(min_endpoints_stabletime),
            time::Duration::from_secs_f64(max_endpoints_stabletime),
            id.clone(),
            k8s.clone(),
        )
        .instrument(info_span!("eps.scale")),
    );

    // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
    // complete before exiting.
    if runtime.run().await.is_err() {
        bail!("Aborted");
    }

    drop(idle_observation);

    cleanup(&k8s, &id).await?;

    Ok(())
}

async fn ready_client(id: String, k8s: kube::Client) -> Result<()> {
    let name = format!("chigger-client-{id}");
    let api = kube::Api::namespaced(k8s, "default");
    api.create(
        &kube::api::PostParams::default(),
        &appsv1::ReplicaSet {
            metadata: kube::core::ObjectMeta {
                name: Some(name.clone()),
                namespace: Some("default".into()),
                labels: Some(convert_args!(btreemap!(
                    "app" => "chigger",
                    "role" => "client",
                    "id" => &id,
                ))),
                ..Default::default()
            },
            spec: Some(appsv1::ReplicaSetSpec {
                replicas: Some(1),
                min_ready_seconds: Some(0),
                selector: metav1::LabelSelector {
                    match_labels: Some(convert_args!(btreemap!(
                        "app" => "chigger",
                        "role" => "client",
                        "id" => &id,
                    ))),
                    ..Default::default()
                },
                template: Some(corev1::PodTemplateSpec {
                    metadata: Some(kube::core::ObjectMeta {
                        labels: Some(convert_args!(btreemap!(
                            "app" => "chigger",
                            "role" => "client",
                            "id" => &id,
                        ))),
                        annotations: Some(convert_args!(btreemap!(
                            "linkerd.io/inject" => "enabled",
                            "config.linkerd.io/proxy-image" => "ghcr.io/olix0r/l2-proxy",
                            "config.linkerd.io/proxy-version" => "ver.repro.78ac7fb87",
                            "config.linkerd.io/proxy-log-level" => "debug,h2=trace,trust_dns=warn",
                        ))),
                        ..Default::default()
                    }),
                    spec: Some(corev1::PodSpec {
                        containers: vec![corev1::Container {
                            name: "main".into(),
                            image: Some("ghcr.io/olix0r/tcp-echo:v15".into()),
                            env: Some(vec![corev1::EnvVar {
                                name: "RUST_LOG".into(),
                                value: Some("debug".into()),
                                ..Default::default()
                            }]),
                            args: Some(vec![
                                "client".into(),
                                "--message".into(),
                                "beep\r\n\r\n".into(),
                                "--concurrency=1".into(),
                                format!("--messages-per-connection={}", std::usize::MAX),
                                format!("chigger-{id}.default.svc.cluster.local:4444"),
                            ]),
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

    // Wait until the replicaset is ready.
    kube::runtime::wait::await_condition(api, &name, |obj: Option<&appsv1::ReplicaSet>| -> bool {
        if let Some(rs) = obj {
            return rs
                .status
                .as_ref()
                .map_or(false, |s| s.ready_replicas == Some(s.replicas));
        }
        false
    })
    .await?;

    Ok(())
}

async fn ready_stable_endpoints(replicas: usize, id: String, k8s: kube::Client) -> Result<()> {
    let rs = create_servers(&id, "stable", &k8s).await?;
    if replicas != 0 {
        scale_replicaset(&k8s, "default", &rs.name_unchecked(), replicas).await?;
    }

    // Wait for all endpoints to be ready.
    await_condition(
        &k8s,
        "default",
        &rs.name_unchecked(),
        |obj: Option<&appsv1::ReplicaSet>| -> bool {
            if let Some(rs) = obj {
                if let Some(status) = &rs.status {
                    return status.ready_replicas == Some(status.replicas);
                }
            }
            false
        },
    )
    .await;

    Ok(())
}

async fn scale_endpoints(
    min: usize,
    max: usize,
    min_stable: time::Duration,
    max_stable: time::Duration,
    id: String,
    k8s: kube::Client,
) -> Result<()> {
    let rs = create_servers(&id, "scale", &k8s).await?;

    let mut replicas = 0;
    loop {
        let stable = time::Duration::from_secs_f64(
            rand::thread_rng().gen_range(min_stable.as_secs_f64()..=max_stable.as_secs_f64()),
        );

        let desired = rand::thread_rng().gen_range(min..=max);
        info!(rs.ready = replicas, rs.replicas = desired, "Scaling");

        scale_replicaset(&k8s, "default", &rs.name_unchecked(), desired).await?;
        replicas = desired;
        info!(rs.ready = replicas, stable = stable.as_secs_f64(), "Scaled");

        time::sleep(stable).await;
    }
}

async fn create_servers(id: &str, role: &str, k8s: &kube::Client) -> Result<appsv1::ReplicaSet> {
    kube::Api::namespaced(k8s.clone(), "default")
        .create(
            &kube::api::PostParams::default(),
            &appsv1::ReplicaSet {
                metadata: kube::core::ObjectMeta {
                    name: Some(format!("chigger-srv-{role}-{id}")),
                    namespace: Some("default".into()),
                    labels: Some(convert_args!(btreemap!(
                        "app" => "chigger",
                        "role" => "server",
                        "srv" => role,
                        "id" => id,
                    ))),
                    ..Default::default()
                },
                spec: Some(appsv1::ReplicaSetSpec {
                    replicas: Some(0),
                    min_ready_seconds: Some(0),
                    selector: metav1::LabelSelector {
                        match_labels: Some(convert_args!(btreemap!(
                            "app" => "chigger",
                            "role" => "server",
                            "srv" => role,
                            "id" => id,
                        ))),
                        ..Default::default()
                    },
                    template: Some(corev1::PodTemplateSpec {
                        metadata: Some(kube::core::ObjectMeta {
                            labels: Some(convert_args!(btreemap!(
                                "app" => "chigger",
                                "role" => "server",
                                "srv" => role,
                                "id" => id,
                            ))),
                            ..Default::default()
                        }),
                        spec: Some(corev1::PodSpec {
                            containers: vec![corev1::Container {
                                name: "main".into(),
                                image: Some("ghcr.io/olix0r/tcp-echo:v15".into()),
                                args: Some(vec![
                                    "server".into(),
                                    "--port=4444".into(),
                                    "--message=boop".into(),
                                ]),
                                ..Default::default()
                            }],
                            ..Default::default()
                        }),
                    }),
                }),
                status: None,
            },
        )
        .await
        .map_err(Into::into)
}

fn spawn_observers(
    count: usize,
    min_lifetime: time::Duration,
    max_lifetime: time::Duration,
    svc: corev1::Service,
    k8s: &kube::Client,
) {
    async fn observe(mut dst: tonic::Streaming<api::Update>) -> Result<()> {
        let mut endpoints = HashSet::new();
        let mut updates = 0;
        while let Some(up) = dst.try_next().await? {
            updates += 1;
            match up.update.unwrap() {
                api::update::Update::Add(addrs) => {
                    for a in addrs.addrs.into_iter() {
                        let addr = std::net::SocketAddr::try_from(a.addr.unwrap())?;
                        endpoints.insert(addr);
                        info!(ep.ip = %addr.ip(), updates, endpoints = endpoints.len(), "Added");
                    }
                }
                api::update::Update::Remove(addrs) => {
                    for a in addrs.addrs.into_iter() {
                        let addr = std::net::SocketAddr::try_from(a)?;
                        endpoints.remove(&addr);
                        info!(ep.ip = %addr.ip(), updates, endpoints = endpoints.len(), "Removed");
                    }
                }
                api::update::Update::NoEndpoints(_) => {
                    endpoints.clear();
                    info!(updates, endpoints = endpoints.len(), "Cleared");
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
                    let rx = match dst.watch(&svc, 4444).await {
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

async fn cleanup(k8s: &kube::Client, id: &str) -> Result<()> {
    kube::Api::<corev1::Service>::namespaced(k8s.clone(), "default")
        .delete(
            &format!("chigger-{id}"),
            &kube::api::DeleteParams::default(),
        )
        .await?;

    let objs = kube::Api::<appsv1::ReplicaSet>::namespaced(k8s.clone(), "default")
        .list_metadata(&kube::api::ListParams::default().labels(&format!("app=chigger,id={id}")))
        .await?;
    for obj in objs.into_iter() {
        if let Some(name) = obj.metadata.name.as_ref() {
            if let Err(error) = delete_replicaset(k8s, "default", name).await {
                error!(%error, %name, "Failed to delete ReplicaSet");
            }
        }
    }

    Ok(())
}
