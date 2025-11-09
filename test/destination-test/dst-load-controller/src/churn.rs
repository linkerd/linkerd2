//! Churn controller: Creates and manages Services/Deployments to simulate endpoint churn

use std::{collections::BTreeMap, time::Duration};

use k8s_openapi::{
    api::{
        apps::v1::{Deployment, DeploymentSpec},
        core::v1::{Container, ContainerPort, PodSpec, PodTemplateSpec, Service, ServicePort, ServiceSpec},
    },
    apimachinery::pkg::apis::meta::v1::LabelSelector,
};
use kube::{
    api::{Api, ObjectMeta, Patch, PatchParams, PostParams},
    Client,
};
use prometheus_client::{
    encoding::EncodeLabelSet,
    metrics::{counter::Counter, family::Family, gauge::Gauge},
    registry::Registry,
};
use tokio::time::sleep;
use tracing::{error, info};

#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
pub struct ChurnLabels {
    pattern: String,
    service: String,
}

pub struct ChurnMetrics {
    pub services_created: Counter,
    pub deployments_created: Counter,
    pub scale_events: Family<ChurnLabels, Counter>,
    pub current_replicas: Family<ChurnLabels, Gauge>,
}

impl ChurnMetrics {
    pub fn new(registry: &mut Registry) -> Self {
        let services_created = Counter::default();
        let deployments_created = Counter::default();
        let scale_events = Family::default();
        let current_replicas = Family::default();

        registry.register(
            "churn_services_created",
            "Total number of services created",
            services_created.clone(),
        );
        registry.register(
            "churn_deployments_created",
            "Total number of deployments created",
            deployments_created.clone(),
        );
        registry.register(
            "churn_scale_events",
            "Total number of scale events",
            scale_events.clone(),
        );
        registry.register(
            "churn_current_replicas",
            "Current replica count per service",
            current_replicas.clone(),
        );

        Self {
            services_created,
            deployments_created,
            scale_events,
            current_replicas,
        }
    }
}

pub struct ChurnController {
    pub client: Client,
    pub namespace: String,
    pub metrics: ChurnMetrics,
}

impl ChurnController {
    pub fn new(client: Client, namespace: String, metrics: ChurnMetrics) -> Self {
        Self {
            client,
            namespace,
            metrics,
        }
    }

    /// Create a Service and Deployment pair
    async fn create_service_deployment(
        &self,
        name: &str,
        replicas: i32,
        port: i32,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let services: Api<Service> = Api::namespaced(self.client.clone(), &self.namespace);
        let deployments: Api<Deployment> = Api::namespaced(self.client.clone(), &self.namespace);

        let labels = BTreeMap::from([("app".to_string(), name.to_string())]);

        // Create Service
        let service = Service {
            metadata: ObjectMeta {
                name: Some(name.to_string()),
                namespace: Some(self.namespace.clone()),
                labels: Some(labels.clone()),
                ..Default::default()
            },
            spec: Some(ServiceSpec {
                selector: Some(labels.clone()),
                ports: Some(vec![ServicePort {
                    port,
                    target_port: Some(k8s_openapi::apimachinery::pkg::util::intstr::IntOrString::Int(port)),
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..Default::default()
        };

        services
            .create(&PostParams::default(), &service)
            .await
            .map_err(|e| {
                error!(?e, service = name, "Failed to create service");
                e
            })?;

        info!(service = name, port, "Created service");
        self.metrics.services_created.inc();

        // Create Deployment with KWOK scheduling
        let mut pod_labels = labels.clone();
        // KWOK label tells KWOK to manage this pod
        pod_labels.insert("kwok.x-k8s.io/node".to_string(), "kwok-node".to_string());

        let deployment = Deployment {
            metadata: ObjectMeta {
                name: Some(name.to_string()),
                namespace: Some(self.namespace.clone()),
                labels: Some(labels.clone()),
                ..Default::default()
            },
            spec: Some(DeploymentSpec {
                replicas: Some(replicas),
                selector: LabelSelector {
                    match_labels: Some(labels.clone()),
                    ..Default::default()
                },
                template: PodTemplateSpec {
                    metadata: Some(ObjectMeta {
                        labels: Some(pod_labels),
                        ..Default::default()
                    }),
                    spec: Some(PodSpec {
                        // Node selector ensures KWOK schedules these pods
                        node_selector: Some(BTreeMap::from([(
                            "type".to_string(),
                            "kwok".to_string(),
                        )])),
                        containers: vec![Container {
                            name: "app".to_string(),
                            image: Some("fake-image:latest".to_string()),
                            ports: Some(vec![ContainerPort {
                                container_port: port,
                                ..Default::default()
                            }]),
                            ..Default::default()
                        }],
                        ..Default::default()
                    }),
                },
                ..Default::default()
            }),
            ..Default::default()
        };

        deployments
            .create(&PostParams::default(), &deployment)
            .await
            .map_err(|e| {
                error!(?e, deployment = name, "Failed to create deployment");
                e
            })?;

        info!(deployment = name, replicas, "Created deployment");
        self.metrics.deployments_created.inc();

        Ok(())
    }

    /// Scale a Deployment to the specified replica count
    async fn scale_deployment(
        &self,
        name: &str,
        replicas: i32,
        pattern: &str,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let deployments: Api<Deployment> = Api::namespaced(self.client.clone(), &self.namespace);

        let patch = serde_json::json!({
            "spec": {
                "replicas": replicas
            }
        });

        deployments
            .patch(
                name,
                &PatchParams::apply("dst-load-controller"),
                &Patch::Merge(&patch),
            )
            .await
            .map_err(|e| {
                error!(?e, deployment = name, replicas, "Failed to scale deployment");
                e
            })?;

        info!(deployment = name, replicas, pattern, "Scaled deployment");

        let labels = ChurnLabels {
            pattern: pattern.to_string(),
            service: name.to_string(),
        };
        self.metrics.scale_events.get_or_create(&labels).inc();
        self.metrics
            .current_replicas
            .get_or_create(&labels)
            .set(replicas as i64);

        Ok(())
    }

    /// Run stable pattern: Create services with fixed endpoint count, no changes
    pub async fn run_stable(
        &self,
        stable_services: u32,
        stable_endpoints: u32,
    ) -> Result<(), Box<dyn std::error::Error>> {
        info!(
            stable_services,
            stable_endpoints, "Starting stable pattern"
        );

        // Create all services
        for i in 0..stable_services {
            let name = format!("stable-svc-{}", i);
            self.create_service_deployment(&name, stable_endpoints as i32, 8080)
                .await?;

            // Track initial replicas
            let labels = ChurnLabels {
                pattern: "stable".to_string(),
                service: name.clone(),
            };
            self.metrics
                .current_replicas
                .get_or_create(&labels)
                .set(stable_endpoints as i64);
        }

        info!("Stable pattern setup complete. Services will remain unchanged.");

        // Just sleep forever - stable means no churn
        loop {
            sleep(Duration::from_secs(3600)).await;
        }
    }

    /// Run oscillate pattern: Services scale between min and max endpoints
    pub async fn run_oscillate(
        &self,
        oscillate_services: u32,
        oscillate_min_endpoints: u32,
        oscillate_max_endpoints: u32,
        oscillate_hold_duration: Duration,
        oscillate_jitter_percent: u8,
    ) -> Result<(), Box<dyn std::error::Error>> {
        info!(
            oscillate_services,
            oscillate_min_endpoints,
            oscillate_max_endpoints,
            ?oscillate_hold_duration,
            oscillate_jitter_percent,
            "Starting oscillate pattern"
        );

        // Create all services at min replicas
        for i in 0..oscillate_services {
            let name = format!("oscillate-svc-{}", i);
            self.create_service_deployment(&name, oscillate_min_endpoints as i32, 8080)
                .await?;

            // Track initial replicas
            let labels = ChurnLabels {
                pattern: "oscillate".to_string(),
                service: name.clone(),
            };
            self.metrics
                .current_replicas
                .get_or_create(&labels)
                .set(oscillate_min_endpoints as i64);
        }

        info!("Initial services created. Starting oscillation...");

        let mut at_max = false;

        loop {
            // Determine target replicas
            let target_replicas = if at_max {
                oscillate_min_endpoints as i32
            } else {
                oscillate_max_endpoints as i32
            };

            let phase = if at_max { "scale-down" } else { "scale-up" };
            info!(phase, target_replicas, "Beginning oscillation phase");

            // Scale all services with jitter
            for i in 0..oscillate_services {
                let name = format!("oscillate-svc-{}", i);

                // Apply jitter: random delay 0 to (hold_duration * jitter_percent / 100)
                if oscillate_jitter_percent > 0 {
                    let max_jitter_ms = oscillate_hold_duration.as_millis()
                        * oscillate_jitter_percent as u128
                        / 100;
                    let jitter_ms =
                        (rand::random::<u64>() % (max_jitter_ms as u64 + 1)) as u64;
                    sleep(Duration::from_millis(jitter_ms)).await;
                }

                self.scale_deployment(&name, target_replicas, "oscillate")
                    .await?;
            }

            info!(phase, "All services scaled");

            // Hold at this level
            sleep(oscillate_hold_duration).await;

            // Toggle state
            at_max = !at_max;
        }
    }
}

/// Parse duration string (e.g., "30s", "5m", "1h")
pub fn parse_duration(s: &str) -> Result<Duration, String> {
    let s = s.trim();
    if s.is_empty() {
        return Err("Empty duration string".to_string());
    }

    let (num_str, unit) = s.split_at(s.len() - 1);
    let num: u64 = num_str
        .parse()
        .map_err(|_| format!("Invalid number: {}", num_str))?;

    match unit {
        "s" => Ok(Duration::from_secs(num)),
        "m" => Ok(Duration::from_secs(num * 60)),
        "h" => Ok(Duration::from_secs(num * 3600)),
        _ => Err(format!("Invalid duration unit: {}", unit)),
    }
}
