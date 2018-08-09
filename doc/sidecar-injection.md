+++
title = "Experimental: Automatic sidecar injection"
docpage = true
[menu.docs]
    parent = "sidecar-injection"
+++

Linkerd can be configured to automatically inject the sidecar proxy in all pod
that are contained in a specific namespace with the correct label.

This feature is currently **experimental**. As the feature matures,
more automation to this task is introduced.

## Prerequisites

This feature need Kubernetes 1.9.0 or above with the
`admissionregistration.k8s.io/v1beta1` API enabled.
Verify that by the following command:
```
kubectl api-versions | grep admissionregistration.k8s.io/v1beta1
```
The result should be:
```
admissionregistration.k8s.io/v1beta1
```

In addition, the `MutatingAdmissionWebhook` and `ValidatingAdmissionWebhook`
admission controllers should be added and listed in the correct order in the
admission-control flag of kube-apiserver.

## Creating the secret

In order to work the webhook must use TLS, with a certificate signed from the
cluster, for creating the secret run `webhook-create-signed-cert.sh` script with
 the following parameters:

```bash
./bin/webhook-create-signed-cert.sh \
    --service linkerd-sidecar-injector \
    --secret linkerd-webhook-certs \
    --namespace linkerd
```

## Setup

Once you have create the secret you can run `linkerd install`, for automatic
sidecar injection you have to label the namespace, only namespace with label
`inkerd-inject=enabled` are injected:

```bash
kubectl label namespace default inkerd-inject=enabled
```

**Note**: only new pod are injected so you have to redeploy the whole namespace
after you have labelled it.

## Known issues

As this feature is _experimental_, we know that there's still a lot of [work
to do][tls-issues]. We **LOVE** bug reports though, so please don't hesitate
to [file an issue][new-issue] if you run into any problems while testing
automatic sidecar injection.

[new-issue]: https://github.com/linkerd/linkerd2/issues/new
