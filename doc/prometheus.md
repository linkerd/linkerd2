+++
title = "Exporting metrics to Prometheus"
weight = 5
docpage = true
[menu.docs]
  parent = "prometheus"
+++

If you have an existing Prometheus cluster, it is very easy to export Conduit's
rich telemetry data to your cluster.  Simply add the following item to your
`scrape_configs` in your Prometheus config file:

```yaml
    - job_name: 'conduit'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          # Replace this with the namespace that Conduit is running in
          names: ['conduit']
      relabel_configs:
      - source_labels: [__meta_kubernetes_pod_container_port_name]
        action: keep
        regex: ^admin-http$
```

That's it!  Your Prometheus cluster is now configured to scrape Conduit's
metrics.  Conduit's metrics will have the label `job="conduit"` and include:

* `requests_total`: Total number of requests
* `responses_total`: Total number of responses
* `response_latency_ms`: Response latency in milliseconds

All metrics include the following labels:

* `source_deployment`: The deployment (or replicaset, job, etc.) that sent the request
* `target_deployment`: The deployment (or replicaset, job, etc.) that received the request
