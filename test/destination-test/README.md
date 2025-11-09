# Destination Service Load Testing

This directory contains load testing infrastructure for Linkerd's destination service.

## Overview

The `dst-load-controller` is a Rust binary with two subcommands:

- **`churn`**: Creates and manages Services/Deployments in a target cluster
- **`client`**: Creates gRPC clients that subscribe to the Destination service

## Architecture

The load testing framework uses:

- **Helmfile** for declarative cluster configuration (production-aligned)
- **KWOK** for creating fake Pods/Nodes without actual containers
- **Linkerd** installed via Helm (matching customer deployments)
- **step-cli** for generating shared trust roots (multicluster identity)
- **kube-prometheus-stack** (optional) for metrics collection

## Prerequisites

1. **Tools** (all available in dev container):
   - `k3d` - local Kubernetes clusters
   - `helm` - Kubernetes package manager
   - `helmfile` - declarative Helm release management
   - `step` - certificate generation
   - `kubectl` - Kubernetes CLI
   - `linkerd` - Linkerd CLI (for linking clusters)
   - Rust toolchain - for building `dst-load-controller`

2. **Container registry access**:
   - Images will be built locally and loaded into k3d clusters

## Quick Start

### 1. Generate Shared Trust Root

```bash
# Generate certificates for Linkerd identity
./hack/gen-certs.sh
export LINKERD_CA_DIR=/tmp/linkerd-ca
```

This creates:

- `ca.crt` - Trust anchor
- `issuer.crt` - Issuer certificate  
- `issuer.key` - Issuer private key

### 2. Create k3d Cluster

```bash
# Create test cluster
k3d cluster create test \
  --no-lb \
  --k3s-arg '--disable=local-storage,traefik,servicelb,metrics-server@server:*'
```

### 3. Deploy Infrastructure (Linkerd + KWOK)

```bash
# Install all dependencies via Helmfile
LINKERD_CA_DIR=/tmp/linkerd-ca helmfile sync

# Wait for Linkerd to be ready
linkerd check
```

### 4. Optional: Install Monitoring

```bash
# Install kube-prometheus-stack
LINKERD_CA_DIR=/tmp/linkerd-ca helmfile --state-values-set monitoring.enabled=true apply
```

### 5. Build and Deploy Load Controllers

```bash
# Build the Rust binary
cargo build --release

# Build Docker image
docker build -t dst-load-controller:latest -f Dockerfile .

# Load into k3d cluster
k3d image import dst-load-controller:latest --cluster test

# Deploy controllers
# TODO: Helm chart for deploying controllers
```

## Running Load Tests

### Scenario 1: Baseline (Stable Observation Load)

```bash
# Create 10 stable services with 100 endpoints each
dst-load-controller churn \
  --namespace=dst-load-test \
  --stable-services=10 \
  --stable-endpoints=100

# In another terminal: 100 clients watching all services
dst-load-controller client \
  --destination-addr=linkerd-dst.linkerd:8086 \
  --target-services=$(for i in {0..9}; do echo "stable-svc-$i.dst-load-test.svc.cluster.local:8080"; done | paste -sd,)
```

### Scenario 2: Small Oscillation (Autoscaler Pattern)

```bash
# 10 services oscillating 10→200→10 endpoints
dst-load-controller churn \
  --namespace=dst-load-test \
  --oscillate-services=10 \
  --oscillate-min-endpoints=10 \
  --oscillate-max-endpoints=200 \
  --oscillate-hold-duration=2m \
  --oscillate-jitter-percent=5

# In another terminal: 100 clients watching oscillating services
dst-load-controller client \
  --destination-addr=linkerd-dst.linkerd:8086 \
  --target-services=$(for i in {0..9}; do echo "oscillate-svc-$i.dst-load-test.svc.cluster.local:8080"; done | paste -sd,)
```

## Observability

### Prometheus Metrics

Access Prometheus:

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
# Open http://localhost:9090
```

Key metrics to monitor:

```promql
# Destination controller
destination_endpoint_views_active
rate(destination_stream_send_timeouts_total[5m])
container_memory_working_set_bytes{pod=~"linkerd-destination-.*"}

# Load controller metrics
churn_services_created_total
churn_scale_events_total
churn_current_replicas
```

### Grafana Dashboards

Access Grafana:

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80
# Open http://localhost:3000 (admin/admin)
```

## Development

### Building the Binary

```bash
cargo build                  # Debug build
cargo build --release        # Release build
cargo run -- churn --help    # Test CLI
```

### Testing Certificate Generation

```bash
./hack/gen-certs.sh /tmp/test-ca
step certificate inspect /tmp/test-ca/ca.crt
step certificate inspect /tmp/test-ca/issuer.crt
```

### Cleanup

```bash
# Uninstall everything
helmfile destroy

# Delete cluster
k3d cluster delete test

# Clean certificates
rm -rf /tmp/linkerd-ca
```

## Project Structure

```
test/destination-test/
├── Cargo.toml                   # Rust workspace
├── Dockerfile                   # Container build
├── helmfile.yaml                # Infrastructure as code
├── README.md                    # This file
├── dst-load-controller/         # Binary crate
│   ├── Cargo.toml
│   └── src/
│       ├── main.rs              # CLI + orchestration
│       └── churn.rs             # Service/Deployment churn logic
├── hack/
│   └── gen-certs.sh             # Certificate generation (step)
└── values/
    ├── kube-prometheus-stack.yaml
    ├── linkerd-multicluster-source.yaml
    └── linkerd-multicluster-target.yaml
```

## See Also

- [LOAD_TEST_PLAN.md](../../controller/api/destination/LOAD_TEST_PLAN.md) - Full test scenarios and architecture
- [Linkerd Helm docs](https://linkerd.io/2/tasks/install-helm/) - Production Helm installation guide
- [KWOK documentation](https://kwok.sigs.k8s.io/) - Fake node/pod simulation
