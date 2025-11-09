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

    /// Client controller: Creates gRPC clients and subscribes to Destination service (runs in source cluster)
    Client {
        /// Destination service address (e.g., "linkerd-dst.linkerd:8086")
        #[arg(long)]
        destination_addr: String,

        /// Number of concurrent gRPC Get requests
        #[arg(long, default_value = "0")]
        get_requests: u32,

        /// Number of concurrent gRPC GetProfile requests
        #[arg(long, default_value = "0")]
        get_profile_requests: u32,

        /// Target Service addresses to subscribe to (format: "svc.namespace.svc.cluster.local:port")
        #[arg(long)]
        target_services: Vec<String>,

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
            get_requests,
            get_profile_requests,
            target_services,
            metrics_port,
        } => {
            tracing::info!(
                destination_addr,
                get_requests,
                get_profile_requests,
                ?target_services,
                metrics_port,
                "Starting client controller"
            );

            if target_services.is_empty() {
                return Err("At least one target service must be specified".into());
            }

            if get_requests == 0 && get_profile_requests == 0 {
                return Err("At least one of --get-requests or --get-profile-requests must be > 0".into());
            }

            // Set up metrics
            let mut registry = prometheus_client::registry::Registry::default();
            let metrics = client::ClientMetrics::new(&mut registry);

            // Create client controller
            let controller = client::ClientController::new(destination_addr, metrics);

            // TODO: Start metrics server on metrics_port

            // Run Get requests if requested
            if get_requests > 0 {
                // Replicate target services to match requested concurrency
                let mut targets = Vec::new();
                for _ in 0..get_requests {
                    targets.extend(target_services.clone());
                }
                
                controller.run_get_requests(targets).await?;
            }

            // Run GetProfile requests if requested
            if get_profile_requests > 0 {
                let mut targets = Vec::new();
                for _ in 0..get_profile_requests {
                    targets.extend(target_services.clone());
                }
                
                controller.run_get_profile_requests(targets).await?;
            }
        }
    }

    Ok(())
}
