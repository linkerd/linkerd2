use clap::{Parser, Subcommand};

mod churn;
mod client;

#[derive(Parser)]
#[command(name = "dst-load-controller")]
#[command(about = "Destination service load testing controller", long_about = None)]
struct Cli {
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

        /// Prometheus metrics port
        #[arg(long, default_value = "8080")]
        metrics_port: u16,

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

        /// Prometheus metrics port
        #[arg(long, default_value = "8080")]
        metrics_port: u16,
    },
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Initialize tracing
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    let cli = Cli::parse();

    match cli.command {
        Commands::Scale {
            deployment_pattern,
            min_replicas,
            max_replicas,
            hold_duration,
            jitter_percent,
            metrics_port,
            namespace,
        } => {
            tracing::info!(
                deployment_pattern,
                min_replicas,
                max_replicas,
                hold_duration,
                jitter_percent,
                metrics_port,
                namespace,
                "Starting scale controller"
            );

            // Validate inputs
            if min_replicas < 0 || max_replicas < 0 {
                return Err("Replica counts must be >= 0".into());
            }
            if min_replicas >= max_replicas {
                return Err("--min-replicas must be < --max-replicas".into());
            }
            if jitter_percent > 100 {
                return Err("--jitter-percent must be 0-100".into());
            }

            let hold_duration = churn::parse_duration(&hold_duration)?;

            // Create Kubernetes client
            let client = kube::Client::try_default().await?;

            // Set up metrics
            let mut registry = prometheus_client::registry::Registry::default();
            let metrics = churn::ChurnMetrics::new(&mut registry);

            // Create churn controller
            let controller = churn::ChurnController::new(client, namespace, metrics);

            // TODO: Start metrics server on metrics_port

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
            namespace,
            pod_name,
            pod_namespace,
            node_name,
            metrics_port,
        } => {
            tracing::info!(
                destination_addr,
                service_label_selector,
                watchers_per_service,
                namespace,
                ?pod_name,
                ?pod_namespace,
                ?node_name,
                metrics_port,
                "Starting client controller"
            );

            if watchers_per_service == 0 {
                return Err("--watchers-per-service must be > 0".into());
            }

            // Build context token (mimics linkerd proxy injector)
            let context_token = build_context_token(
                pod_name.as_deref(),
                pod_namespace.as_deref(),
                node_name.as_deref(),
            )?;

            tracing::info!(context_token, "Built context token");

            // Create Kubernetes client
            let client = kube::Client::try_default().await?;

            // Set up metrics
            let mut registry = prometheus_client::registry::Registry::default();
            let metrics = client::ClientMetrics::new(&mut registry);

            // Create client controller
            let controller = client::ClientController::new(
                client,
                destination_addr,
                namespace,
                context_token,
                metrics,
            );

            // TODO: Start metrics server on metrics_port

            // Run Get requests
            controller
                .run_get_requests(service_label_selector, watchers_per_service)
                .await?;
        }
    }

    Ok(())
}

/// Build a context token for destination service requests
/// Format matches what linkerd proxy-injector does: {"ns":"namespace","nodeName":"node","pod":"podname"}
fn build_context_token(
    pod_name: Option<&str>,
    pod_namespace: Option<&str>,
    node_name: Option<&str>,
) -> Result<String, Box<dyn std::error::Error>> {
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
