# Linkerd Roadmap

This document is intended to describe high-level plans for the Linkerd
project and is neither comprehensive nor prescriptive. For a more granular
view of planned work, please refer to the project's upcoming [milestones].

## Separate helm chart for Linkerd CRDs

Following best practices, Custom Resource Definitions are being migrated to
their own helm chart

## Client-side policy, v2

As we identify new primitives/patterns for server-side configuration, we plan
to replace the current `ServiceProfile` APIs with new client-side primitives
that support the existing resources as well as:

- Circuit-breakers
- Non-cluster-local traffic targets
- TLS requirements
- Route-Specific Authorization
- Outbound Route Configuration
- Header-based Routing

## Mesh expansion

Linkerd's mTLS identity only works for resources managed by Kubernetes. We
plan to extend Linkerd to support non-Kubernetes workloads.

## OpenMetrics

The OpenMetrics working group is developing a new standard for metric
exposition. We plan to support this format and follow best practices
identified by this project.

## Linkerd Gateway

The foundation for this feature exists in the multi-cluster gateway which
provides connectivity between clusters using the multi-cluster extension. As the
need for tighter ingress integration develops, the gateway functionality will
develop into a multi-purpose entry point for traffic entering the cluster,
reducing the dependency on features and development cycles for current ingress
providers.

<!-- references -->
[milestones]: https://github.com/linkerd/linkerd2/milestones
