+++
title = "Proxy Metrics"
docpage = true
[menu.docs]
  parent = "proxy-metrics"
+++

The Conduit proxy exposes metrics that describe the traffic flowing through the
proxy.  These metrics are exposed in Prometheus format on the proxy control port
(4190 by default) at the `/metrics` path.  The following metrics will be
exposed:

### `request_total`

A counter of the number of requests the proxy has received.

### `request_duration_ms`

A histogram of the duration of a request.  This is measured from when the
request headers are received to when the request stream has completed.

### `response_total`

A counter of the number of responses the proxy has received.

### `response_duration_ms`

A histogram of the duration of a response.  This is measured from when the
response headers are received to when the response stream has completed.

### `response_latency_ms`

A histogram of the total latency of a response.  This is measured from when the
request headers are received to when the response stream has completed.

## Labels

Each of these metrics has the following labels:

* `direction`: `inbound` if the request originated from outside of the pod,
               `outbound` if the request originated from inside of the pod.
* `deployment`: The deployment that the pod belongs to (if applicable).
* `job`: The job that the pod belongs to (if applicable).
* `replica_set`: The replica set that the pod belongs to (if applicable).
* `daemon_set`: The daemon set that the pod belongs to (if applicable).
* `replication_controller`: The replication controller that the pod belongs to 
                            (if applicable).
* `authority`: The value of the `:authority` (HTTP/2) or `Host` (HTTP/1.1)
               header of the request.
* `dst_deployment`: The deployment to which this request is being sent.  Only
                    applicable if `direction=outbound`.
* `dst_job`: The job to which this request is being sent.  Only applicable if
             `direction=outbound`.
* `dst_replica_set`: The replica set to which this request is being sent.  Only
                     applicable if `direction=outbound`.
* `dst_daemon_set`: The daemon set to which this request is being sent.  Only
                    applicable if `direction=outbound`.
* `dst_replication_controller`: The relication controller to which this request
                                is being sent.  Only applicable if
                                `direction=outbound`.
* `dst_namespace`: The namespace to which this request is being sent.  Only
                   applicable if `direction=outbound`.
* `status_code`: The HTTP status code of the response.  Only applicable to
                 response metrics.
* `grpc_status_code`: The value of the `grpc-status` trailer.  Only applicable
                      to response metrics for gRPC responses.

Note that the `instance` and `namespace` labels will typically be added by the
Prometheus collector.
