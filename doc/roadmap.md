This is the planned roadmap for Conduit. Of course, as with any software project
(especially open source) even the best of plans change rapidly as development progresses.

Our goal is to get Conduit to production-readiness as rapidly as possible with a minimal
featureset, then to build functionality out from there. We’ll make alpha / beta / GA
designations based on actual community usage, and generally will err on the side of being
overly conservative.


##### Status: beta
## [0.4.0: Rich, Kubernetes-aware Grafana & Prometheus](https://github.com/runconduit/conduit/milestone/6)
#### 2018-04-16

### Visibility

- Rich, Kubernets-aware `conduit stat`:
  - Works on deployments & namespaces.
  - `--from` & `--to` flags filter stats by source & destination.
- Proxy exposes Prometheus labeled with rich outbound stats.
- Grafana dashboards for Kubernetes Deployments & Namespaces.

### Reliability

- The proxy properly routes egress traffic to arbitrary DNS names.


## [0.4.1: Rich, Kubernetes-aware debugging](https://github.com/runconduit/conduit/milestones)
#### Late April 2018
##### Status: beta

### Visibility

- `conduit stat` works on many Kubernetes resources.
  - Per-authority HTTP stats.
  - TCP-level stats
- `conduit tap` works on many Kubernetes resources, too.
- `conduit wtf`: what's the failure?


## [0.5: Private, Stable communication](https://github.com/runconduit/conduit/milestone/7)
#### Mid-May 2018

### Security

- Self-bootstrapping Certificate Authority
- Secured communication to and within the Conduit control plane
- Automatically provide all meshed services with cryptographic identity
- Automatically secure all meshed communication

### Reliability

- Stable Service Discovery semantics.
- Latency-aware load balancing.


## [0.6: Externaly accessible](https://github.com/runconduit/conduit/milestone/8)
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

