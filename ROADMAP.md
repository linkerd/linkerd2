# Linkerd Roadmap

This document is intended to describe high-level plans for the Linkerd
project and is neither comprehensive nor prescriptive. For a more granular
view of planned work, please refer to the project's upcoming [milestones].

## Server-side policy

Linkerd's `ServiceProfile` abstraction is primarily focused on client-side
policy/configuration and is inappropriate to address authorization-oriented
policies. We plan on identifying/introducing primitives to support
server-oriented policy/configuration to support authorization as well as
other types of server-centric policy, including the types of configuration
that have been provided by `ServiceProfile`s, including timeouts, routes,
etc.

These policies will supplant existing workload-level annotations where
possible.

## Client-side policy, v2

As we identify new primitives/patterns for server-side configuration, we plan
to replace the current `ServiceProfile` APIs with new client-side primitives
that support the existing resources as well as:

- Circuit-breakers
- Non-cluster-local traffic targets
- TLS requirements

## Retries for gRPC services

Linkerd does not currently support for retries for requests with payloads,
but this limits the utility of this feature for gRPC services (that
necessarily include request payloads). We plan to support retries for unary
(non-streaming) gRPC requests.

## Mesh expansion

Linkerd's mTLS identity only works for resources managed by Kubernetes. We
plan to extend Linkerd to support non-Kubernetes workloads.

## OpenMetrics

The OpenMetrics working group is developing a new standard for metric
exposition. We plan to support this format and follow best practices
identified by this project.

<!-- references -->
[milestones]: https://github.com/linkerd/linkerd2/milestones
