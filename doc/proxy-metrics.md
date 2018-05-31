+++
title = "Proxy Metrics"
docpage = true
[menu.docs]
  parent = "proxy-metrics"
+++

The Conduit proxy exposes metrics that describe the traffic flowing through the
proxy.  The following metrics are available at `/metrics` on the proxy's metrics
port (default: `:4191`) in the [Prometheus format][prom-format]:

# Protocol-Level Metrics

### `request_total`

A counter of the number of requests the proxy has received.  This is incremented
when the request stream begins.

### `response_total`

A counter of the number of responses the proxy has received.  This is
incremented when the response stream ends.

### `response_latency_ms`

A histogram of response latencies. This measurement reflects the
[time-to-first-byte][ttfb] (TTFB) by recording the elapsed time between the proxy
processing a request's headers and the first data frame of the response. If a response
does not include any data, the end-of-stream event is used. The TTFB measurement is used
so that Conduit accurately reflects application behavior when a server provides response
headers immediately but is slow to begin serving the response body.

Note that latency measurements are not exported to Prometheus until the stream
_completes_. This is necessary so that latencies can be labeled with the appropriate
[response classification](#rsp-class).

## Labels

Each of these metrics has the following labels:

* `authority`: The value of the `:authority` (HTTP/2) or `Host` (HTTP/1.1)
               header of the request.
* `direction`: `inbound` if the request originated from outside of the pod,
               `outbound` if the request originated from inside of the pod.
* `tls`: `true` if the request's connection was secured with TLS.

### Response Labels

The following labels are only applicable on `response_*` metrics.

<a name="rsp-class"></a>
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
* `dst_stateful_set`: The stateful set to which this request is being sent.
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
* `stateful_set`: The stateful set that the pod belongs to (if applicable).

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

# Transport-Level Metrics

The following metrics are collected at the level of the underlying transport
layer.

### `tcp_open_total`

A counter of the total number of opened transport connections.

### `tcp_close_total`

A counter of the total number of transport connections which have closed.

### `tcp_open_connections`

A gauge of the number of transport connections currently open.

### `tcp_write_bytes_total`

A counter of the total number of sent bytes. This is updated when the
connection closes.

### `tcp_read_bytes_total`

A counter of the total number of recieved bytes. This is updated when the
connection closes.

### `tcp_connection_duration_ms`

A histogram of the duration of the lifetime of a connection, in milliseconds.
This is updated when the connection closes.

## Labels

Each of these metrics has the following labels:

* `direction`: `inbound` if the connection was established either from outside the
                pod to the proxy, or from the proxy to the application,
               `outbound` if the connection was established either from the
                application to the proxy, or from the proxy to outside the pod.
* `peer`: `src` if the connection was accepted by the proxy from the source,
          `dst` if the connection was opened by the proxy to the destination.

Note that the labels described above under the heading "Prometheus Collector labels"
are also added to transport-level metrics, when applicable.

### Connection Close Labels

The following labels are added only to metrics which are updated when a connection closes
(`tcp_close_total` and `tcp_connection_duration_ms`):

+ `classification`: `success` if the connection terminated cleanly, `failure` if the
                    connection closed due to a connection failure.

[prom-format]: https://prometheus.io/docs/instrumenting/exposition_formats/#format-version-0.0.4
[pod-template-hash]: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#pod-template-hash-label
[ttfb]: https://en.wikipedia.org/wiki/Time_to_first_byte
