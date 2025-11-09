//! Client controller: Creates gRPC clients that subscribe to the Destination service

use std::time::Duration;

use linkerd2_proxy_api::destination as dst_api;
use prometheus_client::{
    encoding::EncodeLabelSet,
    metrics::{counter::Counter, family::Family, gauge::Gauge},
    registry::Registry,
};
use tokio::time::sleep;
use tonic::transport::Channel;
use tracing::{error, info, warn};

#[derive(Clone, Debug, Hash, PartialEq, Eq, EncodeLabelSet)]
pub struct ClientLabels {
    target: String,
    request_type: String,
}

pub struct ClientMetrics {
    pub streams_active: Family<ClientLabels, Gauge>,
    pub updates_received: Family<ClientLabels, Counter>,
    pub endpoints_current: Family<ClientLabels, Gauge>,
    pub stream_errors: Family<ClientLabels, Counter>,
}

impl ClientMetrics {
    pub fn new(registry: &mut Registry) -> Self {
        let streams_active = Family::default();
        let updates_received = Family::default();
        let endpoints_current = Family::default();
        let stream_errors = Family::default();

        registry.register(
            "client_streams_active",
            "Number of active gRPC streams",
            streams_active.clone(),
        );
        registry.register(
            "client_updates_received",
            "Total number of updates received",
            updates_received.clone(),
        );
        registry.register(
            "client_endpoints_current",
            "Current number of endpoints for a service",
            endpoints_current.clone(),
        );
        registry.register(
            "client_stream_errors",
            "Total number of stream errors",
            stream_errors.clone(),
        );

        Self {
            streams_active,
            updates_received,
            endpoints_current,
            stream_errors,
        }
    }
}

pub struct ClientController {
    pub destination_addr: String,
    pub metrics: ClientMetrics,
}

impl ClientController {
    pub fn new(destination_addr: String, metrics: ClientMetrics) -> Self {
        Self {
            destination_addr,
            metrics,
        }
    }

    /// Run Get requests for the specified target services
    pub async fn run_get_requests(
        &self,
        target_services: Vec<String>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        info!(
            destination_addr = %self.destination_addr,
            targets = ?target_services,
            "Starting Get requests"
        );

        // Connect to destination service
        let channel = Channel::from_shared(format!("http://{}", self.destination_addr))?
            .connect()
            .await?;

        info!("Connected to destination service");

        // Spawn a task for each target service
        let mut tasks = Vec::new();
        for target in target_services {
            let channel = channel.clone();
            let metrics = self.metrics.clone();
            let task = tokio::spawn(async move {
                if let Err(e) = subscribe_to_destination(channel, target.clone(), metrics).await {
                    error!(target = %target, error = ?e, "Get stream failed");
                }
            });
            tasks.push(task);
        }

        // Wait for all tasks (they should run forever)
        futures::future::join_all(tasks).await;

        Ok(())
    }

    /// Run GetProfile requests for the specified target services
    pub async fn run_get_profile_requests(
        &self,
        _target_services: Vec<String>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        warn!("GetProfile not yet implemented");
        // TODO: Implement GetProfile streams
        Ok(())
    }
}

/// Subscribe to a destination service and process updates
async fn subscribe_to_destination(
    channel: Channel,
    target: String,
    metrics: ClientMetrics,
) -> Result<(), Box<dyn std::error::Error>> {
    let mut client = dst_api::destination_client::DestinationClient::new(channel);

    info!(target = %target, "Subscribing to destination");

    let labels = ClientLabels {
        target: target.clone(),
        request_type: "Get".to_string(),
    };

    loop {
        // Create Get request
        let request = tonic::Request::new(dst_api::GetDestination {
            scheme: "k8s".to_string(),
            path: target.clone(),
            context_token: String::new(),
        });

        // Track active stream
        metrics.streams_active.get_or_create(&labels).set(1);

        // Subscribe to stream
        match client.get(request).await {
            Ok(response) => {
                let mut stream = response.into_inner();
                info!(target = %target, "Stream established");

                // Process updates
                while let Ok(Some(update)) = stream.message().await {
                    handle_update(&target, update, &metrics, &labels);
                }

                warn!(target = %target, "Stream ended, reconnecting...");
            }
            Err(e) => {
                error!(target = %target, error = ?e, "Failed to establish stream");
                metrics.stream_errors.get_or_create(&labels).inc();
            }
        }

        // Stream closed, mark as inactive
        metrics.streams_active.get_or_create(&labels).set(0);

        // Wait before reconnecting
        sleep(Duration::from_secs(5)).await;
    }
}

/// Handle a destination update
fn handle_update(target: &str, update: dst_api::Update, metrics: &ClientMetrics, labels: &ClientLabels) {
    metrics.updates_received.get_or_create(labels).inc();

    match update.update {
        Some(dst_api::update::Update::Add(add)) => {
            let endpoint_count = add.addrs.len();
            info!(
                target = %target,
                endpoints = endpoint_count,
                "Received Add update"
            );
            metrics
                .endpoints_current
                .get_or_create(labels)
                .set(endpoint_count as i64);
        }
        Some(dst_api::update::Update::Remove(remove)) => {
            info!(
                target = %target,
                removed = remove.addrs.len(),
                "Received Remove update"
            );
        }
        Some(dst_api::update::Update::NoEndpoints(no_endpoints)) => {
            info!(
                target = %target,
                exists = no_endpoints.exists,
                "Received NoEndpoints update"
            );
            metrics.endpoints_current.get_or_create(labels).set(0);
        }
        None => {
            warn!(target = %target, "Received update with no data");
        }
    }
}

impl Clone for ClientMetrics {
    fn clone(&self) -> Self {
        Self {
            streams_active: self.streams_active.clone(),
            updates_received: self.updates_received.clone(),
            endpoints_current: self.endpoints_current.clone(),
            stream_errors: self.stream_errors.clone(),
        }
    }
}
