# Destination Service Load Testing Plan

## Overview

This load testing framework validates the refactored destination service under
realistic cluster conditions, measuring both performance characteristics and
resource consumption. The test suite supports both single-cluster and federated
multi-cluster scenarios.

## Architecture

### Single-Cluster Mode

```text
┌─────────────────────────────────────────────────────────────────┐
│ Source Cluster (k3d)                                            │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Linkerd Control Plane                                    │   │
│  │ - Destination controller serves gRPC Get streams         │   │
│  │ - Watches local K8s endpoints/services                   │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ dst-load-controller client                               │   │
│  │ - Establishes gRPC connections to destination controller │   │
│  │ - Concurrent Get() streams at configured concurrency     │   │
│  │ - Measures latency (change → observation)                │   │
│  │ - Emits Prometheus metrics                               │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ dst-load-controller churn                                │   │
│  │ - Creates/updates/deletes K8s services and deployments   │   │
│  │ - Simulates churn patterns (rolling, spike, etc.)        │   │
│  │ - Timestamps events for latency measurement              │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Test Workloads                                           │   │
│  │ - Services, Deployments, Pods                            │   │
│  │ - Manipulated by churn controller                        │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ kube-prometheus-stack                                    │   │
│  │ - Prometheus for metrics scraping                        │   │
│  │ - Grafana for visualization                              │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### Federated Multi-Cluster Mode

```text
┌─────────────────────────────────────────────────────────────────┐
│ Source Cluster (k3d)                                            │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Linkerd Control Plane + Multicluster Extension           │   │
│  │ - Destination controller serves gRPC Get streams         │   │
│  │ - Watches local + remote endpoints via Link CRD         │   │
│  │ - Remote cluster credentials configured                  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ dst-load-controller client                               │   │
│  │ - Establishes gRPC connections to LOCAL destination ctrl │   │
│  │ - Receives federated view of local + remote endpoints    │   │
│  │ - Measures latency (change → observation)                │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ kube-prometheus-stack                                    │   │
│  │ - Prometheus, Grafana                                    │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              ↕ 
            Flat Network (pod CIDR routes)
                              ↕
┌─────────────────────────────────────────────────────────────────┐
│ Target Cluster (k3d)                                            │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ dst-load-controller churn                                │   │
│  │ - Creates/updates/deletes K8s workloads                  │   │
│  │ - Simulates churn patterns                               │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Test Workloads                                           │   │
│  │ - Services, Deployments, Pods (no Linkerd)              │   │
│  │ - Discovered by source cluster via multicluster         │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

**Key Design Decisions:**

1. **Client always talks to local destination controller**: In both single and
   federated modes, the client controller establishes gRPC streams to the
   destination controller running in its own cluster.

2. **Destination controller federates the view**: In federated mode, the
   destination controller in the source cluster uses the multicluster extension
   to read remote cluster metadata and present a unified view to clients.

3. **Target cluster has no Linkerd**: For federated tests, the target cluster
   only runs plain Kubernetes resources. The churn controller runs there to
   manipulate workloads.

4. **Single binary, multiple subcommands**: Both churn and client controllers
   are subcommands of a single `dst-load-controller` binary for easier
   versioning and packaging.

5. **Controllers are workload executors, not cluster managers**: Following the
   pattern of tools like k6, the controllers assume clusters already exist and
   are properly configured. They focus solely on executing their specific
   workload (generating churn or client load).

6. **Helm-based deployment**: Controllers are deployed via a Helm chart with
   scenario-specific values files. This provides templating, values composition,
   and lifecycle management while keeping scenarios declarative and versioned.

7. **KWOK for fake workloads**: The churn controller creates standard Kubernetes
   resources (Services, Deployments), while KWOK automatically creates fake Pods
   and Nodes. This provides full resource fidelity for the destination controller
   while using minimal cluster resources and enabling fast scaling.

8. **CLI-first configuration**: Controllers accept configuration via CLI arguments
   for simplicity. YAML scenario files can be added later if needed for complex
   compositions.

## Components

### 1. Load Controller (`dst-load-controller`)

A single Rust binary with two subcommands for generating load and churn.

#### Subcommand: `dst-load-controller churn`

**Purpose**: Simulate realistic service endpoint behavior by creating Kubernetes
resources that KWOK will populate with fake Pods and Nodes.

**How it works:**

1. Churn controller creates Services and Deployments
2. KWOK (installed in cluster) watches Deployments and creates fake Pods
3. Kubernetes automatically creates/updates EndpointSlices based on fake Pods
4. Destination controller sees complete resource churn (Services, Pods, Nodes,
   EndpointSlices)

**Features**:

- Create Services and Deployments via K8s API
- Scale Deployments: 0 → N replicas with controlled ramp-up
- Simulate churn patterns:
  - Scale events (autoscaler adding/removing nodes)
  - Rolling updates (gradual replica changes)
  - Batch operations (multiple services scaling together)
- Timestamped events for end-to-end latency measurement
- Prometheus metrics for churn operations

**Deployment**:

- Deployed as a Pod (not a Deployment - should NOT scale beyond 1 replica)
- Single-cluster mode: Runs in source cluster
- Federated mode: Runs in target cluster
- One controller instance per namespace/scenario (1:1 model)
- RBAC scoped to its own namespace

**Prerequisites**:

- KWOK installed in the cluster (`kwok` and `kwok-controller`)
- KWOK configured to manage fake Pods and Nodes

**CLI Arguments**:

**CLI Arguments**:

```bash
dst-load-controller churn \
  --namespace=test-load \
  --services=10 \
  --endpoints-per-service=100 \
  --pattern=scale-up \
  --scale-from=10 \
  --scale-to=200 \
  --scale-duration=30s \
  --duration=10m
```

**Common patterns:**

```bash
# Baseline: Stable topology, no churn (runs until stopped)
dst-load-controller churn \
  --services=10 \
  --endpoints-per-service=100 \
  --pattern=stable

# Oscillating scale: Realistic autoscaler pattern
dst-load-controller churn \
  --services=10 \
  --pattern=oscillate \
  --min-endpoints=10 \
  --max-endpoints=200 \
  --min-hold=2m \
  --max-hold=5m \
  --jitter=5s

# Same pattern, but stop after 3 complete cycles
dst-load-controller churn \
  --services=10 \
  --pattern=oscillate \
  --min-endpoints=10 \
  --max-endpoints=200 \
  --min-hold=2m \
  --max-hold=5m \
  --jitter=5s \
  --cycles=3
```

**Oscillation pattern lifecycle:**

1. **Initial state**: Create services with `min-endpoints` replicas
2. **Scale up**: All services instantly scale to `max-endpoints` (with jitter)
3. **Max hold**: Stay at `max-endpoints` for `max-hold` duration
4. **Scale down**: All services instantly scale back to `min-endpoints` (with jitter)
5. **Min hold**: Stay at `min-endpoints` for `min-hold` duration
6. **Repeat**: Go to step 2 (until stopped or `--cycles` reached)

**Jitter behavior**: Each service adds random delay (0 to `--jitter`) before
scaling to spread API server load naturally.

**Implementation**:

- Rust using `kube` crate (following `policy-test` patterns)
- Creates Services and Deployments via K8s API
- KWOK handles Pod/Node creation automatically
- Event timestamping for latency measurement
- Prometheus metrics for churn operations

**Example workflow:**

1. Churn controller creates Service: `test-service-1` (port 8080)
2. Churn controller creates Deployment: `test-service-1` (replicas=100)
3. KWOK sees Deployment, creates 100 fake Pods (instantly)
4. Kubernetes creates EndpointSlice with 100 endpoints
5. Destination controller observes new endpoints
6. Churn controller scales Deployment to 200 replicas
7. KWOK creates 100 more fake Pods
8. EndpointSlice updated with 200 endpoints
9. Destination controller observes updates, notifies clients

#### Subcommand: `dst-load-controller client`

**Purpose**: Simulate realistic client behavior and measure destination service
performance.

**Features**:

- Concurrent gRPC Get() stream establishment
- Time-bounded streams (e.g., 60s duration, then reset)
- Periodic reconnection to test stream lifecycle
- Latency measurement:
  - Time from K8s resource change → destination controller notification
  - Time from notification → client observation
  - End-to-end: K8s change → client sees update
- Metrics collection:
  - Stream lifetime distribution
  - Update latency (p50, p95, p99, p99.9)
  - Backpressure events (send timeouts)
  - Connection errors and retries

**Deployment**:

- Deployed as a Deployment (CAN scale horizontally)
- Both single-cluster and federated modes: Always runs in source cluster
- Connects to local destination controller (which federates remote endpoints)
- Scale via replicas to achieve desired total concurrency
- Each replica contributes to aggregate load

**CLI Arguments**:

```bash
# Basic client load (runs until stopped)
dst-load-controller client \
  --destination=linkerd-destination.linkerd.svc.cluster.local:8086 \
  --services=test-service-{1..10}.test-load.svc.cluster.local:8080 \
  --streams-per-service=10

# With stream lifecycle management
dst-load-controller client \
  --destination=linkerd-destination.linkerd.svc.cluster.local:8086 \
  --services=test-service-{1..10}.test-load.svc.cluster.local:8080 \
  --streams-per-service=10 \
  --stream-max-lifetime=5m \
  --stream-reset-interval=2m
```

**Key parameters:**

- `--services`: Service patterns to watch (supports brace expansion)
- `--streams-per-service`: How many concurrent Get() streams per service
- `--stream-max-lifetime`: Optional max lifetime before resetting a stream
- `--stream-reset-interval`: Optional interval to reset all streams (tests reconnection)
- Total concurrency = (# services) × (streams per service) × (# replicas)

**Stream lifecycle:** By default, streams run forever. If `--stream-max-lifetime`
or `--stream-reset-interval` is set, the client will periodically close and
reopen streams to test reconnection behavior.

**Metrics Emitted**:

```promql
# Latency metrics
destination_client_update_latency_seconds{quantile, service}
destination_client_stream_lifetime_seconds{quantile}
destination_client_connection_duration_seconds{quantile}

# Throughput metrics
destination_client_updates_received_total{service}
destination_client_streams_active
destination_client_streams_created_total
destination_client_streams_reset_total

# Error metrics
destination_client_connection_errors_total{reason}
destination_client_send_timeouts_total
```

**Implementation**:

- Rust binary using `linkerd2-proxy-api` gRPC client and `kube` crate
- Follows patterns from `policy-test/src/grpc.rs`
- Prometheus instrumentation via `prometheus-client`
- Structured logging with `tracing`
- Event correlation: churn controller timestamp → client observation timestamp

### 2. Monitoring Stack (kube-prometheus-stack)

**Purpose**: Observe control plane resource usage and system health.

**Key Metrics to Monitor**:

**Control Plane Resources**:

```promql
# Memory usage
container_memory_working_set_bytes{pod=~"linkerd-destination-.*"}

# CPU usage  
rate(container_cpu_usage_seconds_total{pod=~"linkerd-destination-.*"}[5m])

# Goroutines (leak detection)
go_goroutines{job="linkerd-destination"}

# Heap allocations
go_memstats_heap_inuse_bytes{job="linkerd-destination"}
```

**Destination Service Metrics** (from refactor):

```promql
# View tracking (memory leak indicator)
destination_endpoint_views_active

# Backpressure
rate(destination_stream_send_timeouts_total[5m])
histogram_quantile(0.95, rate(destination_stream_send_duration_seconds_bucket[5m]))

# Stream lifecycle (from gRPC metrics)
grpc_server_started_total{grpc_method="Get"} - grpc_server_handled_total{grpc_method="Get"}
histogram_quantile(0.95, rate(grpc_server_handling_seconds_bucket{grpc_method="Get"}[5m]))
```

**Kubernetes API Server**:

```promql
# Watch connection health
apiserver_longrunning_requests{verb="watch"}

# Endpoint churn rate
rate(apiserver_request_total{resource="endpoints|endpointslices",verb="update"}[5m])
```

**Dashboards**:

- Control plane resource usage over time
- Destination service internal metrics
- Client-side latency distributions
- Event correlation timeline (change → observation)

## Test Scenarios

**Focus:** Establish operational envelope (red-line limits) for a single
destination controller under realistic update fanout patterns.

**Core pattern being tested:**

- Many client pods (hundreds to thousands) watching services
- Services oscillate between min/max endpoints (autoscaler pattern)
- Measure: How well does update fanout perform? (1 change → N notifications)
- Compare: Performance across software versions (deterministic, reproducible)

**Philosophy:** Start simple, get to results, iterate. We focus on realistic,
deterministic patterns that enable reproducible performance comparison across
software versions. All scenarios use controlled oscillation patterns with jitter
to simulate production behavior while maintaining predictability.

**Test progression rationale:**

1. **Baseline** → Measure idle cost (no updates)
2. **Small Oscillation** → Basic fanout pattern (10 services × 100 clients)
3. **Large Service Rollout** → Deep fanout (1 service × 1,000 clients) -
   production deployment scenario
4. **Many Services** → Broad fanout (100 services × 100 clients) - cluster-wide
   scaling
5. **Red-Line** → Find breaking point (50 services × 500 clients × 200
   endpoints)

Each scenario is deterministic and repeatable, enabling A/B comparison when
testing destination controller changes.

**Scenario progression:**

1. **Baseline**: Zero churn, pure observation load (resource baseline)
2. **Small Oscillation**: 10 services, 100 clients (basic autoscaler pattern)
3. **Large Service Rollout**: Single large service, many clients (deployment scenario)
4. **Many Services**: 100 services oscillating, moderate clients (broad fanout)
5. **Red-Line Composite**: Combined stress test to find limits

### Scenario 1: Baseline (Stable Observation Load)

**Goal**: Establish baseline resource usage and behavior with stable
topology—no churn at all, just pure observation load.

**Setup**:

- 10 services, 100 endpoints each (1,000 total endpoints)
- 100 client pods, each watching all 10 services (1,000 total streams)
- **Zero churn**: Endpoints remain stable throughout test
- Stream lifecycle: Clients maintain long-lived streams, no artificial resets
- **Run until stopped**: No duration limit, observe steady-state behavior

**Success Criteria** (measured over time):

- Memory stable (no leaks - check hourly for 24h)
- CPU usage measured (establish baseline idle cost)
- All 1,000 streams remain healthy (no unexpected disconnects)
- `destination_endpoint_views_active` = 1,000 (stable)
- Minimal CPU/memory overhead for just holding state

**Key metric:** Resource baseline = CPU/memory usage for N clients × M
services × K endpoints with zero updates

**Why this matters:** Before testing update fanout, we need to understand the
cost of just _holding_ the observation state. This gives us a clean baseline to
compare against when we introduce churn in later scenarios.

### Scenario 2: Small Oscillation (Autoscaler Pattern)

**Goal**: Test update fanout with realistic autoscaler behavior—small number of
services oscillating between min/max capacity.

**Setup**:

- 10 services, starting with 10 endpoints each (minimum)
- 100 client pods, each watching all 10 services (1,000 total streams)
- **Oscillation pattern** (repeats continuously):
  1. Start: 10 endpoints per service (hold for 2 minutes)
  2. Scale up: 10 → 200 endpoints (instant)
  3. Hold: 200 endpoints (hold for 5 minutes)
  4. Scale down: 200 → 10 endpoints (instant)
  5. Return to step 1
- **Jitter**: Each service adds random delay (0-5s) before scaling
- **Run until stopped**: Observe over multiple cycles

**Success Criteria** (measured per cycle):

- p95 update latency < 500ms
- p99 update latency < 2s during scale events
- All clients observe all changes within 10 seconds
- Memory returns to baseline after each cycle (no leaks)
- No stream timeouts or resets

**Key metrics:**

- Time to full fanout per cycle
- Memory delta (max - baseline)
- CPU spike during scale events

**Why this matters:** This is the fundamental autoscaler pattern. Clean,
repeatable cycles enable A/B comparison across software versions.

### Scenario 3: Large Service Rollout (High Client Fanout)

**Goal**: Test fanout performance when many clients watch a single large
service during rollout—simulates production deployment scenario.

**Setup**:

- **1 large service**, starting with 500 endpoints
- **1,000 client pods**, all watching this service (1,000 streams on 1 service)
- **Oscillation pattern** (repeats continuously):
  1. Start: 500 endpoints (hold for 2 minutes)
  2. Scale up: 500 → 1,000 endpoints (instant, simulates doubling capacity)
  3. Hold: 1,000 endpoints (hold for 5 minutes)
  4. Scale down: 1,000 → 500 endpoints (instant)
  5. Return to step 1
- **No jitter**: Single service means no spreading needed
- **Run until stopped**: Observe over multiple cycles

**Success Criteria** (measured per cycle):

- p95 update latency < 1s during 500-endpoint fanout
- p99 update latency < 3s during 500-endpoint fanout
- All 1,000 clients observe all 500 new endpoints within 15 seconds
- Stream send timeout rate < 1%
- Memory spike < 2GB above baseline
- CPU spike measured (this is the high-water mark for single-service fanout)

**Key metrics:**

- Updates delivered per second during scale-up (500 endpoints × 1,000 clients = 500k updates)
- Fanout completion time (when last client gets last endpoint)
- Send buffer saturation (% of streams that experience backpressure)

**Why this matters:** This tests the critical production scenario—rolling out a
large service watched by many clients. Measures controller's ability to handle
massive fanout efficiently.

### Scenario 4: Many Services (Broad Fanout)

**Goal**: Test fanout when clients watch many services oscillating
simultaneously—simulates cluster-wide autoscaling event.

**Setup**:

- **100 services**, starting with 50 endpoints each
- 100 client pods, each watching **all 100 services** (10,000 total streams)
- **Oscillation pattern** (repeats continuously):
  1. Start: 50 endpoints per service (hold for 2 minutes)
  2. Scale up: 50 → 100 endpoints per service (instant)
  3. Hold: 100 endpoints (hold for 5 minutes)
  4. Scale down: 100 → 50 endpoints per service (instant)
  5. Return to step 1
- **Jitter**: Each service adds random delay (0-5s) before scaling
- **Run until stopped**: Observe over multiple cycles

**Success Criteria** (measured per cycle):

- p95 update latency < 1s during scale events
- p99 update latency < 3s during scale events
- All clients observe all changes within 20 seconds
- Stream send timeout rate < 0.5%
- Memory < 6GB
- No crashes or stream resets

**Key metrics:**

- Total updates per cycle (100 services × 50 endpoints × 100 clients = 500k
  updates per scale event)
- Fanout completion time (when last client observes last change)
- CPU/memory overhead vs Scenario 2 (measure cost of broad fanout)

**Why this matters:** Tests controller's ability to handle many services
changing simultaneously (cluster-wide scaling event) with broad client
observation.

### Scenario 5: Red-Line Composite

**Goal**: Find operational limits—combine large service + many clients + many
services simultaneously.

**Setup**:

- 50 services, starting with 100 endpoints each (5,000 total endpoints)
- 500 client pods, each watching all 50 services (25,000 total streams)
- **Oscillation pattern** (repeats continuously):
  1. Start: 100 endpoints per service (hold for 2 minutes)
  2. Scale up: 100 → 200 endpoints per service (instant)
  3. Hold: 200 endpoints (hold for 5 minutes)
  4. Scale down: 200 → 100 endpoints per service (instant)
  5. Return to step 1
- **Jitter**: Each service adds random delay (0-5s) before scaling
- **Run until stopped**: Observe over multiple cycles

**Success Criteria** (measured per cycle):

- System remains stable (no crashes, no stream resets)
- p99 update latency < 10s (degraded but functional)
- Stream send timeout rate < 5%
- Memory < 8GB
- Identify bottlenecks via profiling

**Key metrics:**

- Total updates per cycle (50 services × 100 endpoints × 500 clients = 2.5M
  updates per scale event)
- Resource saturation indicators (CPU, memory, send buffer pressure)
- Breaking point thresholds

**Purpose:** This scenario is expected to push the controller to its limits.
Results inform:

- When to scale horizontally (add more destination controllers)
- Resource requests/limits for production deployments
- Tuning parameters (e.g., send timeouts, buffer sizes)

---

**Future Scenarios** (after baseline federated test):

- Scenario 3: Large Service Rollout (deep fanout)
- Scenario 4: Many Services (broad fanout)
- Scenario 5: Red-Line Composite (stress test)
- Chaos testing (pod kills, slow clients)
- Mixed workload patterns

## Implementation Roadmap

**Goal:** Get to federated multicluster baseline test as quickly as possible.
Expand scenarios after steel thread is working.

### Phase 1: Steel Thread to Federated Baseline (Week 1-2)

**Milestone:** Run Scenario 1 (Baseline) + Scenario 2 (Small Oscillation) in
federated multicluster mode.

#### Week 1: MVP Controllers + Single-Cluster Test

1. **Day 1-2: Project scaffolding**
   - Create `test/destination-test/` directory structure
   - Set up Cargo workspace with `dst-load-controller` binary
   - Basic CLI parsing (clap) with `churn` and `client` subcommands
   - Dockerfile for building the binary

2. **Day 3-4: Churn controller MVP**
   - Create Services and Deployments via `kube` crate
   - Two patterns only: `stable` and `oscillate`
   - KWOK integration (assumes KWOK already installed)
   - Basic Prometheus metrics (services created, scale events)

3. **Day 5: Client controller MVP**
   - gRPC client using `linkerd2-proxy-api`
   - Connect to destination controller
   - Establish N streams per service (configurable)
   - Basic metrics (streams active, updates received)
   - No fancy lifecycle management yet

4. **Weekend: Single-cluster validation**
   - Manual k3d cluster setup
   - Install KWOK, Linkerd
   - Deploy churn + client controllers manually (kubectl apply)
   - Run Scenario 1 (stable) for 10 minutes
   - Validate: streams work, metrics appear

#### Week 2: Federated Multicluster

5. **Day 1-2: Multicluster setup**
   - Create `hack/k3d-multicluster.sh` script (reference only)
   - Two k3d clusters with flat networking
   - Install Linkerd + multicluster in source cluster
   - Link source → target cluster
   - Document manual setup steps

6. **Day 3: Helm chart MVP**
   - Basic chart structure (`charts/dst-load-test/`)
   - Deploy churn controller as Pod
   - Deploy client controller as Deployment
   - Simple values.yaml (no fancy templating)
   - RBAC for namespace-scoped permissions

7. **Day 4: Federated test execution**
   - Deploy churn in target cluster (via Helm)
   - Deploy client in source cluster (via Helm)
   - Run Scenario 1 (stable) for 30 minutes
   - Run Scenario 2 (oscillate) for 3 cycles
   - Collect metrics, validate cross-cluster discovery

8. **Day 5: Documentation + wrap-up**
   - README with quick start
   - Document expected metrics
   - Known issues / limitations
   - Next steps

### Phase 2: Expand Scenarios (Week 3+)

**Only after Phase 1 is complete:**

1. **Scenario 3: Large Service Rollout**
   - Add support for single large service
   - Enhanced client metrics (latency histograms)

2. **Scenario 4: Many Services**
   - Scale to 100 services

3. **Scenario 5: Red-Line**
   - Combined stress test

4. **Monitoring improvements**
   - Grafana dashboards
   - Event correlation timeline
   - Automated alerts

5. **Performance tuning**
   - Adjust `LINKERD_DESTINATION_STREAM_SEND_TIMEOUT` if needed
   - Profile CPU/memory hotspots
   - Document performance characteristics

---

## Phase 1 MVP Deliverables

**Goal:** Steel thread to federated multicluster - prove the pattern works
end-to-end before expanding scope.

**Definition of Done:** Federated multicluster baseline test running with
Scenario 1 & 2.

**Success Criteria:**

1. ✅ Can create 10 services with KWOK-managed pods in target cluster
2. ✅ Can establish 100 client streams from source cluster
3. ✅ Client streams see endpoints from target cluster (cross-cluster discovery)
4. ✅ Oscillation pattern works: services scale 10→200→10 over multiple cycles
5. ✅ No crashes, no panics, no stream errors
6. ✅ Basic metrics visible (services created, streams active, updates received)
7. ✅ Can deploy/undeploy via Helm chart
8. ✅ README enables someone else to reproduce the test

**Non-goals for MVP:** Performance, scale, advanced metrics, automation.
Just prove the pattern works.

### Code Deliverables

1. **`test/destination-test/` directory**
   - Cargo.toml (workspace)
   - dst-load-controller/ (binary crate)
   - Dockerfile

2. **`dst-load-controller` binary**
   - `churn` subcommand with `stable` and `oscillate` patterns
   - `client` subcommand with basic stream management
   - CLI arguments (no YAML config needed for MVP)
   - Prometheus metrics endpoints

3. **Helm chart: `charts/dst-load-test/`**
   - Chart.yaml
   - values.yaml (baseline + oscillate scenarios)
   - templates/ (churn Pod, client Deployment, RBAC)

4. **Reference scripts: `test/destination-test/hack/`**
   - k3d-multicluster.sh (example setup)
   - install-kwok.sh
   - install-linkerd-multicluster.sh
   - README.md with manual setup instructions

### Test Evidence

1. **Single-cluster validation**
   - Screenshot/logs: 10 services created with KWOK Pods
   - Screenshot/logs: 100 client streams established
   - Prometheus metrics showing activity

2. **Federated multicluster validation**
   - Scenario 1 (stable): Ran 30 minutes, no errors
   - Scenario 2 (oscillate): Completed 3 cycles, metrics stable
   - Cross-cluster discovery verified (target endpoints visible in source)

### Documentation

1. **README.md**
   - Quick start (assumes k3d + KWOK + Linkerd installed)
   - How to deploy with Helm
   - How to read metrics
   - Known limitations

2. **This document (LOAD_TEST_PLAN.md)**
   - Updated with MVP status
   - Phase 2 work clearly deferred

### Out of Scope for MVP

**Explicitly NOT included in Phase 1:**

- ❌ Scenario 3, 4, 5 (only Baseline + Small Oscillation)
- ❌ Grafana dashboards (manual Prometheus queries are fine)
- ❌ Advanced metrics (latency histograms, event correlation)
- ❌ Stream lifecycle management (no reset intervals)
- ❌ Automated test harness (manual kubectl/helm is fine)
- ❌ CI/CD integration
- ❌ Performance comparison reports
- ❌ Chaos testing
- ❌ kube-prometheus-stack (optional, not required)

**MVP focus:** Get one thing working end-to-end in federated multicluster.
Everything else can wait.

---

## Detailed Implementation Specifications

### Technology Choice: Rust

Both the churn controller and client controller will be written in **Rust**,
following the established patterns in `policy-test`. This choice is motivated
by:

1. **Consistency**: The `policy-test` infrastructure is already in Rust and
   provides proven patterns for Kubernetes controllers and gRPC clients
2. **Reusable dependencies**: We can leverage existing workspace dependencies:
   - `kube` for Kubernetes API interactions
   - `linkerd2-proxy-api` for gRPC destination client
   - `prometheus-client` for metrics
   - `tokio` for async runtime
3. **Strong typing**: Better compile-time guarantees for complex concurrent
   stream management
4. **Performance**: Low overhead for high-concurrency scenarios
5. **Team familiarity**: The team has recent experience with Rust test tooling

While Go is also viable (used extensively in the Linkerd control plane), Rust
provides better alignment with existing test infrastructure and reduces
duplication of dependency management.

### Test Infrastructure Layout

The destination load test tooling follows the pattern established by
`policy-test` and is designed to be deployed into existing clusters via Helm:

```text
test/destination-test/
├── Cargo.toml                    # Rust workspace
├── README.md                     # Overview and quick start
├── dst-load-controller/          # Main binary crate
│   ├── Cargo.toml
│   ├── src/
│   │   ├── main.rs               # CLI and subcommand dispatch
│   │   ├── churn/
│   │   │   ├── mod.rs            # Churn controller entrypoint
│   │   │   ├── config.rs         # Scenario configuration
│   │   │   ├── patterns.rs       # Churn patterns (rolling, spike, etc.)
│   │   │   └── metrics.rs        # Prometheus instrumentation
│   │   └── client/
│   │       ├── mod.rs            # Client controller entrypoint
│   │       ├── config.rs         # Load test configuration
│   │       ├── grpc_client.rs    # Destination API client
│   │       ├── metrics.rs        # Latency tracking
│   │       └── stream_manager.rs # Concurrent stream lifecycle
│   └── Dockerfile                # Multi-stage build
├── common/                       # Shared library code
│   ├── Cargo.toml
│   └── src/
│       ├── lib.rs
│       ├── k8s.rs                # Kubernetes client helpers
│       └── telemetry.rs          # Logging/tracing setup
├── charts/
│   └── dst-load-test/            # Helm chart for deployment
│       ├── Chart.yaml
│       ├── values.yaml           # Default values
│       ├── templates/
│       │   ├── churn-pod.yaml    # Churn controller Pod
│       │   ├── client-deployment.yaml  # Client controller Deployment
│       │   ├── configmap.yaml    # Configuration
│       │   ├── rbac.yaml         # ServiceAccount, Role, RoleBinding
│       │   └── servicemonitor.yaml     # Prometheus scraping (optional)
│       └── README.md             # Chart documentation
├── scenarios/                    # Scenario values files
│   ├── baseline.yaml             # 100 services, 100 streams, low churn
│   ├── high-concurrency.yaml     # 100 services, 1000 streams, low churn
│   ├── deployment-storm.yaml     # 100 services, 500 streams, high churn
│   ├── scale-event.yaml          # 50 services, 10→100 pods, 500 streams
│   ├── federated.yaml            # Multi-cluster configuration
│   └── chaos.yaml                # With failure injection
├── grafana/                      # Monitoring dashboards
│   ├── dashboards/
│   │   ├── destination-overview.json
│   │   ├── client-metrics.json
│   │   └── cluster-resources.json
│   └── kube-prometheus-values.yaml  # For monitoring stack setup
└── tests/                        # Integration tests (future)
    ├── baseline_test.rs
    ├── concurrency_test.rs
    └── common/
        └── mod.rs                # Test utilities
```

**Key aspects:**

- **No cluster provisioning scripts**: Users bring their own clusters
  (k3d, kind, GKE, EKS, etc.)
- **Helm chart is the deployment unit**: All scenarios use the same chart with
  different values
- **Scenarios are values files**: Easy to version, compare, and compose
- **Controllers assume proper RBAC**: Chart creates necessary ServiceAccount
  and permissions

### Helm Chart Design

The Helm chart (`charts/dst-load-test/`) deploys both controllers with
configurable scenarios.

**Example values.yaml structure:**

```yaml
# Global settings
image:
  repository: ghcr.io/linkerd/dst-load-controller
  tag: edge
  pullPolicy: IfNotPresent

# Churn controller configuration
churn:
  enabled: true
  
  # CLI arguments passed to dst-load-controller churn
  args:
    - --services=10
    - --endpoints-per-service=100
    - --pattern=stable

  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi

# Client controller configuration
client:
  enabled: true
  replicas: 10  # Scale for desired concurrency
  
  # CLI arguments passed to dst-load-controller client
  args:
    - --destination=linkerd-destination.linkerd.svc.cluster.local:8086
    - --services=test-service-{1..10}.test-load.svc.cluster.local:8080
    - --streams-per-service=10  # Each replica: 10 services × 10 streams = 100
    - --duration=15m
  
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 1000m
      memory: 512Mi

# Monitoring (optional)
monitoring:
  serviceMonitor:
    enabled: true
    interval: 10s
```

**Scenario-specific values** (e.g., `scenarios/baseline.yaml`):

```yaml
churn:
  args:
    - --services=10
    - --endpoints-per-service=100
    - --pattern=stable

client:
  replicas: 10
  args:
    - --destination=linkerd-destination.linkerd.svc.cluster.local:8086
    - --services=test-service-{1..10}.test-load.svc.cluster.local:8080
    - --streams-per-service=10
```

**Scenario: Oscillating scale** (`scenarios/oscillate.yaml`):

```yaml
churn:
  args:
    - --services=10
    - --pattern=oscillate
    - --min-endpoints=10
    - --max-endpoints=200
    - --min-hold=2m
    - --max-hold=5m
    - --jitter=5s

client:
  replicas: 10
  args:
    - --destination=linkerd-destination.linkerd.svc.cluster.local:8086
    - --services=test-service-{1..10}.test-load.svc.cluster.local:8080
    - --streams-per-service=10
```

**Usage:**

```bash
# Install baseline scenario (runs until stopped)
helm install baseline ./charts/dst-load-test \
  -f scenarios/baseline.yaml \
  --namespace test-baseline \
  --create-namespace

# Install oscillating scenario
helm install oscillate ./charts/dst-load-test \
  -f scenarios/oscillate.yaml \
  --namespace test-oscillate \
  --create-namespace

# Monitor
kubectl logs -n test-baseline -l app=churn-controller -f
kubectl logs -n test-baseline -l app=client-controller -f

# Scale up client load
helm upgrade baseline ./charts/dst-load-test \
  --set client.replicas=50 \
  --reuse-values

# Cleanup
helm uninstall baseline -n test-baseline
kubectl delete namespace test-baseline
```

### Prerequisites (Out of Scope)

The `dst-load-test` Helm chart is designed to work with any properly configured
Kubernetes cluster. The following must be set up before deployment:

**Required:**

- Kubernetes cluster (1.28+)
- KWOK installed (`kwok` and `kwok-controller` running in cluster)
- Linkerd control plane installed (with destination controller)
- kubectl configured with appropriate context
- Helm 3.x installed

**For federated tests:**

- Linkerd multicluster extension installed in source cluster
- Link CRD configured pointing to target cluster
- Remote cluster credentials configured
- Network connectivity between clusters (pods can reach across clusters)

**For monitoring:**

- Prometheus operator (optional, for ServiceMonitor support)
- Grafana (optional, for dashboards)

### Reference Setup Scripts

While cluster setup is out of scope for the Helm chart, we provide **reference
scripts** for local development and CI testing. These are examples that users
can adapt to their environment.

**Location: `test/destination-test/hack/`**

This directory contains scripts that are **not required** by the Helm chart but
are useful for:

- Local development with k3d
- CI pipelines
- Quick start examples
- Testing different cluster configurations

**Example scripts:**

```text
test/destination-test/hack/
├── k3d-single-cluster.sh       # Create single k3d cluster with Linkerd
├── k3d-multicluster.sh         # Create two k3d clusters with flat network
├── install-kwok.sh             # Install KWOK in existing cluster
├── install-linkerd.sh          # Install Linkerd in existing cluster
├── install-monitoring.sh       # Install kube-prometheus-stack
└── README.md                   # Usage guide
```

**Example: `hack/k3d-single-cluster.sh`**

```bash
#!/usr/bin/env bash
# Reference script for creating a local k3d cluster for testing
# This is NOT required by the Helm chart - use your own cluster!

set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-dst-test}"
K8S_VERSION="${K8S_VERSION:-v1.28}"

echo "Creating k3d cluster: $CLUSTER_NAME"
k3d cluster create "$CLUSTER_NAME" \
    --image="rancher/k3s:$K8S_VERSION" \
    --agents=1 \
    --servers=1 \
    --no-lb \
    --k3s-arg '--disable=local-storage,traefik,servicelb,metrics-server@server:*' \
    --wait

echo "✓ Cluster created"
echo "  Context: k3d-$CLUSTER_NAME"
echo ""
echo "Next steps:"
echo "  1. Install KWOK: ./hack/install-kwok.sh"
echo "  2. Install Linkerd: ./hack/install-linkerd.sh"
echo "  3. Install monitoring: ./hack/install-monitoring.sh"
echo "  4. Run load test: helm install baseline ./charts/dst-load-test"
```

**Example: `hack/install-kwok.sh`**

```bash
#!/usr/bin/env bash
# Install KWOK for fake pod/node creation

set -euo pipefail

KWOK_VERSION="${KWOK_VERSION:-v0.5.0}"
CLUSTER_CONTEXT="${CLUSTER_CONTEXT:-$(kubectl config current-context)}"

echo "Installing KWOK ${KWOK_VERSION} in cluster ${CLUSTER_CONTEXT}"

# Install KWOK CRDs and controller
kubectl --context="${CLUSTER_CONTEXT}" apply -f \
  "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/kwok.yaml"

# Wait for KWOK controller to be ready
kubectl --context="${CLUSTER_CONTEXT}" wait --for=condition=ready \
  --namespace=kube-system \
  --selector=app=kwok-controller \
  --timeout=1m pod

echo "✓ KWOK installed"
echo "  KWOK will now automatically create fake Pods for any Deployments"
echo "  Fake pods appear as Running/Ready but consume no resources"
```

**Example: `hack/k3d-multicluster.sh`**

```bash
#!/usr/bin/env bash
# Reference script for creating two k3d clusters with flat networking
# Useful for testing federated destination service behavior

set -euo pipefail

NETWORK_NAME="dst-test-net"
SOURCE_CLUSTER="dst-source"
TARGET_CLUSTER="dst-target"

# Create Docker network
docker network create "$NETWORK_NAME" 2>/dev/null || true

# Create source cluster (runs Linkerd + client controller)
k3d cluster create "$SOURCE_CLUSTER" \
    --network="$NETWORK_NAME" \
    --agents=0 --servers=1 --no-lb \
    --k3s-arg '--disable=local-storage,traefik,servicelb,metrics-server@server:*' \
    --wait

# Create target cluster (runs churn controller + workloads)
k3d cluster create "$TARGET_CLUSTER" \
    --network="$NETWORK_NAME" \
    --agents=0 --servers=1 --no-lb \
    --k3s-arg '--disable=local-storage,traefik,servicelb,metrics-server@server:*' \
    --wait

# Configure flat network (pod CIDR routes)
SOURCE_SERVER="k3d-$SOURCE_CLUSTER-server-0"
TARGET_SERVER="k3d-$TARGET_CLUSTER-server-0"

SOURCE_ROUTE=$(kubectl --context="k3d-$SOURCE_CLUSTER" get node "$SOURCE_SERVER" \
    -o jsonpath='ip route add {.spec.podCIDR} via {.status.addresses[?(@.type=="InternalIP")].address}')
TARGET_ROUTE=$(kubectl --context="k3d-$TARGET_CLUSTER" get node "$TARGET_SERVER" \
    -o jsonpath='ip route add {.spec.podCIDR} via {.status.addresses[?(@.type=="InternalIP")].address}')

docker exec "$SOURCE_SERVER" $TARGET_ROUTE
docker exec "$TARGET_SERVER" $SOURCE_ROUTE

echo "✓ Multicluster setup complete"
echo "  Source: k3d-$SOURCE_CLUSTER (install Linkerd + multicluster here)"
echo "  Target: k3d-$TARGET_CLUSTER (workloads only)"
echo ""
echo "Next steps:"
echo "  1. Install Linkerd in source: kubectl --context=k3d-$SOURCE_CLUSTER ..."
echo "  2. Install multicluster: linkerd --context=k3d-$SOURCE_CLUSTER multicluster install"
echo "  3. Link clusters: linkerd --context=k3d-$SOURCE_CLUSTER multicluster link"
```

**Key principle:** These scripts are **reference implementations** for common
scenarios. Users running in GKE, EKS, AKS, or other environments will use their
own cluster provisioning tools.

### Controller Implementation Details

The following sections describe the internal implementation of the
`dst-load-controller` binary. These are code sketches that will be factored
into actual source files in the repository.

#### Churn Controller Implementation

The churn controller simulates realistic endpoint churn patterns. It uses the
`kube` crate to manipulate Kubernetes resources.

**Dependencies** (`dst-load-controller/Cargo.toml`):

```toml
[package]
name = "destination-churn-controller"
version = "0.1.0"
edition = "2021"
license = "Apache-2.0"
publish = false

[dependencies]
anyhow = "1"
clap = { version = "4", features = ["derive", "env"] }
k8s-openapi = { workspace = true, features = ["v1_28"] }
kube = { workspace = true, features = ["client", "runtime", "derive"] }
prometheus-client = "0.22"
rand = "0.9"
serde = { version = "1", features = ["derive"] }
serde_yaml = "0.9"
tokio = { version = "1", features = ["full"] }
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter", "json"] }

[dependencies.linkerd-destination-test-common]
path = "../common"
```

**Core structure** (`churn-controller/src/main.rs`):

```rust
use anyhow::Result;
use clap::Parser;
use k8s_openapi::api::core::v1::{Endpoints, Pod, Service};
use kube::{Api, Client};
use std::time::Duration;
use tracing::info;

mod config;
mod metrics;
mod patterns;

use config::{ChurnPattern, Config, Scenario};
use metrics::Metrics;
use patterns::ChurnOrchestrator;

#[derive(Parser)]
#[command(name = "churn-controller")]
#[command(about = "Simulates endpoint churn for destination service load testing")]
struct Args {
    /// Path to scenario configuration file
    #[arg(short, long, default_value = "scenarios/baseline.yaml")]
    config: String,

    /// Namespace for test resources
    #[arg(short, long, env = "NAMESPACE", default_value = "destination-test")]
    namespace: String,

    /// Metrics port
    #[arg(long, env = "METRICS_PORT", default_value = "9090")]
    metrics_port: u16,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .json()
        .init();

    let args = Args::parse();
    let config = Config::load(&args.config)?;
    let client = Client::try_default().await?;
    let metrics = Metrics::new();

    info!("Starting churn controller with {} scenarios", config.scenarios.len());

    // Start metrics server
    tokio::spawn(metrics.serve(args.metrics_port));

    // Run scenarios sequentially
    let orchestrator = ChurnOrchestrator::new(
        client.clone(),
        args.namespace.clone(),
        metrics.clone(),
    );

    for scenario in config.scenarios {
        info!("Running scenario: {}", scenario.name);
        orchestrator.run_scenario(&scenario).await?;
    }

    Ok(())
}
```

**Churn patterns** (`churn-controller/src/patterns.rs`):

```rust
use k8s_openapi::api::core::v1::{Endpoints, Pod};
use kube::{Api, Client};
use rand::seq::SliceRandom;
use std::time::Duration;
use tokio::time;

pub struct ChurnOrchestrator {
    client: Client,
    namespace: String,
    metrics: Metrics,
}

impl ChurnOrchestrator {
    pub async fn run_scenario(&self, scenario: &Scenario) -> Result<()> {
        match scenario.pattern {
            ChurnPattern::RollingUpdate { batch_size, interval } => {
                self.rolling_update(scenario, batch_size, interval).await
            }
            ChurnPattern::ScaleEvent { from, to, duration } => {
                self.scale_event(scenario, from, to, duration).await
            }
            ChurnPattern::SteadyState { churn_rate } => {
                self.steady_state(scenario, churn_rate).await
            }
            ChurnPattern::Spike { peak_rate, duration } => {
                self.spike(scenario, peak_rate, duration).await
            }
        }
    }

    async fn rolling_update(
        &self,
        scenario: &Scenario,
        batch_size: usize,
        interval: Duration,
    ) -> Result<()> {
        let pods_api: Api<Pod> = Api::namespaced(self.client.clone(), &self.namespace);
        
        // List all test pods
        let mut pods = pods_api
            .list(&Default::default())
            .await?
            .items;
        
        // Delete and recreate in batches
        for chunk in pods.chunks(batch_size) {
            for pod in chunk {
                let name = pod.name_any();
                
                // Record timestamp for latency measurement
                self.metrics.record_churn_event(&name);
                
                // Delete pod
                pods_api.delete(&name, &Default::default()).await?;
                
                // Recreate pod (K8s controller will handle this via ReplicaSet)
                // We just track the event
            }
            
            time::sleep(interval).await;
        }
        
        Ok(())
    }

    async fn scale_event(
        &self,
        scenario: &Scenario,
        from: u32,
        to: u32,
        duration: Duration,
    ) -> Result<()> {
        // Implementation: gradually scale endpoints from `from` to `to`
        // over the specified duration
        todo!()
    }

    async fn steady_state(
        &self,
        scenario: &Scenario,
        churn_rate: u32,
    ) -> Result<()> {
        // Implementation: random pod updates at specified rate
        todo!()
    }

    async fn spike(
        &self,
        scenario: &Scenario,
        peak_rate: u32,
        duration: Duration,
    ) -> Result<()> {
        // Implementation: burst of changes, then return to baseline
        todo!()
    }
}
```

### Client Controller (Rust)

The client controller establishes concurrent gRPC streams to the destination
service and measures latencies. It uses the same patterns as the policy-test
gRPC client.

**Dependencies** (`client-controller/Cargo.toml`):

```toml
[package]
name = "destination-client-controller"
version = "0.1.0"
edition = "2021"
license = "Apache-2.0"
publish = false

[dependencies]
anyhow = "1"
clap = { version = "4", features = ["derive", "env"] }
futures = "0.3"
http = "0.2"
prometheus-client = "0.22"
serde = { version = "1", features = ["derive"] }
serde_yaml = "0.9"
tokio = { version = "1", features = ["full"] }
tonic = { workspace = true }
tower = { workspace = true }
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter", "json"] }

[dependencies.linkerd2-proxy-api]
workspace = true
features = ["destination"]

[dependencies.linkerd-destination-test-common]
path = "../common"
```

**Core structure** (`client-controller/src/main.rs`):

```rust
use anyhow::Result;
use clap::Parser;
use linkerd2_proxy_api::destination::{
    destination_client::DestinationClient, GetDestination,
};
use std::time::{Duration, Instant};
use tonic::transport::Channel;
use tracing::{info, warn};

mod config;
mod metrics;
mod stream_manager;

use config::Config;
use metrics::Metrics;
use stream_manager::StreamManager;

#[derive(Parser)]
#[command(name = "client-controller")]
#[command(about = "gRPC client load generator for destination service")]
struct Args {
    /// Destination controller address
    #[arg(long, env = "DESTINATION_ADDR", default_value = "linkerd-destination.linkerd.svc.cluster.local:8086")]
    destination_addr: String,

    /// Path to load test configuration
    #[arg(short, long, default_value = "scenarios/baseline.yaml")]
    config: String,

    /// Metrics port
    #[arg(long, env = "METRICS_PORT", default_value = "9091")]
    metrics_port: u16,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .json()
        .init();

    let args = Args::parse();
    let config = Config::load(&args.config)?;
    let metrics = Metrics::new();

    // Start metrics server
    tokio::spawn(metrics.serve(args.metrics_port));

    // Connect to destination service
    let channel = Channel::from_shared(format!("http://{}", args.destination_addr))?
        .connect()
        .await?;
    let client = DestinationClient::new(channel);

    info!("Connected to destination service at {}", args.destination_addr);

    // Create stream manager
    let stream_manager = StreamManager::new(client, metrics.clone());

    // Run load test scenarios
    for scenario in config.scenarios {
        info!("Running scenario: {} ({} concurrent streams)", 
            scenario.name, scenario.concurrency);
        
        stream_manager.run_scenario(&scenario).await?;
    }

    Ok(())
}
```

**Stream manager** (`client-controller/src/stream_manager.rs`):

```rust
use futures::StreamExt;
use linkerd2_proxy_api::destination::{
    destination_client::DestinationClient, GetDestination, Update,
};
use std::time::{Duration, Instant};
use tokio::time;
use tonic::transport::Channel;
use tracing::{debug, error, info};

pub struct StreamManager {
    client: DestinationClient<Channel>,
    metrics: Metrics,
}

impl StreamManager {
    pub async fn run_scenario(&self, scenario: &Scenario) -> Result<()> {
        let mut handles = vec![];

        // Ramp up to target concurrency
        let ramp_step = scenario.concurrency / 10.max(1);
        let ramp_interval = scenario.ramp_up / 10;

        for i in 0..scenario.concurrency {
            if i % ramp_step == 0 {
                time::sleep(ramp_interval).await;
            }

            let service = scenario.pick_service();
            let handle = self.spawn_stream(service, scenario.stream_duration);
            handles.push(handle);
        }

        // Wait for test duration
        time::sleep(scenario.duration).await;

        // Gracefully shut down streams
        for handle in handles {
            handle.abort();
        }

        Ok(())
    }

    fn spawn_stream(
        &self,
        service: String,
        duration: Duration,
    ) -> tokio::task::JoinHandle<()> {
        let mut client = self.client.clone();
        let metrics = self.metrics.clone();

        tokio::spawn(async move {
            loop {
                let start = Instant::now();
                
                let req = GetDestination {
                    path: service.clone(),
                    ..Default::default()
                };

                metrics.record_stream_created(&service);

                match client.get(req).await {
                    Ok(response) => {
                        let mut stream = response.into_inner();
                        let stream_start = Instant::now();

                        while let Some(result) = stream.next().await {
                            match result {
                                Ok(update) => {
                                    let latency = start.elapsed();
                                    metrics.record_update_received(&service, latency);
                                    debug!("Received update for {} in {:?}", service, latency);
                                }
                                Err(e) => {
                                    error!("Stream error for {}: {}", service, e);
                                    metrics.record_stream_error(&service);
                                    break;
                                }
                            }

                            // Check if stream should be reset
                            if stream_start.elapsed() > duration {
                                metrics.record_stream_reset(&service);
                                break;
                            }
                        }

                        let stream_lifetime = stream_start.elapsed();
                        metrics.record_stream_lifetime(&service, stream_lifetime);
                    }
                    Err(e) => {
                        error!("Failed to create stream for {}: {}", service, e);
                        metrics.record_connection_error(&service);
                        time::sleep(Duration::from_secs(1)).await;
                    }
                }

                // Small delay before reconnecting
                time::sleep(Duration::from_millis(100)).await;
            }
        })
    }
}
```

### Integration Testing

Similar to `policy-test`, we'll have integration tests that orchestrate the
entire load testing flow:

**Test structure** (`tests/baseline_test.rs`):

```rust
use destination_test_common::{create_namespace, with_client};
use k8s_openapi::api::core::v1::{Namespace, Service};
use std::time::Duration;
use tokio::time;

#[tokio::test]
async fn test_baseline_single_cluster() {
    let client = with_client().await;
    let ns = create_namespace(&client, "baseline-test").await;

    // Create test services
    for i in 0..100 {
        let svc = Service {
            metadata: ObjectMeta {
                name: Some(format!("test-service-{}", i)),
                namespace: Some(ns.clone()),
                ..Default::default()
            },
            spec: Some(ServiceSpec {
                ports: Some(vec![ServicePort {
                    port: 8080,
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..Default::default()
        };
        create(&client, svc).await;
    }

    // Deploy churn controller
    // Deploy client controller
    // Run test for 10 minutes
    // Collect metrics
    // Assert success criteria

    time::sleep(Duration::from_secs(600)).await;

    // Cleanup
    delete_namespace(&client, &ns).await;
}
```

### justfile Integration

Add targets to the main justfile for easy test execution:

```just
##
## Destination load tests
##

export DST_TEST_SOURCE_CONTEXT := "k3d-dst-source"
export DST_TEST_TARGET_CONTEXT := "k3d-dst-target"

# Setup destination test clusters
dst-test-setup: dst-test-cluster-create dst-test-monitoring-install dst-test-linkerd-install

# Create k3d clusters for destination load testing
dst-test-cluster-create:
    test/destination-test/bin/setup-clusters.sh

# Install kube-prometheus-stack for monitoring
dst-test-monitoring-install:
    test/destination-test/bin/setup-monitoring.sh

# Install Linkerd in test clusters
dst-test-linkerd-install:
    test/destination-test/bin/setup-linkerd.sh

# Build destination test controllers
dst-test-build:
    cd test/destination-test && {{ _cargo }} build --release

# Run destination load tests
dst-test-run *flags:
    cd test/destination-test && {{ _cargo-test }} {{ flags }}

# Run specific scenario
dst-test-scenario scenario:
    cd test/destination-test && {{ _cargo }} run --bin dst-load-controller -- \
        client --config scenarios/{{ scenario }}.yaml

# Full destination test suite (setup + run + cleanup)
dst-test: dst-test-setup dst-test-build dst-test-run && dst-test-cleanup

# Cleanup destination test resources
dst-test-cleanup:
    kubectl --context={{ DST_TEST_SOURCE_CONTEXT }} delete ns destination-test || true
    kubectl --context={{ DST_TEST_TARGET_CONTEXT }} delete ns destination-test || true

# Delete destination test clusters
dst-test-cluster-delete:
    k3d cluster delete dst-source dst-target || true
    docker network rm dst-test-net || true
```

## Deliverables

1. **`dst-load-controller` Binary** (Rust)
   - `test/destination-test/dst-load-controller/` - Single binary with
     subcommands
   - `dst-load-controller churn` - Simulates K8s endpoint churn patterns
   - `dst-load-controller client` - Establishes concurrent gRPC streams
   - Configurable via YAML scenario files
   - README with usage and configuration options

2. **Setup Scripts**
   - `test/destination-test/bin/` - Setup and teardown scripts
   - Cluster provisioning (k3d with flat networking)
   - kube-prometheus-stack installation
   - Linkerd installation (with customizable configuration)

3. **Monitoring Stack**
   - `test/destination-test/grafana/` - Custom dashboards
   - Alert rules for load testing
   - Grafana dashboard JSON exports
   - Pre-configured scrape configs for destination controller

4. **Test Scenarios**
   - `test/destination-test/scenarios/*.yaml` - Scenario configurations
   - Integration test runner (similar to policy-test)
   - Expected results / success criteria

5. **Documentation**
   - Setup guide (cluster requirements, installation)
   - Running tests guide (via justfile targets)
   - Interpreting results guide
   - Troubleshooting guide

6. **Results Report**
   - Performance comparison (old vs new architecture)
   - Resource usage analysis
   - Scalability limits identified
   - Production deployment recommendations
   - Tuning guidelines

## Infrastructure Requirements

**Test Cluster**:

- Kubernetes 1.28+
- 4 nodes (8 vCPU, 32GB RAM each)
- Fast disk I/O for etcd
- Prometheus operator installed
- Network: Low latency between nodes

**For Federated Tests**:

- 4 clusters total (1 source + 3 target)
- Same specs per cluster
- Low latency network between clusters (<10ms)

**Development Environment**:

- Go 1.21+
- Docker
- kubectl
- Helm 3.x

## Success Metrics Summary

| Metric | Baseline | High Concurrency | Deployment Storm | Scale Event |
|--------|----------|------------------|------------------|-------------|
| p95 latency | <100ms | <500ms | <1s | <2s (during event) |
| Send timeout rate | 0% | <0.1% | <1% | <0.5% |
| Memory (MB) | <500 | <2000 | <1500 | <1500 |
| CPU (cores) | <0.5 | <2 | <1.5 | <2 |
| Active views | 100 | 1000 | 500 | 500 |

## Open Questions

1. **Timeout tuning**: Should we test different
   `LINKERD_DESTINATION_STREAM_SEND_TIMEOUT` values (1s, 5s, 10s)?
2. **Notification channel size**: Currently size-1, should we test size-2 or
   size-5?
3. **Snapshot version handling**: How do we verify duplicate notification
   coalescing is working?
4. **Memory profiling**: Should we include continuous pprof collection during
   load tests?
5. **Comparison baseline**: Do we need to run equivalent tests on the
   pre-refactor code for comparison?

## Next Steps

- [ ] Review and approve this plan
- [ ] Set up test infrastructure (clusters, KWOK, Prometheus)
- [ ] Begin Phase 1 implementation
- [ ] Schedule weekly sync to review progress
