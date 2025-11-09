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

## Prerequisites

The load test controllers are deployed via Helm into existing, configured
Kubernetes clusters. The following must be set up before deployment:

**Required:**

- Kubernetes cluster (1.28+)
- KWOK installed (`kwok` and `kwok-controller` running in cluster)
- Linkerd control plane installed (with destination controller)
- kubectl configured with appropriate context
- Helm 3.x installed
- Rust toolchain (for building the binary)

**For federated tests:**

- Two clusters: source (runs Linkerd + clients) and target (runs workloads)
- Linkerd multicluster extension installed in source cluster
- Link CRD configured pointing to target cluster
- Network connectivity between clusters (flat networking or pod CIDR routes)

**For monitoring (optional):**

- Prometheus (for scraping metrics)
- Grafana (for visualization)

---

## Deliverables

## Deliverables (Phase 1 MVP)

1. **`dst-load-controller` Binary** (Rust)
   - Single binary with `churn` and `client` subcommands
   - Two patterns: `stable` and `oscillate`
   - CLI-based configuration (no YAML needed for MVP)
   - Basic Prometheus metrics
   - Dockerfile for containerization

2. **Helm Chart** (`charts/dst-load-test/`)
   - Deploys churn controller (Pod) and client controller (Deployment)
   - Basic values.yaml with scenario examples
   - RBAC templates (ServiceAccount, Role, RoleBinding)
   - README with deployment instructions

3. **Reference Scripts** (`test/destination-test/hack/`)
   - k3d-multicluster.sh (example cluster setup)
   - install-kwok.sh
   - install-linkerd-multicluster.sh
   - README with manual setup instructions

4. **Documentation**
   - README.md with quick start guide
   - This LOAD_TEST_PLAN.md (updated)
   - Expected metrics to monitor

---

## Next Steps (Post-MVP / Phase 2)

**After Phase 1 MVP is complete:**

- Scenario 3-5 implementation (Large Service, Many Services, Red-Line)
- Grafana dashboards
- Advanced metrics (latency histograms, event correlation)
- Stream lifecycle management features
- Automated test harness
- Performance tuning and profiling
- Production deployment guide

---

## Infrastructure Requirements (For Reference)

**Test Cluster**:

- Kubernetes 1.28+
- 4 nodes (8 vCPU, 32GB RAM each)
- Fast disk I/O for etcd

**MVP Test Environment:**

- 2 k3d clusters (source + target)
- 2 vCPU, 4GB RAM each (local development)
- KWOK for fake pod/node creation
- Linkerd + multicluster extension

**Future/Production Testing:**

- Larger clusters for Scenarios 3-5
- More resources for stress testing
- Dedicated monitoring infrastructure

---

## Success Metrics (Phase 1 MVP)

**Scenario 1 (Baseline):**

- Memory stable over 24h (no leaks)
- All streams healthy
- CPU baseline measured

**Scenario 2 (Small Oscillation):**

- Oscillation completes 3+ cycles successfully
- p95 update latency < 500ms
- No crashes or stream errors
- Memory returns to baseline after each cycle

---
