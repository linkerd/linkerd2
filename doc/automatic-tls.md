+++
title = "Experimental: Automatic TLS"
docpage = true
[menu.docs]
    parent = "automatic-tls"
+++

Linkerd can be configured to automatically negotiate Transport Layer Security
(TLS) for application communication.

When TLS is enabled, Linkerd automatically establishes and authenticates
secure, private connections between Linkerd proxies. This is done without
breaking unencrypted communication with endpoints that are not configured
with TLS-enabled Linkerd proxies.

This feature is currently **experimental** and is designed to _fail open_ so
that it cannot easily break existing applications. As the feature matures,
this policy will change in favor of stronger security guarantees.

## Getting started with TLS

The TLS feature is currently disabled by default. To enable it, you must
install the control plane with the `--tls` flag set to `optional`. This
configures the mesh so that TLS is enabled opportunistically:

```bash
linkerd install --tls=optional | kubectl apply -f -
```

This causes a Certificate Authority (CA) container to be run in the
control-plane. The CA watches for the creation and updates of Linkerd-enabled
pods. For each Linkerd-enabled pod, it generates a private key, issues a
certificate, and distributes the certificate and private key to each pod as a
Kubernetes Secret.

Once you've configured the control plane to support TLS, you may enable TLS
for each application when it is injected with the Linkerd proxy:

```bash
linkerd inject  --tls=optional app.yml | kubectl apply -f -
```

Then, tools like `linkerd dashboard`, `linkerd stat`, and `linkerd tap` will
indicate the TLS status of traffic:

```bash
linkerd stat authority -n emojivoto
NAME                        MESHED   SUCCESS      RPS   LATENCY_P50   LATENCY_P95   LATENCY_P99    TLS
emoji-svc.emojivoto:8080         -   100.00%   0.6rps           1ms           1ms           1ms   100%
emoji-svc.emojivoto:8888         -   100.00%   0.8rps           1ms           1ms           9ms   100%
voting-svc.emojivoto:8080        -    45.45%   0.6rps           4ms          10ms          18ms   100%
web-svc.emojivoto:80             -     0.00%   0.6rps           8ms          33ms          39ms   100%
```

## Known issues

As this feature is _experimental_, we know that there's still a lot of [work
to do][tls-issues]. We **LOVE** bug reports though, so please don't hesitate
to [file an issue][new-issue] if you run into any problems while testing
automatic TLS.

[tls-issues]: https://github.com/linkerd/linkerd2/issues?q=is%3Aissue+is%3Aopen+label%3Aarea%2Ftls
[new-issue]: https://github.com/linkerd/linkerd2/issues/new
