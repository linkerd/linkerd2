# linkerd-viz

Linkerd Viz extension contains the observability and visualization
components that can be integrated directly.

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square)

![AppVersion: edge-XX.X.X](https://img.shields.io/badge/AppVersion-edge--XX.X.X-informational?style=flat-square)

**Homepage:** <https://linkerd.io>

## Quickstart and documentation

You can run Linkerd on any Kubernetes 1.13+ cluster in a matter of seconds. See
the [Linkerd Getting Started Guide][getting-started] for how.

For more comprehensive documentation, start with the [Linkerd
docs][linkerd-docs].

## Prerequisite: identity certificates

The identity component of Linkerd requires setting up a trust anchor
certificate, and an issuer certificate with its key. These need to be provided
to Helm by the user (unlike when using the `linkerd install` CLI which can
generate these automatically). You can provide your own, or follow [these
instructions](https://linkerd.io/2/tasks/generate-certificates/) to generate new
ones.

Note that the provided certificates must be ECDSA certificates.

## Adding Linkerd's Helm repository

```bash
# To add the repo for Linkerd2 stable releases:
helm repo add linkerd https://helm.linkerd.io/stable
# To add the repo for Linkerd2 edge releases:
helm repo add linkerd-edge https://helm.linkerd.io/edge
```

The following instructions use the `linkerd` repo. For installing an edge
release, just replace with `linkerd-edge`.

## Installing the chart

You must provide the certificates and keys described in the preceding section,
and the same expiration date you used to generate the Issuer certificate.

In this example we set the expiration date to one year ahead:

```bash
helm install \
  --set-file global.identityTrustAnchorsPEM=ca.crt \
  --set-file identity.issuer.tls.crtPEM=issuer.crt \
  --set-file identity.issuer.tls.keyPEM=issuer.key \
  --set identity.issuer.crtExpiry=$(date -d '+8760 hour' +"%Y-%m-%dT%H:%M:%SZ") \
  linkerd/linkerd2
```

## Setting High-Availability

Besides the default `values.yaml` file, the chart provides a `values-ha.yaml`
file that overrides some default values as to set things up under a
high-availability scenario, analogous to the `--ha` option in `linkerd install`.
Values such as higher number of replicas, higher memory/cpu limits and
affinities are specified in that file.

You can get ahold of `values-ha.yaml` by fetching the chart files:

```bash
helm fetch --untar linkerd/linkerd2
```

Then use the `-f` flag to provide the override file, for example:

```bash
helm install \
  --set-file global.identityTrustAnchorsPEM=ca.crt \
  --set-file identity.issuer.tls.crtPEM=issuer.crt \
  --set-file identity.issuer.tls.keyPEM=issuer.key \
  --set identity.issuer.crtExpiry=$(date -d '+8760 hour' +"%Y-%m-%dT%H:%M:%SZ") \
  -f linkerd2/values-ha.yaml
  linkerd/linkerd2
```

## Get involved

* Check out Linkerd's source code at [Github][linkerd2].
* Join Linkerd's [user mailing list][linkerd-users], [developer mailing
  list][linkerd-dev], and [announcements mailing list][linkerd-announce].
* Follow [@linkerd][twitter] on Twitter.
* Join the [Linkerd Slack][slack].

[cncf]: https://www.cncf.io/
[getting-started]: https://linkerd.io/2/getting-started/
[linkerd2]: https://github.com/linkerd/linkerd2
[linkerd-announce]: https://lists.cncf.io/g/cncf-linkerd-announce
[linkerd-dev]: https://lists.cncf.io/g/cncf-linkerd-dev
[linkerd-docs]: https://linkerd.io/2/overview/
[linkerd-users]: https://lists.cncf.io/g/cncf-linkerd-users
[slack]: http://slack.linkerd.io
[twitter]: https://twitter.com/linkerd

## Addons for linkerd

For the linkerd application there are some addons that can be configured. The
documentation for the configurations of the addons can be found in their
respective readme.md

[Prometheus](https://github.com/linkerd/linkerd2/blob/main/charts/add-ons/prometheus/README.md)

[Grafana](https://github.com/linkerd/linkerd2/blob/main/charts/add-ons/grafana/README.md)

[Tracing](https://github.com/linkerd/linkerd2/blob/main/charts/add-ons/tracing/README.md)

## Requirements

Kubernetes: `>=1.13.0-0`

| Repository | Name | Version |
|------------|------|---------|
| file://../../../charts/partials | partials | 0.1.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| clusterDomain | string | `"cluster.local"` |  |
| createdByAnnotation | string | `"linkerd.io/created-by"` | Annotation label for the proxy. Do not edit. |
| dashboard.UID | int | `2103` |  |
| dashboard.enforcedHostRegexp | string | `""` | Host header validation regex for the dashboard. See the [Linkerd documentation](https://linkerd.io/2/tasks/exposing-dashboard) for more information |
| dashboard.image.name | string | `"ghcr.io/linkerd/web"` | Docker image name for the web instance |
| dashboard.image.tag | string | `"linkerdVersionValue"` | Docker image tag for the web instance |
| dashboard.logLevel | string | `"info"` | log level of the dashboard component |
| dashboard.replicas | int | `1` | Number of replicas of dashboard |
| dashboard.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the web container can use |
| dashboard.resources.cpu.request | string | `nil` | Amount of CPU units that the web container requests |
| dashboard.resources.memory.limit | string | `nil` | Maximum amount of memory that web container can use |
| dashboard.resources.memory.request | string | `nil` | Amount of memory that the web container requests |
| globalLogLevel | string | `"info"` |  |
| globalUID | int | `2103` |  |
| grafana.enabled | bool | `true` | toggle field to enable or disable grafana |
| grafana.image.name | string | `"ghcr.io/linkerd/grafana"` | Docker image name for the grafana instance |
| grafana.image.tag | string | `"linkerdVersionValue"` | Docker image tag for the grafana instance |
| grafana.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the grafana container can use |
| grafana.resources.cpu.request | string | `nil` | Amount of CPU units that the grafana container requests |
| grafana.resources.memory.limit | string | `nil` | Maximum amount of memory that grafana container can use |
| grafana.resources.memory.request | string | `nil` | Amount of memory that the grafana container requests |
| identityTrustDomain | string | `"cluster.local"` |  |
| linkerdNamespace | string | `"linkerd"` |  |
| linkerdVersion | string | `"linkerdVersionValue"` |  |
| namespace | string | `"linkerd-viz"` |  |
| nodeSelector | object | `{"beta.kubernetes.io/os":"linux"}` | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| prometheus.alertManagers | string | `nil` | Alertmanager instances the Prometheus server sends alerts to configured via the static_configs parameter. |
| prometheus.alertRelabelConfigs | string | `nil` | Alert relabeling is applied to alerts before they are sent to the Alertmanager. |
| prometheus.args | object | `{"config.file":"/etc/prometheus/prometheus.yml","log.level":"info","storage.tsdb.path":"/data","storage.tsdb.retention.time":"6h"}` | Command line options for Prometheus binary |
| prometheus.enabled | bool | `true` | toggle field to enable or disable prometheus |
| prometheus.globalConfig | object | `{"evaluation_interval":"10s","scrape_interval":"10s","scrape_timeout":"10s"}` | The global configuration specifies parameters that are valid in all other configuration contexts. |
| prometheus.image.name | string | `"prom/prometheus"` | Docker image name for the prometheus instance |
| prometheus.image.pullPolicy | string | `"Always"` |  |
| prometheus.image.tag | string | `"v2.19.3"` | Docker image tag for the prometheus instance |
| prometheus.remoteWrite | string | `nil` | Allows transparently sending samples to an endpoint. Mostly used for long term storage. |
| prometheus.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the prometheus container can use |
| prometheus.resources.cpu.request | string | `nil` | Amount of CPU units that the prometheus container requests |
| prometheus.resources.memory.limit | string | `nil` | Maximum amount of memory that prometheus container can use |
| prometheus.resources.memory.request | string | `nil` | Amount of memory that the prometheus container requests |
| prometheus.ruleConfigMapMounts | string | `nil` | Alerting/recording rule ConfigMap mounts (sub-path names must end in ´_rules.yml´ or ´_rules.yaml´) |
| prometheus.scrapeConfigs | string | `nil` | A scrapeConfigs section specifies a set of targets and parameters describing how to scrape them. |
| prometheus.sideCarContainers | string | `nil` | A sidecarContainers section specifies a list of secondary containers to run in the prometheus pod e.g. to export data to non-prometheus systems |
| proxyInjectAnnotation | string | `"linkerd.io/inject"` |  |
| tap.UID | int | `2103` |  |
| tap.caBundle | string | `""` | Bundle of CA certificates for Tap component. If not provided then Helm will use the certificate generated  for `tap.crtPEM`. If `tap.externalSecret` is set to true, this value must be set, as no certificate will be generated. |
| tap.crtPEM | string | `""` | Certificate for the Tap component. If not provided then Helm will generate one. |
| tap.externalSecret | bool | `false` | Do not create a secret resource for the Tap component. If this is set to `true`, the value `tap.caBundle` must be set (see below). |
| tap.image.name | string | `"ghcr.io/linkerd/controller"` | Docker image name for the grafana instance |
| tap.image.tag | string | `"linkerdVersionValue"` | Docker image tag for the grafana instance |
| tap.keyPEM | string | `""` | Certificate key for Tap component. If not provided then Helm will generate one.  |
| tap.logLevel | string | `"info"` | log level of the tap component |
| tap.replicas | int | `1` |  |
| tap.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the tap container can use |
| tap.resources.cpu.request | string | `nil` | Amount of CPU units that the tap container requests |
| tap.resources.memory.limit | string | `nil` | Maximum amount of memory that tap container can use |
| tap.resources.memory.request | string | `nil` | Amount of memory that the tap container requests |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.4.0](https://github.com/norwoodj/helm-docs/releases/v1.4.0)
