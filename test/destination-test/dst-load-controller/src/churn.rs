//! Churn controller: Scales existing Deployments to simulate endpoint churn

use std::time::Duration;

use k8s_openapi::api::apps::v1::Deployment;
use kube::{
    api::{Api, Patch, PatchParams},
    Client,
};
use prometheus_client::{
    encoding::EncodeLabelSet,
    metrics::{counter::Counter, family::Family, gauge::Gauge},
    registry::Registry,
};
use tracing::{error, info};

#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
pub struct ChurnLabels {
    pattern: String,
    service: String,
}

pub struct ChurnMetrics {
    pub scale_events: Family<ChurnLabels, Counter>,
    pub current_replicas: Family<ChurnLabels, Gauge>,
}

impl ChurnMetrics {
    pub fn new(registry: &mut Registry) -> Self {
        let scale_events = Family::default();
        let current_replicas = Family::default();

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

    /// Scale a Deployment to the specified replica count
    async fn scale_deployment(
        &self,
        name: &str,
        replicas: i32,
        pattern: &str,
    ) -> anyhow::Result<()> {
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

    /// Oscillate replicas for deployments matching a pattern
    /// This is the simplified version that only scales existing deployments
    pub async fn run_oscillate_deployments(
        &self,
        pattern: &str,
        min_replicas: i32,
        max_replicas: i32,
        hold_duration: Duration,
        jitter_percent: u8,
    ) -> anyhow::Result<()> {
        info!(
            pattern,
            min_replicas,
            max_replicas,
            ?hold_duration,
            jitter_percent,
            "Starting deployment oscillation"
        );

        let deployments: Api<Deployment> = Api::namespaced(self.client.clone(), &self.namespace);

        // List all deployments and filter by pattern
        let deployment_list = deployments.list(&Default::default()).await?;
        
        // Simple glob matching (supports wildcards like "test-svc-*")
        let matching_deployments: Vec<String> = deployment_list
            .items
            .iter()
            .filter_map(|d| {
                let name = d.metadata.name.as_ref()?;
                if matches_pattern(name, pattern) {
                    Some(name.clone())
                } else {
                    None
                }
            })
            .collect();

        if matching_deployments.is_empty() {
            anyhow::bail!("No deployments found matching pattern: {}", pattern);
        }

        info!(
            count = matching_deployments.len(),
            deployments = ?matching_deployments,
            "Found matching deployments"
        );

        // Oscillate forever
        let mut current_replicas = max_replicas;
        loop {
            // Toggle between min and max
            current_replicas = if current_replicas == max_replicas {
                min_replicas
            } else {
                max_replicas
            };

            // Scale all matching deployments
            for deployment_name in &matching_deployments {
                self.scale_deployment(deployment_name, current_replicas, "oscillate")
                    .await?;
            }

            info!(
                replicas = current_replicas,
                deployments = matching_deployments.len(),
                "Scaled deployments"
            );

            // Wait with jitter
            let jitter = if jitter_percent > 0 {
                use rand::Rng;
                let max_jitter = hold_duration.as_millis() * jitter_percent as u128 / 100;
                Duration::from_millis(rand::thread_rng().gen_range(0..=max_jitter as u64))
            } else {
                Duration::from_secs(0)
            };

            let sleep_duration = hold_duration + jitter;
            info!(?sleep_duration, "Holding at current scale");
            tokio::time::sleep(sleep_duration).await;
        }
    }
}

/// Simple glob pattern matching (supports * wildcard)
fn matches_pattern(name: &str, pattern: &str) -> bool {
    if pattern == "*" {
        return true;
    }
    
    if let Some(prefix) = pattern.strip_suffix('*') {
        name.starts_with(prefix)
    } else if let Some(suffix) = pattern.strip_prefix('*') {
        name.ends_with(suffix)
    } else {
        name == pattern
    }
}

/// Parse duration string (e.g., "30s", "5m", "1h")
pub fn parse_duration(s: &str) -> anyhow::Result<Duration> {
    let s = s.trim();
    if s.is_empty() {
        anyhow::bail!("Empty duration string");
    }

    let (num_str, unit) = s.split_at(s.len() - 1);
    let num: u64 = num_str
        .parse()
        .map_err(|_| anyhow::anyhow!("Invalid number: {}", num_str))?;

    match unit {
        "s" => Ok(Duration::from_secs(num)),
        "m" => Ok(Duration::from_secs(num * 60)),
        "h" => Ok(Duration::from_secs(num * 3600)),
        _ => anyhow::bail!("Invalid duration unit: {}", unit),
    }
}
