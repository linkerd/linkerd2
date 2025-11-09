# Destination.Get Metrics - Implementation Summary

## Overview

Implemented a minimal, high-signal set of pure aggregate Prometheus metrics for the refactored destination.Get endpoint streaming path. These metrics replace the previous queue-based overflow counter and complement existing gRPC metrics with implementation-specific observability.

## Metrics Implemented (3 total)

### 1. Endpoint Views (Core Refactor Metric)

- **`destination_endpoint_views_active`** (Gauge)
  - Tracks number of active endpoint views (topic subscriptions)
  - Critical for detecting view leaks → memory leaks
  - Multiple views may exist per stream (e.g., federated services)
  - **Cannot derive from gRPC metrics** - internal implementation detail

### 2. Backpressure & Client Health

- **`destination_stream_send_timeouts_total`** (Counter)
  - Counts stream.Send operations that timed out
  - Replaces old `endpoint_updates_queue_overflow` metric
  - Indicates slow/stuck clients triggering stream reset
  - **Alert condition**: Non-zero rate requires investigation
  - **Cannot derive from gRPC metrics** - unbuffered channel timeout mechanism

- **`destination_stream_send_duration_seconds`** (Histogram)
  - Tracks latency of individual stream.Send operations
  - Buckets: 1ms, 10ms, 100ms, 1s, 5s
  - Use: Identify whether slowness is in K8s watch, filtering, or network
  - **Cannot derive from gRPC metrics** - more granular than total RPC duration

## Stream Lifecycle Metrics (Use gRPC Metrics Instead)

Stream lifecycle is already instrumented by `grpc_prometheus` interceptor:

- **In-flight streams**: `grpc_server_started_total{grpc_method="Get"} - grpc_server_handled_total{grpc_method="Get"}`
- **Stream duration**: `grpc_server_handling_seconds{grpc_method="Get"}`
- **Stream completions**: `grpc_server_handled_total{grpc_method="Get",grpc_code="..."}`

## Design Decisions

### ✅ Pure Aggregate (No Service Labels)

- **Rationale**: Minimize cardinality and memory footprint
- **Trade-off**: Can't immediately identify problematic service
- **Mitigation**: Use structured logging with service context for debugging
- **Pattern**: Follows Kubernetes controller best practices

### ✅ Minimal Implementation (Leverage Existing Instrumentation)

- **3 metrics total** - only what gRPC and watcher layers cannot provide
- **gRPC metrics** already cover stream lifecycle (started/completed/duration)
- **Watcher metrics** already cover per-service observability (subscribers/updates/pods)
- **API metrics** focus on implementation-specific details:
  - View subscriptions (new refactor abstraction)
  - Timeout-based backpressure (unbuffered channel mechanism)
  - Send operation granularity (vs total RPC time)

### ✅ Dropped Metrics & Why

- ❌ `destination_get_streams_active` - Use `grpc_server_started_total - grpc_server_handled_total`
- ❌ `destination_get_stream_duration_seconds` - Use `grpc_server_handling_seconds`
- ❌ `snapshots_processed` - Normal operation, not actionable
- ❌ `addresses_filtered` - Optimization detail, not operationally relevant
- ❌ `updates_enqueued` - Redundant with snapshot processing
- ❌ `topic_notifications` - Internal plumbing detail

### ✅ Actionable Alerts

Each metric answers a specific operational question:

1. **Are views leaking?** → `destination_endpoint_views_active` trending up
2. **Are clients stuck?** → `destination_stream_send_timeouts_total` > 0
3. **Is Send slow?** → `destination_stream_send_duration_seconds` p95 high
4. **Are streams leaking?** → Use `grpc_server_started_total - grpc_server_handled_total`
5. **What's normal stream lifetime?** → Use `grpc_server_handling_seconds`

## Code Changes

### New Files

- `controller/api/destination/endpoint_metrics.go` - Metric definitions and helpers
- `controller/api/destination/endpoint_metrics_test.go` - Metrics unit tests

### Modified Files

- `controller/api/destination/endpoint_view.go`
  - Removed `overflowCounter prometheus.Counter` field
  - Added `metrics *viewMetrics` field
  - Updated `newEndpointView()` to use `newViewMetrics()`
  - Updated `close()` to call `metrics.close()`
  - Removed prometheus import

- `controller/api/destination/endpoint_stream_dispatcher.go`
  - Updated `process()` to observe send duration
  - Updated `enqueue()` to call `observeSendTimeout()` instead of incrementing overflow counter
  - Removed prometheus import and overflow counter parameter

- `controller/api/destination/endpoint_translator.go`
  - Removed `updatesQueueOverflowCounter` metric definition
  - Removed prometheus imports

## Usage Examples

### Prometheus Queries

```promql
# In-flight streams (using gRPC metrics)
grpc_server_started_total{grpc_method="Get"} 
  - grpc_server_handled_total{grpc_method="Get"}

# Detect view leaks (memory leak indicator)
destination_endpoint_views_active > 1000

# Alert on stuck clients
rate(destination_stream_send_timeouts_total[5m]) > 0.1

# P95 send latency
histogram_quantile(0.95, 
  rate(destination_stream_send_duration_seconds_bucket[5m]))

# P95 stream lifetime (using gRPC metrics)
histogram_quantile(0.95,
  rate(grpc_server_handling_seconds_bucket{grpc_method="Get"}[5m]))
```

### Grafana Dashboard Ideas

```
Panel 1: Active Streams & Views
  - grpc_server_started_total{grpc_method="Get"} - grpc_server_handled_total{grpc_method="Get"}
  - destination_endpoint_views_active
  
Panel 2: Send Health
  - rate(destination_stream_send_timeouts_total[5m])
  - histogram_quantile(0.95, rate(destination_stream_send_duration_seconds_bucket[5m]))
  
Panel 3: Stream Lifetime Distribution (gRPC metrics)
  - histogram_quantile(0.50, rate(grpc_server_handling_seconds_bucket{grpc_method="Get"}[5m]))
  - histogram_quantile(0.95, rate(grpc_server_handling_seconds_bucket{grpc_method="Get"}[5m]))
  - histogram_quantile(0.99, rate(grpc_server_handling_seconds_bucket{grpc_method="Get"}[5m]))
```

## Testing

All tests pass:

- ✅ Unit tests for metric helpers (`TestViewMetrics`, `TestSendMetrics`)
- ✅ Full destination package test suite
- ✅ No regressions in existing functionality

## Migration Notes

### Removed Metrics

- **`endpoint_updates_queue_overflow{service}`** → Replaced by `destination_stream_send_timeouts_total`

### For Operators

- Update any existing alerts/dashboards using `endpoint_updates_queue_overflow`
- New metric has different semantics: timeout-based vs queue-based
- No service label means alerts are aggregate across all services
- Use logs with service context to identify specific problematic services

## Performance Impact

- **Memory**: ~3 time series (vs 100s-1000s with service labels)
- **CPU**: Minimal - simple counter/gauge/histogram operations
- **Scrape time**: Negligible - small number of metrics
- **Cardinality**: Constant regardless of service count
- **Total instrumentation**: 3 custom + existing gRPC metrics

## Future Enhancements (Optional)

If pure aggregate proves insufficient for debugging:

1. Add service label only to `destination_stream_send_timeouts_total`
2. Add exemplars to histograms (requires Prometheus 2.26+)
3. Add tracing integration for deep debugging
