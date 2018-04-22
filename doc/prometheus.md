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
    - job_name: 'conduit-controller'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: ['{{.Namespace}}']
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_label_conduit_io_control_plane_component
        - __meta_kubernetes_pod_container_port_name
        action: keep
        regex: (.*);admin-http$
      - source_labels: [__meta_kubernetes_pod_container_name]
        action: replace
        target_label: component

    - job_name: 'conduit-proxy'
      kubernetes_sd_configs:
      - role: pod
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_container_name
        - __meta_kubernetes_pod_container_port_name
        action: keep
        regex: ^conduit-proxy;conduit-metrics$
      - source_labels: [__meta_kubernetes_namespace]
        action: replace
        target_label: namespace
      - source_labels: [__meta_kubernetes_pod_name]
        action: replace
        target_label: pod
      # special case k8s' "job" label, to not interfere with prometheus' "job"
      # label
      # __meta_kubernetes_pod_label_conduit_io_proxy_job=foo =>
      # k8s_job=foo
      - source_labels: [__meta_kubernetes_pod_label_conduit_io_proxy_job]
        action: replace
        target_label: k8s_job
      # __meta_kubernetes_pod_label_conduit_io_proxy_deployment=foo =>
      # deployment=foo
      - action: labelmap
        regex: __meta_kubernetes_pod_label_conduit_io_proxy_(.+)
      # drop all labels that we just made copies of in the previous labelmap
      - action: labeldrop
        regex: __meta_kubernetes_pod_label_conduit_io_proxy_(.+)
      # __meta_kubernetes_pod_label_foo=bar => foo=bar
      - action: labelmap
        regex: __meta_kubernetes_pod_label_(.+)
```

That's it!  Your Prometheus cluster is now configured to scrape Conduit's
metrics.

Conduit's proxy metrics will have the label `job="conduit-proxy"`.  Conduit's
control-plane metrics will have the label `job="conduit-controller"`.

For more information on specific metric and label definitions, have a look at
[Proxy Metrics](/proxy-metrics),
