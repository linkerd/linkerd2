use clap::{Parser, Subcommand};

mod churn;

#[derive(Parser)]
#[command(name = "dst-load-controller")]
#[command(about = "Destination service load testing controller", long_about = None)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Churn controller: Creates and manages Services/Deployments (runs in target cluster)
    Churn {
        /// Number of stable Services to create
        #[arg(long, default_value = "0")]
        stable_services: u32,

        /// Number of stable endpoints per Service
        #[arg(long, default_value = "0")]
        stable_endpoints: u32,

        /// Number of oscillating Services to create
        #[arg(long, default_value = "0")]
        oscillate_services: u32,

        /// Minimum endpoints per oscillating Service
        #[arg(long, default_value = "0")]
        oscillate_min_endpoints: u32,

        /// Maximum endpoints per oscillating Service
        #[arg(long, default_value = "0")]
        oscillate_max_endpoints: u32,

        /// Hold time at min/max before changing (e.g., "30s", "1m")
        #[arg(long, default_value = "30s")]
        oscillate_hold_duration: String,

        /// Jitter percentage (0-100) to spread oscillation timing
        #[arg(long, default_value = "0")]
        oscillate_jitter_percent: u8,

        /// Prometheus metrics port
        #[arg(long, default_value = "8080")]
        metrics_port: u16,

        /// Namespace to create resources in
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
        Commands::Churn {
            stable_services,
            stable_endpoints,
            oscillate_services,
            oscillate_min_endpoints,
            oscillate_max_endpoints,
            oscillate_hold_duration,
            oscillate_jitter_percent,
            metrics_port,
            namespace,
        } => {
            tracing::info!(
                stable_services,
                stable_endpoints,
                oscillate_services,
                oscillate_min_endpoints,
                oscillate_max_endpoints,
                oscillate_hold_duration,
                oscillate_jitter_percent,
                metrics_port,
                namespace,
                "Starting churn controller"
            );

            // Validate inputs
            if stable_services == 0 && oscillate_services == 0 {
                return Err("Must specify either --stable-services or --oscillate-services".into());
            }

            if stable_services > 0 && stable_endpoints == 0 {
                return Err("--stable-endpoints must be > 0 when using --stable-services".into());
            }

            if oscillate_services > 0 {
                if oscillate_min_endpoints == 0 || oscillate_max_endpoints == 0 {
                    return Err(
                        "--oscillate-min-endpoints and --oscillate-max-endpoints must be > 0"
                            .into(),
                    );
                }
                if oscillate_min_endpoints >= oscillate_max_endpoints {
                    return Err("--oscillate-min-endpoints must be < --oscillate-max-endpoints".into());
                }
                if oscillate_jitter_percent > 100 {
                    return Err("--oscillate-jitter-percent must be 0-100".into());
                }
            }

            let hold_duration = churn::parse_duration(&oscillate_hold_duration)?;

            // Create Kubernetes client
            let client = kube::Client::try_default().await?;

            // Set up metrics
            let mut registry = prometheus_client::registry::Registry::default();
            let metrics = churn::ChurnMetrics::new(&mut registry);

            // Create churn controller
            let controller = churn::ChurnController::new(client, namespace, metrics);

            // TODO: Start metrics server on metrics_port

            // Run the appropriate pattern
            if stable_services > 0 && oscillate_services == 0 {
                // Pure stable
                controller
                    .run_stable(stable_services, stable_endpoints)
                    .await?;
            } else if stable_services == 0 && oscillate_services > 0 {
                // Pure oscillate
                controller
                    .run_oscillate(
                        oscillate_services,
                        oscillate_min_endpoints,
                        oscillate_max_endpoints,
                        hold_duration,
                        oscillate_jitter_percent,
                    )
                    .await?;
            } else {
                // Mixed mode - run both patterns concurrently
                tracing::info!("Running mixed stable + oscillate patterns");

                let stable_client = controller.client.clone();
                let stable_namespace = controller.namespace.clone();
                let stable_metrics = churn::ChurnMetrics::new(&mut registry);
                let stable_controller =
                    churn::ChurnController::new(stable_client, stable_namespace, stable_metrics);

                let oscillate_client = controller.client.clone();
                let oscillate_namespace = controller.namespace.clone();
                let oscillate_metrics = churn::ChurnMetrics::new(&mut registry);
                let oscillate_controller = churn::ChurnController::new(
                    oscillate_client,
                    oscillate_namespace,
                    oscillate_metrics,
                );

                tokio::select! {
                    result = stable_controller.run_stable(stable_services, stable_endpoints) => {
                        result?;
                    }
                    result = oscillate_controller.run_oscillate(
                        oscillate_services,
                        oscillate_min_endpoints,
                        oscillate_max_endpoints,
                        hold_duration,
                        oscillate_jitter_percent,
                    ) => {
                        result?;
                    }
                }
            }
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

            // TODO: Implement client controller
            tracing::warn!("Client controller not yet implemented");
        }
    }

    Ok(())
}
