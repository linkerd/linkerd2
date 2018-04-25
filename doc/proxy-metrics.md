+++
title = "Proxy Metrics"
docpage = true
[menu.docs]
  parent = "proxy-metrics"
+++

The Conduit proxy exposes metrics that describe the traffic flowing through the
proxy.  The following metrics are available at `/metrics` on the proxy's metrics
port (default: `:4191`) in the [Prometheus format][prom-format]:

### `request_total`

A counter of the number of requests the proxy has received.  This is incremented
when the request stream begins.

### `response_total`

A counter of the number of responses the proxy has received.  This is
incremented when the response stream ends.

### `response_latency_ms`

A histogram of the total latency of a response.  This is measured from when the
request headers are received to when the response stream has completed.

## Labels

Each of these metrics has the following labels:

* `authority`: The value of the `:authority` (HTTP/2) or `Host` (HTTP/1.1)
               header of the request.
* `direction`: `inbound` if the request originated from outside of the pod,
               `outbound` if the request originated from inside of the pod.

### Response Labels

The following labels are only applicable on `response_*` metrics.

* `classification`: `success` if the response was successful, or `failure` if
                    a server error occurred. This classification is based on
                    the gRPC status code if one is present, and on the HTTP
                    status code otherwise. Only applicable to response metrics.
* `grpc_status_code`: The value of the `grpc-status` trailer.  Only applicable
                      for gRPC responses.
* `status_code`: The HTTP status code of the response.

### Outbound labels

The following labels are only applicable if `direction=outbound`.

* `dst_deployment`: The deployment to which this request is being sent.
* `dst_k8s_job`: The job to which this request is being sent.
* `dst_replica_set`: The replica set to which this request is being sent.
* `dst_daemon_set`: The daemon set to which this request is being sent.
* `dst_replication_controller`: The replication controller to which this request
                                is being sent.
* `dst_namespace`: The namespace to which this request is being sent.
* `dst_service`: The service to which this request is being sent.
* `dst_pod_template_hash`: The [pod-template-hash][pod-template-hash] of the pod
                           to which this request is being sent. This label
                           selector roughly approximates a pod's `ReplicaSet` or
                           `ReplicationController`.

### Prometheus Collector labels

The following labels are added by the Prometheus collector.

* `instance`: ip:port of the pod.
* `job`: The Prometheus job responsible for the collection, typically
         `conduit-proxy`.

#### Kubernetes labels added at collection time

Kubernetes namespace, pod name, and all labels are mapped to corresponding
Prometheus labels.

* `namespace`: Kubernetes namespace that the pod belongs to.
* `pod`: Kubernetes pod name.
* `pod_template_hash`: Corresponds to the [pod-template-hash][pod-template-hash]
                       Kubernetes label. This value changes during redeploys and
                       rolling restarts. This label selector roughly
                       approximates a pod's `ReplicaSet` or
                       `ReplicationController`.

#### Conduit labels added at collection time

Kubernetes labels prefixed with `conduit.io/` are added to your application at
`conduit inject` time. More specifically, Kubernetes labels prefixed with
`conduit.io/proxy-*` will correspond to these Prometheus labels:

* `daemon_set`: The daemon set that the pod belongs to (if applicable).
* `deployment`: The deployment that the pod belongs to (if applicable).
* `k8s_job`: The job that the pod belongs to (if applicable).
* `replica_set`: The replica set that the pod belongs to (if applicable).
* `replication_controller`: The replication controller that the pod belongs to
                            (if applicable).

### Example

Here's a concrete example, given the following pod snippet:

```yaml
name: vote-bot-5b7f5657f6-xbjjw
namespace: emojivoto
labels:
  app: vote-bot
  conduit.io/control-plane-ns: conduit
  conduit.io/proxy-deployment: vote-bot
  pod-template-hash: "3957278789"
  test: vote-bot-test
```

The resulting Prometheus labels will look like this:

```
request_total{
  pod="vote-bot-5b7f5657f6-xbjjw",
  namespace="emojivoto",
  app="vote-bot",
  conduit_io_control_plane_ns="conduit",
  deployment="vote-bot",
  pod_template_hash="3957278789",
  test="vote-bot-test",
  instance="10.1.3.93:4191",
  job="conduit-proxy"
}
```

[prom-format]: https://prometheus.io/docs/instrumenting/exposition_formats/#format-version-0.0.4
[pod-template-hash]: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#pod-template-hash-label
