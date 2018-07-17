+++
title = "Linkerd overview"
docpage = true
[menu.docs]
  parent = "docs"
+++

Linkerd is an ultralight service mesh for Kubernetes. It
makes running services on Kubernetes safer and more reliable by transparently
managing the runtime communication between services. It provides features for
observability, reliability, and security---all without requiring changes to your
code.

In this doc, you’ll get a high-level overview of Linkerd and how it works. If
you’re not familiar with the service mesh model, you may want to first read
William Morgan’s overview, [What’s a service mesh? And why do I need one?](https://buoyant.io/2017/04/25/whats-a-service-mesh-and-why-do-i-need-one/)

## Linkerd’s architecture

The Linkerd service mesh is deployed on a Kubernetes
cluster as two basic components: a *data plane* and a *control plane*. The data
plane carries the actual application request traffic between service instances.
The control plane drives the data plane and provides APIs for modifying its
behavior (as well as for accessing aggregated metrics). The Linkerd CLI and web
UI consume this API and provide ergonomic controls for human beings.

Let’s take each of these components in turn.

The Linkerd data plane is comprised of lightweight proxies, which are deployed
as sidecar containers alongside each instance of your service code. In order to
“add” a service to the Linkerd service mesh, the pods for that service must be
redeployed to include a data plane proxy in each pod. (The `linkerd inject`
command accomplishes this, as well as the configuration work necessary to
transparently funnel traffic from each instance through the proxy.)

These proxies transparently intercept communication to and from each pod, and
add features such as retries and timeouts, instrumentation, and encryption
(TLS), as well as allowing and denying requests according to the relevant
policy.

These proxies are not designed to be configured by hand. Rather, their behavior
is driven by the control plane.

The Linkerd control plane is a set of services that run in a dedicated
Kubernetes namespace (`linkerd` by default). These services accomplish various
things---aggregating telemetry data, providing a user-facing API, providing
control data to the data plane proxies, etc. Together, they drive the behavior
of the data plane.

## Using Linkerd

In order to interact with Linkerd as a human,
you use the Linkerd CLI and the web UI (as well as with associated tools like
`kubectl`). The CLI and the web UI drive the control plane via its API, and the
control plane in turn drives the behavior of the data plane.

The control plane API is designed to be generic enough that other tooling can be
built on top of it. For example, you may wish to additionally drive the API from
a CI/CD system.

A brief overview of the CLI’s functionality can be seen by running `linkerd
--help`.

## Linkerd with Kubernetes

Linkerd is designed to fit seamlessly into an
existing Kubernetes system. This design has several important features.

First, the Linkerd CLI (`linkerd`) is designed to be used in conjunction with
`kubectl` whenever possible. For example, `linkerd install` and `linkerd inject` are generated Kubernetes configurations designed to be fed directly into `kubectl`. This is to provide a clear division of labor between the service mesh
and the orchestrator, and to make it easier to fit Linkerd into existing
Kubernetes workflows.

Second, Linkerd’s core noun in Kubernetes is the Deployment, not the Service.
For example, `linkerd inject` adds a Deployment; the Linkerd web UI displays
Deployments; aggregated performance metrics are given per Deployment. This is
because individual pods can be a part of arbitrary numbers of Services, which
can lead to complex mappings between traffic flow and pods. Deployments, by
contrast, require that a single pod be a part of at most one Deployment. By
building on Deployments rather than Services, the mapping between traffic and
pod is always clear.

These two design features compose nicely. For example, `linkerd inject` can be
used on a live Deployment, as Kubernetes rolls pods to include the data plane
proxy as it updates the Deployment.

## Extending Linkerd’s Behavior

The Linkerd control plane also provides a convenient place for custom
functionality to be built. While the initial release of Linkerd does not support
this yet, in the near future, you’ll be able to extend Linkerd’s functionality
by writing gRPC plugins that run as part of the control plane, without needing
to recompile Linkerd.
