use anyhow::Result;
use clap::{Parser, Subcommand};

mod churn;
mod client;

#[derive(Parser)]
#[command(name = "dst-load-controller")]
#[command(about = "Destination service load testing controller", long_about = None)]
struct Args {
    #[clap(long, default_value = "linkerd=info,warn")]
    log_level: kubert::LogFilter,

    #[clap(long, default_value = "plain")]
    log_format: kubert::LogFormat,

    #[clap(flatten)]
    client: kubert::ClientArgs,

    #[clap(flatten)]
    admin: kubert::AdminArgs,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Scale controller: Oscillates Deployment replicas between min/max to simulate autoscaler behavior
    Scale {
        /// Deployment name pattern to scale (supports wildcards, e.g., "test-svc-*")
        #[arg(long)]
        deployment_pattern: String,

        /// Minimum replica count
        #[arg(long)]
        min_replicas: i32,

        /// Maximum replica count
        #[arg(long)]
        max_replicas: i32,

        /// Hold time at min/max before changing (e.g., "30s", "1m")
        #[arg(long, default_value = "30s")]
        hold_duration: String,

        /// Jitter percentage (0-100) to spread oscillation timing
        #[arg(long, default_value = "0")]
        jitter_percent: u8,

        /// Namespace where deployments exist
        #[arg(long, default_value = "default")]
        namespace: String,
    },

    /// Client controller: Creates gRPC clients and subscribes to Destination service
    Client {
        /// Destination service address (e.g., "linkerd-destination.linkerd:8086")
        #[arg(long)]
        destination_addr: String,

        /// Label selector to discover target services (e.g., "app.kubernetes.io/component=test-service")
        #[arg(long)]
        service_label_selector: String,

        /// Number of concurrent watchers per service
        #[arg(long, default_value = "1")]
        watchers_per_service: u32,

        /// Minimum stream lifetime before reconnection (e.g., "30s", "5m")
        #[arg(long, default_value = "5m")]
        min_stream_lifetime: String,

        /// Maximum stream lifetime before reconnection (e.g., "1h", "30m")
        #[arg(long, default_value = "30m")]
        max_stream_lifetime: String,

        /// Namespace where services exist
        #[arg(long, default_value = "default")]
        namespace: String,

        /// Pod name (for context token, typically from downward API)
        #[arg(long, env = "POD_NAME")]
        pod_name: Option<String>,

        /// Pod namespace (for context token, typically from downward API)
        #[arg(long, env = "POD_NAMESPACE")]
        pod_namespace: Option<String>,

        /// Node name (for context token, typically from downward API)
        #[arg(long, env = "NODE_NAME")]
        node_name: Option<String>,
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    args.run().await
}

impl Args {
    async fn run(self) -> Result<()> {
        let Args {
            log_level,
            log_format,
            client: client_args,
            admin,
            command,
        } = self;

        match command {
            Commands::Scale {
                deployment_pattern,
                min_replicas,
                max_replicas,
                hold_duration,
                jitter_percent,
                namespace,
            } => {
                tracing::info!(
                    deployment_pattern,
                    min_replicas,
                    max_replicas,
                    hold_duration,
                    jitter_percent,
                    namespace,
                    "Starting scale controller"
                );

                // Validate inputs
                if min_replicas < 0 || max_replicas < 0 {
                    anyhow::bail!("Replica counts must be >= 0");
                }
                if min_replicas >= max_replicas {
                    anyhow::bail!("--min-replicas must be < --max-replicas");
                }
                if jitter_percent > 100 {
                    anyhow::bail!("--jitter-percent must be 0-100");
                }

                let hold_duration = churn::parse_duration(&hold_duration)?;

                // Set up metrics
                let mut prom = prometheus_client::registry::Registry::default();
                let metrics = churn::ChurnMetrics::new(&mut prom);

                // Build runtime with admin server (provides /metrics, /ready, /live)
                let runtime = kubert::Runtime::builder()
                    .with_log(log_level, log_format)
                    .with_admin(admin.into_builder().with_prometheus(prom))
                    .with_client(client_args)
                    .build()
                    .await?;

                // Get Kubernetes client from runtime
                let client = runtime.client();

                // Create churn controller
                let controller = churn::ChurnController::new(client, namespace, metrics);

                // Run oscillate pattern on matching deployments
                controller
                    .run_oscillate_deployments(
                        &deployment_pattern,
                        min_replicas,
                        max_replicas,
                        hold_duration,
                        jitter_percent,
                    )
                    .await?;
            }

            Commands::Client {
                destination_addr,
                service_label_selector,
                watchers_per_service,
                min_stream_lifetime,
                max_stream_lifetime,
                namespace,
                pod_name,
                pod_namespace,
                node_name,
            } => {
                tracing::info!(
                    destination_addr,
                    service_label_selector,
                    watchers_per_service,
                    min_stream_lifetime,
                    max_stream_lifetime,
                    namespace,
                    ?pod_name,
                    ?pod_namespace,
                    ?node_name,
                    "Starting client controller"
                );

                if watchers_per_service == 0 {
                    anyhow::bail!("--watchers-per-service must be > 0");
                }

                // Parse stream lifetime durations
                let min_lifetime = churn::parse_duration(&min_stream_lifetime)?;
                let max_lifetime = churn::parse_duration(&max_stream_lifetime)?;

                if min_lifetime >= max_lifetime {
                    anyhow::bail!("--min-stream-lifetime must be < --max-stream-lifetime");
                }

                // Build context token (mimics linkerd proxy injector)
                let context_token = build_context_token(
                    pod_name.as_deref(),
                    pod_namespace.as_deref(),
                    node_name.as_deref(),
                )?;

                tracing::info!(context_token, "Built context token");

                // Set up metrics
                let mut prom = prometheus_client::registry::Registry::default();
                let metrics = client::ClientMetrics::new(&mut prom);

                // Build runtime with admin server (provides /metrics, /ready, /live)
                let runtime = kubert::Runtime::builder()
                    .with_log(log_level, log_format)
                    .with_admin(admin.into_builder().with_prometheus(prom))
                    .with_client(client_args)
                    .build()
                    .await?;

                // Get Kubernetes client from runtime
                let client = runtime.client();

                // Create client controller
                let controller = client::ClientController::new(
                    client,
                    destination_addr,
                    namespace,
                    context_token,
                    metrics,
                );

                // Run Get requests
                controller
                    .run_get_requests(
                        service_label_selector,
                        watchers_per_service,
                        min_lifetime,
                        max_lifetime,
                    )
                    .await?;
            }
        }

        Ok(())
    }
}

/// Build a context token for destination service requests
/// Format matches what linkerd proxy-injector does: {"ns":"namespace","nodeName":"node","pod":"podname"}
fn build_context_token(
    pod_name: Option<&str>,
    pod_namespace: Option<&str>,
    node_name: Option<&str>,
) -> Result<String> {
    let mut token = serde_json::json!({});

    if let Some(ns) = pod_namespace {
        token["ns"] = serde_json::json!(ns);
    }

    if let Some(pod) = pod_name {
        token["pod"] = serde_json::json!(pod);
    }

    if let Some(node) = node_name {
        token["nodeName"] = serde_json::json!(node);
    }

    Ok(token.to_string())
}
