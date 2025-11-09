//! Client controller: Creates gRPC clients that subscribe to the Destination service

use std::time::Duration;

use k8s_openapi::api::core::v1::Service;
use kube::{
    api::{Api, ListParams},
    Client,
};
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
    pub client: Client,
    pub destination_addr: String,
    pub namespace: String,
    pub context_token: String,
    pub metrics: ClientMetrics,
}

impl ClientController {
    pub fn new(
        client: Client,
        destination_addr: String,
        namespace: String,
        context_token: String,
        metrics: ClientMetrics,
    ) -> Self {
        Self {
            client,
            destination_addr,
            namespace,
            context_token,
            metrics,
        }
    }

    /// Discover services by label selector and create watchers
    pub async fn run_get_requests(
        &self,
        service_label_selector: String,
        watchers_per_service: u32,
    ) -> Result<(), Box<dyn std::error::Error>> {
        info!(
            destination_addr = %self.destination_addr,
            namespace = %self.namespace,
            label_selector = %service_label_selector,
            watchers_per_service,
            "Starting Get requests"
        );

        // Discover services via Kubernetes API
        let services: Api<Service> = Api::namespaced(self.client.clone(), &self.namespace);
        let lp = ListParams::default().labels(&service_label_selector);
        
        let service_list = services.list(&lp).await?;
        
        if service_list.items.is_empty() {
            return Err(format!(
                "No services found with label selector: {}",
                service_label_selector
            )
            .into());
        }

        info!(
            service_count = service_list.items.len(),
            "Discovered services"
        );

        // Connect to destination service
        let channel = Channel::from_shared(format!("http://{}", self.destination_addr))?
            .connect()
            .await?;

        info!("Connected to destination service");

        // Spawn watchers for each service
        let mut tasks = Vec::new();
        for svc in service_list.items {
            let svc_name = svc.metadata.name.as_ref().ok_or("Service missing name")?;
            
            // Get the service port (assume first port)
            let port = svc
                .spec
                .as_ref()
                .and_then(|spec| spec.ports.as_ref())
                .and_then(|ports| ports.first())
                .map(|p| p.port)
                .ok_or("Service missing port")?;

            // Build the destination path (authority)
            let target = format!(
                "{}.{}.svc.cluster.local:{}",
                svc_name, self.namespace, port
            );

            info!(
                service = %svc_name,
                target = %target,
                watchers = watchers_per_service,
                "Creating watchers for service"
            );

            // Spawn multiple watchers for this service
            for watcher_id in 0..watchers_per_service {
                let channel = channel.clone();
                let target = target.clone();
                let context_token = self.context_token.clone();
                let metrics = self.metrics.clone();
                let svc_name = svc_name.clone();

                let task = tokio::spawn(async move {
                    if let Err(e) = subscribe_to_destination(
                        channel,
                        target.clone(),
                        context_token,
                        metrics,
                        watcher_id,
                    )
                    .await
                    {
                        error!(
                            service = %svc_name,
                            target = %target,
                            watcher_id,
                            error = ?e,
                            "Get stream failed"
                        );
                    }
                });
                tasks.push(task);
            }
        }

        // Wait for all tasks (they should run forever)
        futures::future::join_all(tasks).await;

        Ok(())
    }

    /// Run GetProfile requests for the specified target services
    pub async fn run_get_profile_requests(
        &self,
        _service_label_selector: String,
        _watchers_per_service: u32,
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
    context_token: String,
    metrics: ClientMetrics,
    watcher_id: u32,
) -> Result<(), Box<dyn std::error::Error>> {
    let mut client = dst_api::destination_client::DestinationClient::new(channel);

    info!(
        target = %target,
        watcher_id,
        "Subscribing to destination"
    );

    let labels = ClientLabels {
        target: target.clone(),
        request_type: "Get".to_string(),
    };

    loop {
        // Create Get request with context token
        let request = tonic::Request::new(dst_api::GetDestination {
            scheme: "k8s".to_string(),
            path: target.clone(),
            context_token: context_token.clone(),
        });

        // Track active stream
        metrics.streams_active.get_or_create(&labels).inc();

        // Subscribe to stream
        match client.get(request).await {
            Ok(response) => {
                let mut stream = response.into_inner();
                info!(
                    target = %target,
                    watcher_id,
                    "Stream established"
                );

                // Process updates
                while let Ok(Some(update)) = stream.message().await {
                    handle_update(&target, update, &metrics, &labels, watcher_id);
                }

                warn!(
                    target = %target,
                    watcher_id,
                    "Stream ended, reconnecting..."
                );
            }
            Err(e) => {
                error!(
                    target = %target,
                    watcher_id,
                    error = ?e,
                    "Failed to establish stream"
                );
                metrics.stream_errors.get_or_create(&labels).inc();
            }
        }

        // Stream closed, mark as inactive
        metrics.streams_active.get_or_create(&labels).dec();

        // Wait before reconnecting
        sleep(Duration::from_secs(5)).await;
    }
}

/// Handle a destination update
fn handle_update(
    target: &str,
    update: dst_api::Update,
    metrics: &ClientMetrics,
    labels: &ClientLabels,
    watcher_id: u32,
) {
    metrics.updates_received.get_or_create(labels).inc();

    match update.update {
        Some(dst_api::update::Update::Add(add)) => {
            let endpoint_count = add.addrs.len();
            info!(
                target = %target,
                watcher_id,
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
                watcher_id,
                removed = remove.addrs.len(),
                "Received Remove update"
            );
        }
        Some(dst_api::update::Update::NoEndpoints(no_endpoints)) => {
            info!(
                target = %target,
                watcher_id,
                exists = no_endpoints.exists,
                "Received NoEndpoints update"
            );
            metrics.endpoints_current.get_or_create(labels).set(0);
        }
        None => {
            warn!(
                target = %target,
                watcher_id,
                "Received update with no data"
            );
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
