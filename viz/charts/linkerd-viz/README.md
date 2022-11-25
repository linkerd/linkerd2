# linkerd-viz

The Linkerd-Viz extension contains observability and visualization
components for Linkerd.

![Version: 30.4.5-edge](https://img.shields.io/badge/Version-30.4.5--edge-informational?style=flat-square)

![AppVersion: edge-XX.X.X](https://img.shields.io/badge/AppVersion-edge--XX.X.X-informational?style=flat-square)

**Homepage:** <https://linkerd.io>

## Quickstart and documentation

You can run Linkerd on any Kubernetes 1.21+ cluster in a matter of seconds. See
the [Linkerd Getting Started Guide][getting-started] for how.

For more comprehensive documentation, start with the [Linkerd
docs][linkerd-docs].

## Prerequisite: Linkerd Core Control-Plane

Before installing the Linkerd Viz extension, The core control-plane has to
be installed first by following the [Linkerd Install
Guide](https://linkerd.io/2/tasks/install/).

## Adding Linkerd's Helm repository

```bash
# To add the repo for Linkerd stable releases:
helm repo add linkerd https://helm.linkerd.io/stable
# To add the repo for Linkerd edge releases:
helm repo add linkerd-edge https://helm.linkerd.io/edge
```

The following instructions use the `linkerd` repo. For installing an edge
release, just replace with `linkerd-edge`.

## Installing the Viz Extension Chart

```bash
helm install linkerd-viz -n linkerd-viz --create-namespace linkerd/linkerd-viz
```

## Get involved

* Check out Linkerd's source code at [GitHub][linkerd2].
* Join Linkerd's [user mailing list][linkerd-users], [developer mailing
  list][linkerd-dev], and [announcements mailing list][linkerd-announce].
* Follow [@linkerd][twitter] on Twitter.
* Join the [Linkerd Slack][slack].

[getting-started]: https://linkerd.io/2/getting-started/
[linkerd2]: https://github.com/linkerd/linkerd2
[linkerd-announce]: https://lists.cncf.io/g/cncf-linkerd-announce
[linkerd-dev]: https://lists.cncf.io/g/cncf-linkerd-dev
[linkerd-docs]: https://linkerd.io/2/overview/
[linkerd-users]: https://lists.cncf.io/g/cncf-linkerd-users
[slack]: http://slack.linkerd.io
[twitter]: https://twitter.com/linkerd

## Requirements

Kubernetes: `>=1.21.0-0`

| Repository | Name | Version |
|------------|------|---------|
| file://../../../charts/partials | partials | 0.1.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| clusterDomain | string | `"cluster.local"` | Kubernetes DNS Domain name to use |
| commonLabels | object | `{}` | Labels to apply to all resources |
| dashboard.UID | string | `nil` | UID for the dashboard resource |
| dashboard.enforcedHostRegexp | string | `""` | Host header validation regex for the dashboard. See the [Linkerd documentation](https://linkerd.io/2/tasks/exposing-dashboard) for more information |
| dashboard.image.name | string | `"web"` | Docker image name for the web instance |
| dashboard.image.pullPolicy | string | defaultImagePullPolicy | Pull policy for the  web component |
| dashboard.image.registry | string | defaultRegistry | Docker registry for the web instance |
| dashboard.image.tag | string | linkerdVersion | Docker image tag for the web instance |
| dashboard.logFormat | string | defaultLogFormat | log format of the dashboard component |
| dashboard.logLevel | string | defaultLogLevel | log level of the dashboard component |
| dashboard.proxy | string | `nil` |  |
| dashboard.replicas | int | `1` | Number of replicas of dashboard |
| dashboard.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the web container can use |
| dashboard.resources.cpu.request | string | `nil` | Amount of CPU units that the web container requests |
| dashboard.resources.ephemeral-storage.limit | string | `""` | Maximum amount of ephemeral storage that the web container can use |
| dashboard.resources.ephemeral-storage.request | string | `""` | Amount of ephemeral storage that the web container requests |
| dashboard.resources.memory.limit | string | `nil` | Maximum amount of memory that web container can use |
| dashboard.resources.memory.request | string | `nil` | Amount of memory that the web container requests |
| dashboard.restrictPrivileges | bool | `false` | Restrict the Linkerd Dashboard's default privileges to disallow Tap and Check |
| defaultImagePullPolicy | string | `"IfNotPresent"` | Docker imagePullPolicy for all viz components |
| defaultLogFormat | string | `"plain"` | Log format (`plain` or `json`) for all the viz components. |
| defaultLogLevel | string | `"info"` | Log level for all the viz components |
| defaultRegistry | string | `"cr.l5d.io/linkerd"` | Docker registry for all viz components |
| defaultUID | int | `2103` | UID for all the viz components |
| enablePSP | bool | `false` | Create Roles and RoleBindings to associate this extension's ServiceAccounts to the control plane PSP resource. This requires that `enabledPSP` is set to true on the control plane install. Note PSP has been deprecated since k8s v1.21 |
| enablePodAntiAffinity | bool | `false` | Enables Pod Anti Affinity logic to balance the placement of replicas across hosts and zones for High Availability. Enable this only when you have multiple replicas of components. |
| grafana.externalUrl | string | `nil` | url of a Grafana instance hosted off-cluster. Cannot be set if grafana.url is set. The reverse proxy will not be used for this URL. |
| grafana.uidPrefix | string | `nil` | prefix for Grafana dashboard UID's, used when grafana.externalUrl is set. |
| grafana.url | string | `nil` | url of an in-cluster Grafana instance with reverse proxy configured, used by the Linkerd viz web dashboard to provide direct links to specific Grafana dashboards. Cannot be set if grafana.externalUrl is set. See the [Linkerd documentation](https://linkerd.io/2/tasks/grafana) for more information |
| identityTrustDomain | string | clusterDomain | Trust domain used for identity |
| imagePullSecrets | list | `[]` | For Private docker registries, authentication is needed.  Registry secrets are applied to the respective service accounts |
| jaegerUrl | string | `""` | url of external jaeger instance Set this to `jaeger.linkerd-jaeger.svc.<clusterDomain>:16686` if you plan to use jaeger extension |
| linkerdNamespace | string | `"linkerd"` | Namespace of the Linkerd core control-plane install |
| linkerdVersion | string | `"linkerdVersionValue"` | control plane version. See Proxy section for proxy version |
| metricsAPI.UID | string | `nil` | UID for the metrics-api resource |
| metricsAPI.image.name | string | `"metrics-api"` | Docker image name for the metrics-api component |
| metricsAPI.image.pullPolicy | string | defaultImagePullPolicy | Pull policy for the metrics-api component |
| metricsAPI.image.registry | string | defaultRegistry | Docker registry for the metrics-api component |
| metricsAPI.image.tag | string | linkerdVersion | Docker image tag for the metrics-api component |
| metricsAPI.logFormat | string | defaultLogFormat | log format of the metrics-api component |
| metricsAPI.logLevel | string | defaultLogLevel | log level of the metrics-api component |
| metricsAPI.nodeSelector | object | `{"kubernetes.io/os":"linux"}` | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| metricsAPI.proxy | string | `nil` |  |
| metricsAPI.replicas | int | `1` | number of replicas of the metrics-api component |
| metricsAPI.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the metrics-api container can use |
| metricsAPI.resources.cpu.request | string | `nil` | Amount of CPU units that the metrics-api container requests |
| metricsAPI.resources.ephemeral-storage.limit | string | `""` | Maximum amount of ephemeral storage that the metrics-api container can use |
| metricsAPI.resources.ephemeral-storage.request | string | `""` | Amount of ephemeral storage that the metrics-api container requests |
| metricsAPI.resources.memory.limit | string | `nil` | Maximum amount of memory that metrics-api container can use |
| metricsAPI.resources.memory.request | string | `nil` | Amount of memory that the metrics-api container requests |
| metricsAPI.tolerations | string | `nil` | Tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information |
| namespaceMetadata.image.name | string | `"curl"` | Docker image name for the namespace-metadata instance |
| namespaceMetadata.image.pullPolicy | string | defaultImagePullPolicy | Pull policy for the namespace-metadata instance |
| namespaceMetadata.image.registry | string | `"curlimages"` | Docker registry for the namespace-metadata instance |
| namespaceMetadata.image.tag | string | `"7.78.0"` | Docker image tag for the namespace-metadata instance |
| nodeSelector | object | `{"kubernetes.io/os":"linux"}` | Default nodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| podLabels | object | `{}` | Additional labels to add to all pods |
| prometheus.alertRelabelConfigs | string | `nil` | Alert relabeling is applied to alerts before they are sent to the Alertmanager. |
| prometheus.alertmanagers | string | `nil` | Alertmanager instances the Prometheus server sends alerts to configured via the static_configs parameter. |
| prometheus.args | object | `{"config.file":"/etc/prometheus/prometheus.yml","storage.tsdb.path":"/data","storage.tsdb.retention.time":"6h"}` | Command line options for Prometheus binary |
| prometheus.enabled | bool | `true` | toggle field to enable or disable prometheus |
| prometheus.globalConfig | object | `{"evaluation_interval":"10s","scrape_interval":"10s","scrape_timeout":"10s"}` | The global configuration specifies parameters that are valid in all other configuration contexts. |
| prometheus.image.name | string | `"prometheus"` | Docker image name for the prometheus instance |
| prometheus.image.pullPolicy | string | defaultImagePullPolicy | Pull policy for the prometheus instance |
| prometheus.image.registry | string | `"prom"` | Docker registry for the prometheus instance |
| prometheus.image.tag | string | `"v2.30.3"` | Docker image tag for the prometheus instance |
| prometheus.logFormat | string | defaultLogLevel | log format (plain, json) of the prometheus instance |
| prometheus.logLevel | string | defaultLogLevel | log level of the prometheus instance |
| prometheus.nodeSelector | object | `{"kubernetes.io/os":"linux"}` | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| prometheus.proxy | string | `nil` |  |
| prometheus.remoteWrite | string | `nil` | Allows transparently sending samples to an endpoint. Mostly used for long term storage. |
| prometheus.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the prometheus container can use |
| prometheus.resources.cpu.request | string | `nil` | Amount of CPU units that the prometheus container requests |
| prometheus.resources.ephemeral-storage.limit | string | `""` | Maximum amount of ephemeral storage that the prometheus container can use |
| prometheus.resources.ephemeral-storage.request | string | `""` | Amount of ephemeral storage that the prometheus container requests |
| prometheus.resources.memory.limit | string | `nil` | Maximum amount of memory that prometheus container can use |
| prometheus.resources.memory.request | string | `nil` | Amount of memory that the prometheus container requests |
| prometheus.ruleConfigMapMounts | string | `nil` | Alerting/recording rule ConfigMap mounts (sub-path names must end in ´_rules.yml´ or ´_rules.yaml´) |
| prometheus.scrapeConfigs | string | `nil` | A scrapeConfigs section specifies a set of targets and parameters describing how to scrape them. |
| prometheus.sidecarContainers | string | `nil` | A sidecarContainers section specifies a list of secondary containers to run in the prometheus pod e.g. to export data to non-prometheus systems |
| prometheus.tolerations | string | `nil` | Tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information |
| prometheusUrl | string | `""` | url of external prometheus instance |
| tap.UID | string | `nil` | UID for the dashboard resource |
| tap.caBundle | string | `""` | Bundle of CA certificates for tap. If not provided nor injected with cert-manager, then Helm will use the certificate generated for `tap.crtPEM`. If `tap.externalSecret` is set to true, this value, injectCaFrom, or injectCaFromSecret must be set, as no certificate will be generated. See the cert-manager [CA Injector Docs](https://cert-manager.io/docs/concepts/ca-injector) for more information. |
| tap.crtPEM | string | `""` | Certificate for the Tap component. If not provided and not using an external secret then Helm will generate one. |
| tap.externalSecret | bool | `false` | Do not create a secret resource for the Tap component. If this is set to `true`, the value `tap.caBundle` must be set or the ca bundle must injected with cert-manager ca injector using `tap.injectCaFrom` or `tap.injectCaFromSecret` (see below). |
| tap.image.name | string | `"tap"` | Docker image name for the tap instance |
| tap.image.pullPolicy | string | defaultImagePullPolicy | Pull policy for the tap component |
| tap.image.registry | string | defaultRegistry | Docker registry for the tap instance |
| tap.image.tag | string | linkerdVersion | Docker image tag for the tap instance |
| tap.injectCaFrom | string | `""` | Inject the CA bundle from a cert-manager Certificate. See the cert-manager [CA Injector Docs](https://cert-manager.io/docs/concepts/ca-injector/#injecting-ca-data-from-a-certificate-resource) for more information. |
| tap.injectCaFromSecret | string | `""` | Inject the CA bundle from a Secret. If set, the `cert-manager.io/inject-ca-from-secret` annotation will be added to the webhook. The Secret must have the CA Bundle stored in the `ca.crt` key and have the `cert-manager.io/allow-direct-injection` annotation set to `true`. See the cert-manager [CA Injector Docs](https://cert-manager.io/docs/concepts/ca-injector/#injecting-ca-data-from-a-secret-resource) for more information. |
| tap.keyPEM | string | `""` | Certificate key for Tap component. If not provided and not using an external secret then Helm will generate one. |
| tap.logFormat | string | defaultLogFormat | log format of the tap component |
| tap.logLevel | string | defaultLogLevel | log level of the tap component |
| tap.proxy | string | `nil` |  |
| tap.replicas | int | `1` | Number of tap component replicas |
| tap.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the tap container can use |
| tap.resources.cpu.request | string | `nil` | Amount of CPU units that the tap container requests |
| tap.resources.ephemeral-storage.limit | string | `""` | Maximum amount of ephemeral storage that the tap container can use |
| tap.resources.ephemeral-storage.request | string | `""` | Amount of ephemeral storage that the tap container requests |
| tap.resources.memory.limit | string | `nil` | Maximum amount of memory that tap container can use |
| tap.resources.memory.request | string | `nil` | Amount of memory that the tap container requests |
| tapInjector.UID | string | `nil` | UID for the tapInjector resource |
| tapInjector.caBundle | string | `""` | Bundle of CA certificates for the tapInjector. If not provided nor injected with cert-manager, then Helm will use the certificate generated for `tapInjector.crtPEM`. If `tapInjector.externalSecret` is set to true, this value, injectCaFrom, or injectCaFromSecret must be set, as no certificate will be generated. See the cert-manager [CA Injector Docs](https://cert-manager.io/docs/concepts/ca-injector) for more information. |
| tapInjector.crtPEM | string | `""` | Certificate for the tapInjector. If not provided and not using an external secret then Helm will generate one. |
| tapInjector.externalSecret | bool | `false` | Do not create a secret resource for the tapInjector webhook. If this is set to `true`, the value `tapInjector.caBundle` must be set or the ca bundle must injected with cert-manager ca injector using `tapInjector.injectCaFrom` or `tapInjector.injectCaFromSecret` (see below). |
| tapInjector.failurePolicy | string | `"Ignore"` |  |
| tapInjector.image.name | string | `"tap"` | Docker image name for the tapInjector instance |
| tapInjector.image.pullPolicy | string | defaultImagePullPolicy | Pull policy for the tapInjector component |
| tapInjector.image.registry | string | defaultRegistry | Docker registry for the tapInjector instance |
| tapInjector.image.tag | string | linkerdVersion | Docker image tag for the tapInjector instance |
| tapInjector.injectCaFrom | string | `""` | Inject the CA bundle from a cert-manager Certificate. See the cert-manager [CA Injector Docs](https://cert-manager.io/docs/concepts/ca-injector/#injecting-ca-data-from-a-certificate-resource) for more information. |
| tapInjector.injectCaFromSecret | string | `""` | Inject the CA bundle from a Secret. If set, the `cert-manager.io/inject-ca-from-secret` annotation will be added to the webhook. The Secret must have the CA Bundle stored in the `ca.crt` key and have the `cert-manager.io/allow-direct-injection` annotation set to `true`. See the cert-manager [CA Injector Docs](https://cert-manager.io/docs/concepts/ca-injector/#injecting-ca-data-from-a-secret-resource) for more information. |
| tapInjector.keyPEM | string | `""` | Certificate key for the tapInjector. If not provided and not using an external secret then Helm will generate one. |
| tapInjector.logFormat | string | defaultLogFormat | log format of the tapInjector component |
| tapInjector.logLevel | string | defaultLogLevel | log level of the tapInjector |
| tapInjector.namespaceSelector | string | `nil` |  |
| tapInjector.objectSelector | string | `nil` |  |
| tapInjector.proxy | string | `nil` |  |
| tapInjector.replicas | int | `1` | Number of replicas of tapInjector |
| tapInjector.resources.cpu.limit | string | `nil` | Maximum amount of CPU units that the tapInjector container can use |
| tapInjector.resources.cpu.request | string | `nil` | Amount of CPU units that the tapInjector container requests |
| tapInjector.resources.ephemeral-storage.limit | string | `""` | Maximum amount of ephemeral storage that the tapInjector container can use |
| tapInjector.resources.ephemeral-storage.request | string | `""` | Amount of ephemeral storage that the tapInjector container requests |
| tapInjector.resources.memory.limit | string | `nil` | Maximum amount of memory that tapInjector container can use |
| tapInjector.resources.memory.request | string | `nil` | Amount of memory that the tapInjector container requests |
| tolerations | string | `nil` | Default tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.11.0](https://github.com/norwoodj/helm-docs/releases/v1.11.0)
