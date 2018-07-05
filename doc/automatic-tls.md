+++
title = "Experimental: Automatic TLS"
docpage = true
[menu.docs]
    parent = "automatic-tls"
+++

As of [Conduit v0.5.0][conduit-v0.5.0], Conduit can be configured to automatically
instrument applications to communicate with Transport Layer Security (TLS).

When TLS enabled, Conduit automatically establishes and authenticates
secure,private connections between Conduit proxies. This is done without
breaking unencrypted communication with endpoints that are not configured
with TLS-enabled Conduit proxies.

This feature is currently **experimental** and is designed to _fail open_ so
that it cannot easily break existing applications. As the feature matures,
this policy will change in favor of stronger security guarantees.

### Getting started with TLS


The conduit control plane must be installed with the `--tls=optional` flag:

```
conduit install --tls=optional |kubectl apply -f -
```

This causes a Certificate Authority (CA) container to be run in the control
plane. The CA is responsible for watching the Kubernetes API for new
Conduit-enabled pods and, as new pods are created, it ensures that the pod
has access to a Kubernetes Secret holding the credentials for that Deployment
(or Replication Controller or etc).

Once you've configured the control plane to support TLS, you may enable TLS
for each application when it is injected with the Conduit proxy:

```
conduit inject  --tls=optional app.yml |kubectl apply -f -
```

Then, tools like `conduit dashboard`, `conduit stat`, and `conduit tap` will
indicate the TLS status of traffic:

```
conduit stat authority -n emojivoto
NAME                        MESHED   SUCCESS      RPS   LATENCY_P50   LATENCY_P95   LATENCY_P99    TLS
emoji-svc.emojivoto:8080         -   100.00%   0.6rps           1ms           1ms           1ms   100%
emoji-svc.emojivoto:8888         -   100.00%   0.8rps           1ms           1ms           9ms   100%
voting-svc.emojivoto:8080        -    45.45%   0.6rps           4ms          10ms          18ms   100%
web-svc.emojivoto:80             -     0.00%   0.6rps           8ms          33ms          39ms   100%
```

### Known issues

As this feature is _experimental_, we know that there's still a lot of [work
to do][tls-issues]. We **LOVE** bug reports though, so please don't hesitate
to [file an issue][new-issue] if you run into any problems while testing
automatic TLS.

[conduit-v0.5.0]: https://github.com/runconduit/conduit/releases/tag/v0.5.0
[tls-issues]: https://github.com/runconduit/conduit/issues?q=is%3Aissue+is%3Aopen+label%3Aarea%2Ftls
[new-issue]: https://github.com/runconduit/conduit/issues/new
