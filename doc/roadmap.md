+++
title = "Conduit public roadmap"
docpage = true
[menu.docs]
  parent = "roadmap"
+++

This is the planned roadmap for Conduit. Of course, as with any software project
(especially open source) even the best of plans change rapidly as development progresses.

Our goal is to get Conduit to production-readiness as rapidly as possible with a minimal
featureset, then to build functionality out from there. We’ll make alpha / beta / GA
designations based on actual community usage, and generally will err on the side of being
overly conservative.

##### Status: alpha
## [0.3: Telemetry Stability](https://github.com/runconduit/conduit/milestone/5)

#### Late February 2018

### Visibility

- Stable, automatic top-line metrics for small-scale clusters.

### Usability

- Routing to external DNS names

### Reliability

- Least-loaded L7 load balancing
- Improved error handling
- Improved egress support

### Development

- Published (this) public roadmap
- All milestones, issues, PRs, & mailing lists made public

## [0.4: Automatic TLS; Prometheus++](https://github.com/runconduit/conduit/milestone/6)

#### Late March 2018

### Usability

- Helm integration
- Mutating webhook admission controller

### Security

- Self-bootstrapping Certificate Authority
- Secured communication to and within the Conduit control plane
- Automatically provide all meshed services with cryptographic identity
- Automatically secure all meshed communication

### Visibility

- Enhanced server-side metrics, including per-path and per-status-code counts & latencies.
- Client-side metrics to surface egress traffic, etc.

### Reliability

- Latency-aware load balancing

## [0.5: Controllable Deadlines/Timeouts](https://github.com/runconduit/conduit/milestone/7)

#### Early April 2018

### Reliability

- Controllable latency objectives to configure timeouts
- Controllable response classes to inform circuit breaking, retryability, & success rate calculation
- High-availability controller

### Visibility

- OpenTracing integration

### Security

- Mutual authentication
- Key rotation

## [0.6: Controllable Response Classification & Retries](https://github.com/runconduit/conduit/milestone/8)

#### Late April 2018

### Reliability

- Automatic alerting for latency & success objectives
- Controllable retry policies

### Routing

- Rich ingress routing
- Contextual route overrides

### Security

- Authorization policy

## And Beyond:

- Controller policy plugins
- Support for non-Kubernetes services
- Failure injection (aka "chaos chihuahua")
- Speculative retries
- Dark traffic
- gRPC payload-aware `tap`
- Automated red-line testing

