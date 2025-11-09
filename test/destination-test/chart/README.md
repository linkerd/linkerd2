# dst-load-test Helm Chart

This Helm chart deploys load testing controllers for Linkerd's destination service.

## Components

- **Churn Controller**: Creates and manages Services/Deployments to simulate endpoint churn
- **Client Controller**: Creates gRPC clients that subscribe to the Destination service (coming soon)

## Prerequisites

- Kubernetes cluster (local k3d recommended)
- Linkerd control plane installed
- KWOK for fake pod simulation
- Docker image built and imported into cluster

## Building and Loading Image

```bash
# Build Docker image
docker build -t dst-load-controller:latest -f Dockerfile .

# Load into k3d cluster
k3d image import dst-load-controller:latest --cluster k3s-default
```

## Installation

```bash
# Create test namespace first
kubectl create namespace dst-test

# Install with default values (10 stable services, 100 endpoints each)
helm install dst-load chart/ -n dst-test

# Install with custom churn configuration
helm install dst-load chart/ -n dst-test \
  --set churn.stable.services=20 \
  --set churn.stable.endpoints=50 \
  --set churn.oscillate.services=5 \
  --set churn.oscillate.minEndpoints=10 \
  --set churn.oscillate.maxEndpoints=200
```

## Configuration

### Churn Controller

| Parameter | Description | Default |
|-----------|-------------|---------|
| `churn.enabled` | Enable churn controller | `true` |
| `churn.stable.services` | Number of stable services | `10` |
| `churn.stable.endpoints` | Endpoints per stable service | `100` |
| `churn.oscillate.services` | Number of oscillating services | `5` |
| `churn.oscillate.minEndpoints` | Minimum endpoints for oscillation | `10` |
| `churn.oscillate.maxEndpoints` | Maximum endpoints for oscillation | `200` |
| `churn.oscillate.holdDuration` | Time to hold at min/max | `"30s"` |
| `churn.oscillate.jitterPercent` | Jitter percentage | `10` |
| `churn.metricsPort` | Prometheus metrics port | `8080` |

### Client Controller

| Parameter | Description | Default |
|-----------|-------------|---------|
| `client.enabled` | Enable client controller | `false` |
| `client.replicas` | Number of client pods | `10` |
| `client.destinationAddr` | Linkerd destination service address | `linkerd-dst-headless.linkerd.svc.cluster.local:8086` |
| `client.metricsPort` | Prometheus metrics port | `8080` |

### Common

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Docker image repository | `dst-load-controller` |
| `image.tag` | Docker image tag | `latest` |
| `image.pullPolicy` | Image pull policy | `Never` |
| `testNamespace` | Namespace for test services | `dst-test` |
| `podAnnotations` | Pod annotations (Linkerd injection) | `linkerd.io/inject: enabled` |

## Metrics

Both controllers expose Prometheus metrics on port 8080 (configurable):

### Churn Controller Metrics

- `churn_services_created` - Total services created (by pattern)
- `churn_deployments_created` - Total deployments created
- `churn_scale_events` - Total scale operations (by pattern, service)
- `churn_current_replicas` - Current replica count (by pattern, service)

### Client Controller Metrics (TODO)

- `client_streams_active` - Active gRPC streams
- `client_updates_received` - Destination updates received
- `client_endpoints_watched` - Endpoints being watched

## Uninstallation

```bash
# Remove load test
helm uninstall dst-load -n dst-test

# Clean up test services (created by churn controller)
kubectl delete all --all -n dst-test
```

## Example: Full Load Test

```bash
# 1. Install with large churn
helm install dst-load chart/ -n dst-test \
  --set churn.stable.services=50 \
  --set churn.stable.endpoints=200 \
  --set churn.oscillate.services=10 \
  --set churn.oscillate.minEndpoints=50 \
  --set churn.oscillate.maxEndpoints=500

# 2. Monitor churn metrics
kubectl port-forward -n dst-test pod/dst-load-dst-load-test-churn 8080:8080 &
curl localhost:8080/metrics

# 3. Enable clients (once implemented)
helm upgrade dst-load chart/ -n dst-test \
  --set client.enabled=true \
  --set client.replicas=100 \
  --reuse-values
```
