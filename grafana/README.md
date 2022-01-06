# Using Grafana with Linkerd

You can install Grafana in various ways, like using the [Grafana official Helm
chart](https://github.com/grafana/helm-charts/tree/main/charts/grafana), or the
[Grafana Operator](https://github.com/grafana-operator/grafana-operator). Hosted
solutions are also available, like [Grafana
Cloud](https://grafana.com/products/cloud/).

The file `grafana/values.yaml` provides a default Helm config for the [Grafana
official Helm
chart](https://github.com/grafana/helm-charts/tree/main/charts/grafana), which
pulls the Linkerd dashboards published at
https://grafana.com/orgs/linkerd/dashboards .

You can install the chart like this:

```
helm repo add grafana https://grafana.github.io/helm-charts helm install
grafana -n grafana --create-namespace grafana/grafana -f https://github.com/linkerd/linkerd2/blob/main/grafana/values.yaml
```

Please make sure to update the entries in `grafana/values.yaml` before using the
file; in particular:
- auth and log settings under `grafana.ini`
- `datasources.datasources.yaml.datasources[0].url` should point to your
  Prometheus service

The other installation methods can easily import those same dashboards using
their IDs, as listed in `grafana/values.yaml`.

In order to have the Linkerd Viz Dashboard show the Grafana icon there where
relevant, and have it link to the appropriate Grafana dashboard, make sure you
have a proper location set up in the `grafana.url` setting in Linkerd Viz's
`values.yaml`.

## Note to developers

The `grafana/dashboards` directory contains the same dashboard definitions
published under https://grafana.com/orgs/linkerd . Please keep them in sync when
making any changes.
