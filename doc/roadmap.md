+++
title = "Conduit roadmap"
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
## [0.4.1: Rich, Kubernetes-aware debugging](https://github.com/runconduit/conduit/milestone/10)
#### 2018-04-26

### Visibility

- `conduit stat` works on many Kubernetes resources.
  - Per-authority HTTP stats.
  - TCP-level stats
- `conduit tap` works on many Kubernetes resources, too.
- Grafana dashboards for Kubernetes Pods, Services, & Replication Controllers.

## [0.5: Stable, private communication](https://github.com/runconduit/conduit/milestone/7)
#### Mid-May 2018

### Security

- Self-bootstrapping Certificate Authority
- Secured communication to and within the Conduit control plane
- Automatically provide all meshed services with cryptographic identity
- Automatically secure all meshed communication

### Reliability

- Stable Service Discovery semantics.
- Latency-aware load balancing.

### Visibility

- `conduit wtf`: what's the failure?


## [0.6: Externally accessible](https://github.com/runconduit/conduit/milestone/8)
#### Early June 2018

### Routing

- Kubernetes `Ingress` support

### Security

- Explicitly configured TLS for ingress
- Server Name Indication (SNI)

### Reliability

- Scales to many cores.
- High-availability controller
- Circuit-breaking.

### Usability

- Helm integration


## And then...

- Mutual authentication
- Key rotation
- Let's Encrypt Ingress support
- Automatic alerting for latency & success objectives
- Controllable retry policies
- OpenTracing integration
- Pluggable authorization policy
- Failure injection
- Speculative retries
- Dark traffic
- gRPC payload-aware `tap`
- Automated red-line testing

