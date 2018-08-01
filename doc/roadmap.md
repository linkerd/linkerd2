+++
title = "Linkerd2 roadmap"
docpage = true
[menu.docs]
  parent = "roadmap"
+++

# [Friends and Family][fnf] - 8/6

Stabilize the project, paying off previous tech debt and completing the
migration.

## Goals

- Complete the move of conduit over to linkerd.

- Promote installation of linkerd2 over linkerd1.

- Production readiness for specific use cases (observability, debugging).

## Features

- Scale over 100 pods

- Tap stability and polish

- Performance baseline

# [Gibraltar][gibraltar] - 9/4

Surface existing backend functionality by providing UI tools that help with
specific debugging tasks.

## Goals

- Provide an experience that is awesome for a single service owner operating in
  a locked down environment (admin for a single namespace).

- Build a comprehensive suite of tools that assist in debugging common issues
  with services (status, latency, throughput).

- Illustrate clearly what problems can be debugged and why Linkerd helps there.

## Features

- `top` for real time feedback on what's happening with a service.

- Topology graph to visualize the relationships between services.

- `tap` metadata and filters to assist on narrowing down possible issues.

<!-- refs -->

[fnf]: https://github.com/linkerd/linkerd2/milestone/11
[gibraltar]: https://github.com/linkerd/linkerd2/milestone/8
