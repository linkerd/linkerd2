# Monitoring Dashboard Access

This directory contains monitoring configuration for the destination load test infrastructure.

## Architecture

- **kube-prometheus-stack**: Core monitoring infrastructure (Prometheus + Grafana)
- **linkerd-monitoring**: Linkerd-specific dashboards and ServiceMonitors
- **Custom scrape configs**: Additional targets for dst-load-controller metrics

## Accessing Grafana

After deploying with `helmfile sync`, Grafana is available via NodePort:

```bash
# Get the NodePort
kubectl -n monitoring get svc kube-prometheus-stack-grafana

# Forward the port to localhost
kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3000:80

# Open in browser
open http://localhost:3000
```

**Default credentials:**

- Username: `admin`
- Password: `admin`

## Available Dashboards

### Linkerd Dashboards (from linkerd-monitoring chart)

- **Linkerd Top**: Overview of all meshed workloads
- **Linkerd Deployment**: Per-deployment metrics
- **Linkerd Pod**: Per-pod metrics
- **Linkerd Service**: Per-service metrics
- **Linkerd Namespace**: Per-namespace aggregated metrics
- **Linkerd Health**: Control plane health metrics
- **Linkerd Authority**: Destination service metrics
- **Linkerd Route**: HTTPRoute metrics (if using policy)

### Exploring dst-load-controller Metrics

In Grafana, go to **Explore** and query:

**Client Controller Metrics:**

```promql
# Active gRPC streams
client_streams_active

# Updates received over time
rate(client_updates_received_total[1m])

# Current endpoints per service
client_endpoints_current

# Stream errors
rate(client_stream_errors_total[1m])
```

**Scale Controller Metrics:**

```promql
# Current replica counts
churn_deployments_current_replicas

# Scale operations over time
rate(churn_scale_operations_total[1m])

# Time spent at each scale level
churn_hold_duration_seconds
```

**Linkerd Destination Service Load:**

```promql
# Request rate to destination service
rate(request_total{deployment="linkerd-destination"}[1m])

# Destination service latency
histogram_quantile(0.99, rate(response_latency_ms_bucket{deployment="linkerd-destination"}[1m]))

# Active gRPC streams on destination service
grpc_server_handling_seconds_count{grpc_method="Get", grpc_service="io.linkerd.proxy.destination.Destination"}
```

## Custom Dashboards

To add custom dashboards for dst-load-test:

1. Create a dashboard JSON in `values/dashboards/`
2. Update `values/linkerd-monitoring.yaml` to include the dashboard
3. Run `helmfile sync` to apply

Example structure:

```yaml
# values/linkerd-monitoring.yaml
grafanaDashboards:
  dst-load-test:
    json: |
      {{ readFile "values/dashboards/dst-load-test.json" | quote }}
```

## Troubleshooting

**Metrics not appearing?**

Check Prometheus targets:

```bash
kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090
open http://localhost:9090/targets
```

Look for:

- `linkerd-controller` job (should show linkerd control plane pods)
- `dst-load-controller` job (should show client/churn pods in dst-test namespace)

**ServiceMonitors not being picked up?**

Check ServiceMonitor labels match Prometheus selector:

```bash
kubectl -n monitoring get prometheus kube-prometheus-stack-prometheus -o yaml | grep serviceMonitorSelector -A5
```

## Metrics Reference

### Client Controller

| Metric | Type | Description |
|--------|------|-------------|
| `client_streams_active` | Gauge | Number of active gRPC streams to destination service |
| `client_updates_received_total` | Counter | Total updates received (Add/Remove/NoEndpoints) |
| `client_endpoints_current` | Gauge | Current number of endpoints for each service |
| `client_stream_errors_total` | Counter | Total stream errors (connection failures, etc.) |

Labels: `target` (service FQDN), `request_type` (always "Get")

### Scale Controller

| Metric | Type | Description |
|--------|------|-------------|
| `churn_deployments_current_replicas` | Gauge | Current replica count for each deployment |
| `churn_scale_operations_total` | Counter | Total scale operations performed |
| `churn_hold_duration_seconds` | Histogram | Time spent holding at min/max replicas |

Labels: `deployment`, `namespace`, `pattern` (oscillate/stable)
