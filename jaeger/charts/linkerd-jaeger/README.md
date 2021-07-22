# linkerd-jaeger

The Linkerd-Jaeger extension adds distributed tracing to Linkerd using
OpenCensus and Jaeger.

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square)

![AppVersion: edge-XX.X.X](https://img.shields.io/badge/AppVersion-edge--XX.X.X-informational?style=flat-square)

**Homepage:** <https://linkerd.io>

## Quickstart and documentation

You can run Linkerd on any Kubernetes 1.16+ cluster in a matter of seconds. See
the [Linkerd Getting Started Guide][getting-started] for how.

For more comprehensive documentation, start with the [Linkerd
docs][linkerd-docs].

## Prerequisite: Linkerd Core Control-Plane

Before installing the Linkerd Jaeger extension, The core control-plane has to
be installed first by following the [Linkerd Install
Guide](https://linkerd.io/2/tasks/install/).

## Adding Linkerd's Helm repository

```bash
# To add the repo for Linkerd2 stable releases:
helm repo add linkerd https://helm.linkerd.io/stable
# To add the repo for Linkerd2 edge releases:
helm repo add linkerd-edge https://helm.linkerd.io/edge
```

The following instructions use the `linkerd` repo. For installing an edge
release, just replace with `linkerd-edge`.

## Installing the Jaeger Extension Chart

### Helm v3

```bash
helm install linkerd-jaeger linkerd/linkerd-jaeger
```

### Helm v2

```bash
helm install --name linkerd-jaeger linkerd/linkerd-jaeger
```

## Get involved

* Check out Linkerd's source code at [GitHub][linkerd2].
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

## Requirements

Kubernetes: `>=1.16.0-0`

| Repository | Name | Version |
|------------|------|---------|
| file://../../../charts/partials | partials | 0.1.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| collector.config | string | see `value.yaml` for actual configuration | OpenTelemetry Collector config, See the [Configuration docs](https://opentelemetry.io/docs/collector/configuration/) for more information |
| collector.enabled | bool | `true` | Set to false to exclude collector installation |
| collector.image.name | string | `"otel/opentelemetry-collector"` |  |
| collector.image.pullPolicy | string | `"Always"` |  |
| collector.image.version | string | `"0.27.0"` |  |
| collector.nodeSelector | object | `{"beta.kubernetes.io/os":"linux"}` | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| collector.tolerations | string | `nil` | Tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information |
| installNamespace | bool | `true` | Set to false when installing in a custom namespace. |
| jaeger.enabled | bool | `true` | Set to false to exclude all-in-one Jaeger installation |
| jaeger.image.name | string | `"jaegertracing/all-in-one"` |  |
| jaeger.image.pullPolicy | string | `"Always"` |  |
| jaeger.image.version | string | `"1.19.2"` |  |
| jaeger.nodeSelector | object | `{"beta.kubernetes.io/os":"linux"}` | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| jaeger.tolerations | string | `nil` | Tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information |
| linkerdNamespace | string | `"linkerd"` | Namespace of the Linkerd core control-plane install |
| linkerdVersion | string | `"linkerdVersionValue"` |  |
| namespace | string | `"linkerd-jaeger"` |  |
| nodeSelector | object | `{"beta.kubernetes.io/os":"linux"}` | Default nodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| tolerations | string | `nil` | Default tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information |
| webhook.caBundle | string | `""` | if empty, Helm will auto-generate this field, unless externalSecret is set to true. |
| webhook.collectorSvcAccount | string | `"collector"` | service account associated with the collector instance |
| webhook.collectorSvcAddr | string | `"collector.linkerd-jaeger:55678"` | collector service address for the proxies to send trace data. Points by default to the the linkerd-jaeger collector |
| webhook.crtPEM | string | `""` | if empty, Helm will auto-generate these fields |
| webhook.externalSecret | bool | `false` |  |
| webhook.failurePolicy | string | `"Ignore"` |  |
| webhook.image.name | string | `"cr.l5d.io/linkerd/jaeger-webhook"` |  |
| webhook.image.pullPolicy | string | `"IfNotPresent"` |  |
| webhook.image.version | string | `"linkerdVersionValue"` |  |
| webhook.keyPEM | string | `""` |  |
| webhook.logLevel | string | `"info"` |  |
| webhook.namespaceSelector | string | `nil` |  |
| webhook.nodeSelector | object | `{"beta.kubernetes.io/os":"linux"}` | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| webhook.objectSelector | string | `nil` |  |
| webhook.tolerations | string | `nil` | Tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.4.0](https://github.com/norwoodj/helm-docs/releases/v1.4.0)
