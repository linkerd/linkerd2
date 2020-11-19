# Linkerd2 Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes.

Linkerd is a Cloud Native Computing Foundation ([CNCF][cncf]) project.

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

## Configuration

The following table lists the configurable parameters of the Linkerd2 chart and
their default values.

| Parameter                                   | Description                                                                                                                                                                           | Default                              |
|:--------------------------------------------|:--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:-------------------------------------|
| `controllerImage`                           | Docker image for the controller, tap and identity components                                                                                                                          | `ghcr.io/linkerd/controller`       |
| `controllerReplicas`                        | Number of replicas for each control plane pod                                                                                                                                         | `1`                                  |
| `controllerUID`                             | User ID for the control plane components                                                                                                                                              | `2103`                               |
| `dashboard.replicas`                        | Number of replicas of dashboard                                                                                                                                                       | `1`                                  |
| `debugContainer.image.name`                 | Docker image for the debug container                                                                                                                                                  | `ghcr.io/linkerd/debug`            |
| `debugContainer.image.pullPolicy`           | Pull policy for the debug container Docker image                                                                                                                                      | `IfNotPresent`                       |
| `debugContainer.image.version`              | Tag for the debug container Docker image                                                                                                                                              | latest version                       |
| `destinationResources`                      | CPU and Memory resources required by destination (see `global.proxy.resources` for sub-fields)             |   |
| `destinationProxyResources`                 | CPU and Memory resources required by proxy injected into destination pod (see `global.proxy.resources` for sub-fields)             | values in `global.proxy.resources`   |
| `disableHeartBeat`                          | Set to true to not start the heartbeat cronjob                                                                                                                                        | `false`                              |
| `enableH2Upgrade`                           | Allow proxies to perform transparent HTTP/2 upgrading                                                                                                                                 | `true`                               |
| `global.clusterDomain`                      | Kubernetes DNS Domain name to use                                                                                                                                                     | `cluster.local`                      |
| `global.clusterNetworks`                    | The networks that may include pods & services in this cluscter                                                                                                                         | `10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16` |
| `global.cniEnabled`                         | Omit the NET_ADMIN capability in the PSP and the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed                               | `false`                              |
| `global.controllerComponentLabel`           | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/control-plane-component` |
| `global.controllerImageVersion`             | Tag for the controller container docker image                                                                                                                                         | latest version                       |
| `global.controllerLogLevel`                 | Log level for the control plane components                                                                                                                                            | `info`                               |
| `global.controllerNamespaceLabel`           | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/control-plane-ns`        |
| `global.grafanaUrl`                          | URL of external grafana instance configured with reverse proxy, used by the dashboard                                                                                                                                      |                                 |
| `global.podLabels`                          | Additional labels to add to all pods                                                                                                                                                  | `{}`                                 |
| `global.podAnnotations`                     | Additional annotations to add to all pods                                                                                                                                             | `{}`                                 |
| `global.createdByAnnotation`                | Annotation label for the proxy create. Do not edit.                                                                                                                                   | `linkerd.io/created-by`              |
| `global.identityTrustAnchorsPEM`            | Trust root certificate (ECDSA). It must be provided during install.                                                                                                                   |                                      |
| `global.identityTrustDomain`                | Trust domain used for identity                                                                                                                                                        | `cluster.local`                      |
| `global.imagePullPolicy`                    | Docker image pull policy                                                                                                                                                              | `IfNotPresent`                       |
| `global.linkerdNamespaceLabel`              | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/is-control-plane` |
| `global.linkerdVersion`                     | Control plane version                                                                                                                                                                 | latest version                       |
| `global.namespace`                          | Control plane namespace                                                                                                                                                               | `linkerd`                            |
| `global.prometheusUrl`     | URL of external prometheus instance to perform queries, used by the `public-api`                                                                                                                    |                               |
| `global.proxy.cores`                        | The number of proxy threads to be allocated for each proxy. Must be a whole number, and should be kept in sync with `global.proxy.resources.cpu.limit`, if set.                       |                                      |
| `global.proxy.enableExternalProfiles`       | Enable service profiles for non-Kubernetes services                                                                                                                                   | `false`                              |
| `global.proxy.image.name`                   | Docker image for the proxy                                                                                                                                                            | `ghcr.io/linkerd/proxy`            |
| `global.proxy.image.pullPolicy`             | Pull policy for the proxy container Docker image                                                                                                                                      | `IfNotPresent`                       |
| `global.proxy.image.version`                | Tag for the proxy container Docker image                                                                                                                                              | latest version                       |
| `global.proxy.logLevel`                     | Log level for the proxy                                                                                                                                                               | `warn,linkerd=info`                  |
| `global.proxy.logFormat`                     | Log format (`plain` or `json`) for the proxy                                                                                                                                                               | `plain`                  |
| `global.proxy.ports.admin`                  | Admin port for the proxy container                                                                                                                                                    | `4191`                               |
| `global.proxy.ports.control`                | Control port for the proxy container                                                                                                                                                  | `4190`                               |
| `global.proxy.ports.inbound`                | Inbound port for the proxy container                                                                                                                                                  | `4143`                               |
| `global.proxy.ports.outbound`               | Outbound port for the proxy container                                                                                                                                                 | `4140`                               |
| `global.proxy.resources.cpu.limit`          | Maximum amount of CPU units that the proxy can use                                                                                                                                    |                                      |
| `global.proxy.resources.cpu.request`        | Amount of CPU units that the proxy requests                                                                                                                                           |                                      |
| `global.proxy.resources.memory.limit`       | Maximum amount of memory that the proxy can use                                                                                                                                       |                                      |
| `global.proxy.resources.memory.request`     | Amount of memory that the proxy requests                                                                                                                                              |                                      |
| `global.proxy.trace.collectorSvcAccount`    | Service account associated with the Trace collector instance                                                                                                                          | `default`                            |
| `global.proxy.trace.collectorSvcAddr`       | Collector Service address for the proxies to send Trace Data                                                                                                                          |                                      |
| `global.proxy.uid`                          | User id under which the proxy runs                                                                                                                                                    | `2102`                               |
| `global.proxy.waitBeforeExitSeconds`        | The proxy sidecar will stay alive for at least the given period before receiving SIGTERM signal from Kubernetes but no longer than pod's `terminationGracePeriodSeconds`.             | `0`                                  |
| `global.proxy.outboundConnectTimeout`       | Maximum time allowed for the proxy to establish an outbound TCP connection                                                                                                            | `1000ms`                             |
| `global.proxy.inboundConnectTimeout`        | Maximum time allowed for the proxy to establish an inbound TCP connection                                                                                                             | `100ms`                              |
| `global.proxyInit.ignoreInboundPorts`       | Inbound ports the proxy should ignore                                                                                                                                                 | `25,443,587,3306,11211`              |
| `global.proxyInit.ignoreOutboundPorts`      | Outbound ports the proxy should ignore                                                                                                                                                | `25,443,587,3306,11211`              |
| `global.proxyInit.image.name`               | Docker image for the proxy-init container                                                                                                                                             | `ghcr.io/linkerd/proxy-init`       |
| `global.proxyInit.image.pullPolicy`         | Pull policy for the proxy-init container Docker image                                                                                                                                 | `IfNotPresent`                       |
| `global.proxyInit.image.version`            | Tag for the proxy-init container Docker image                                                                                                                                         | latest version                       |
| `global.proxyInit.resources.cpu.limit`      | Maximum amount of CPU units that the proxy-init container can use                                                                                                                     | `100m`                               |
| `global.proxyInit.resources.cpu.request`    | Amount of CPU units that the proxy-init container requests                                                                                                                            | `10m`                                |
| `global.ProxyInit.resources.memory.limit`   | Maximum amount of memory that the proxy-init container can use                                                                                                                        | `50Mi`                               |
| `global.proxyInit.resources.memory.request` | Amount of memory that the proxy-init container requests                                                                                                                               | `10Mi`                               |
| `global.proxyInjectAnnotation`              | Annotation label to signal injection. Do not edit.                                                                                                                                    | `linkerd.io/inject`                                    |
| `global.proxyInjectDisabled`                | Annotation value to disable injection. Do not edit.                                                                                                                                   | `disabled`                           |
| `heartbeatSchedule`                         | Config for the heartbeat cronjob                                                                                                                                                      | `0 0 * * *`                          |
| `identity.issuer.clockSkewAllowance`        | Amount of time to allow for clock skew within a Linkerd cluster                                                                                                                       | `20s`                                |
| `identity.issuer.crtExpiry`                 | Expiration timestamp for the issuer certificate. It must be provided during install                                                                                                   |                                      |
| `identity.issuer.crtExpiryAnnotation`       | Annotation used to identity the issuer certificate expiration timestamp. Do not edit.                                                                                                 | `linkerd.io/identity-issuer-expiry`  |
| `identity.issuer.issuanceLifetime`          | Amount of time for which the Identity issuer should certify identity                                                                                                                  | `24h0m0s`                             |
| `identity.issuer.scheme`                    | Which scheme is used for the identity issuer secret format                                                                                                                            | `linkerd.io/tls`                     |
| `identity.issuer.tls.crtPEM`                | Issuer certificate (ECDSA). It must be provided during install.                                                                                                                       |                                      |
| `identity.issuer.tls.keyPEM`                | Key for the issuer certificate (ECDSA). It must be provided during install.                                                                                                           |                                      |
| `identityResources`                         | CPU and Memory resources required by the identity controller (see `global.proxy.resources` for sub-fields)             |   |
| `identityProxyResources`                    | CPU and Memory resources required by proxy injected into identity pod (see `global.proxy.resources` for sub-fields)             | values in `global.proxy.resources`   |
| `installNamespace`                          | Set to false when installing Linkerd in a custom namespace. See the [Linkerd documentation](https://linkerd.io/2/tasks/install-helm/#customizing-the-namespace) for more information. | `true`                               |
| `omitWebhookSideEffects`                    | Omit the `sideEffects` flag in the webhook manifests                                                                                                                                  | `false`                              |
| `proxyInjector.externalSecret`              | Do not create a secret resource for the profileValidator webhook. If this is set to `true`, the value `proxyInjector.caBundle` must be set (see below).                               | `false`                              |
| `proxyInjector.namespaceSelector`           | Namespace selector used by admission webhook. If not set defaults to all namespaces without the annotation `config.linkerd.io/admission-webhooks=disabled`                            |                                      |
| `proxyInjector.crtPEM`                      | Certificate for the proxy injector. If not provided then Helm will generate one.                                                                                                      |                                      |
| `proxyInjector.keyPEM`                      | Certificate key for the proxy injector. If not provided then Helm will generate one.                                                                                                      |                                      |
| `proxyInjector.caBundle`                    | Bundle of CA certificates for proxy injector. If not provided then Helm will use the certificate generated  for `proxyInjector.crtPEM`. If `proxyInjector.externalSecret` is set to true, this value must be set, as no certificate will be generated.        |   |
| `proxyInjectorResources`                    | CPU and Memory resources required by the proxy injector (see `global.proxy.resources` for sub-fields)             |   |
| `proxyInjectorProxyResources`               | CPU and Memory resources required by proxy injected into the proxy injector pod (see `global.proxy.resources` for sub-fields)             | values in `global.proxy.resources`   |
| `profileValidator.externalSecret`           | Do not create a secret resource for the profileValidator webhook. If this is set to `true`, the value `profileValidator.caBundle` must be set (see below).                            | false                                |
| `profileValidator.namespaceSelector`        | Namespace selector used by admission webhook. If not set defaults to all namespaces without the annotation `config.linkerd.io/admission-webhooks=disabled`                            |                                      |
| `profileValidator.crtPEM`                   | Certificate for the service profile validator. If not provided then Helm will generate one.                                                                                           |                                      |
| `profileValidator.keyPEM`                   | Certificate key for the service profile validator. If not provided then Helm will generate one.                                                                                       |                                      |
| `profileValidator.caBundle`                 | Bundle of CA certificates for service profile validator. If not provided then Helm will use the certificate generated  for `profileValidator.crtPEM`. If `profileValidator.externalSecret` is set to true, this value must be set, as no certificate will be generated.         |  |
| `publicAPIResources`                        | CPU and Memory resources required by controllers publicAPI (see `global.proxy.resources` for sub-fields)             |   |
| `publicAPIProxyResources`                   | CPU and Memory resources required by proxy injected into controllers public API pod (see `global.proxy.resources` for sub-fields)             |  values  `global.proxy.resources`   |
| `spValidatorResources`                      | CPU and Memory resources required by the SP validator (see `global.proxy.resources` for sub-fields)             |   |
| `spValidatorProxyResources`                 | CPU and Memory resources required by proxy injected into the SP validator pod (see `global.proxy.resources` for sub-fields)             | values in `global.proxy.resources`   |
| `tap.externalSecret`                        | Do not create a secret resource for the Tap component. If this is set to `true`, the value `tap.caBundle` must be set (see below).                                                  | false                                |
| `tap.crtPEM`                                | Certificate for the Tap component. If not provided then Helm will generate one.                                                                                                       |                                      |
| `tap.keyPEM`                                | Certificate key for Tap component. If not provided then Helm will generate one.                                                                                                       |                                      |
| `tap.caBundle`                              | Bundle of CA certificates for Tap component. If not provided then Helm will use the certificate generated  for `tap.crtPEM`. If `tap.externalSecret` is set to true, this value must be set, as no certificate will be generated.                       ||
| `tapResources`                              | CPU and Memory resources required by tap (see `global.proxy.resources` for sub-fields)             |   |
| `tapProxyResources`                         | CPU and Memory resources required by proxy injected into tap pod (see `global.proxy.resources` for sub-fields)             | values in `global.proxy.resources`   |
| `webhookFailurePolicy`                      | Failure policy for the proxy injector                                                                                                                                                 | `Ignore`                             |
| `webImage`                                  | Docker image for the web container                                                                                                                                                    | `ghcr.io/linkerd/web`              |
| `webResources`                              | CPU and Memory resources required by web UI (see `global.proxy.resources` for sub-fields)             |   |
| `webProxyResources`                         | CPU and Memory resources required by proxy injected into web UI pod (see `global.proxy.resources` for sub-fields)             | values in `global.proxy.resources`   |
| `enforcedHostRegexp`                        | Host header validation regex for the dashboard. See the [Linkerd documentation](https://linkerd.io/2/tasks/exposing-dashboard) for more information                                   | `""`                                 |
| `nodeSelector`                        | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information                                   | `beta.kubernetes.io/os: linux`                                 |
| `tolerations`                        | Tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information                                   |                                  |

## Add-Ons Configuration

### Grafana Add-On

The following table lists the configurable parameters for the Grafana Add-On.

| Parameter                             | Description                                                                                                                                                                           | Default                              |
|:--------------------------------------|:--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:-------------------------------------|
| `grafana.enabled`                     | Flag to enable grafana instance to be installed                                                                                                                                                | `true`
| `grafana.image.name`                | Docker image name for the grafana instance                                                                                                                                                 | `ghcr.io/linkerd/grafana`                             |
| `grafana.image.tag`                | Docker image tag for the grafana instance                                                                                                                                                 | latest version                              |
| `grafana.resources.cpu.limit`       | Maximum amount of CPU units that the grafana container can use                                                                                                                     ||
| `grafana.resources.cpu.request`     | Amount of CPU units that the grafana container requests                                                                                                                            ||
| `grafana.resources.memory.limit`    | Maximum amount of memory that grafana container can use                                                                                                                        ||
| `grafana.resources.memory.request`  | Amount of memory that the grafana container requests                                                                                                                               ||
| `grafana.proxy.resources`           | Structure analog to the `resources` fields above, but overriding the resources of the linkerd proxy injected into the grafana pod.   | values in `global.proxy.resources` of the linkerd2 chart. |

### Prometheus Add-On

The following table lists the configurable parameters for the Prometheus Add-On.

| Parameter                             | Description                                                                                                                                                                           | Default                              |
|:--------------------------------------|:--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:-------------------------------------|
| `prometheus.enabled`                     | Flag to enable prometheus instance to be installed                                                                                                                                                | `true`                             |
| `prometheus.alert_relabel_configs`                   | Alert relabeling is applied to alerts before they are sent to the Alertmanager.                                                                            | `[]`                                 |
| `prometheus.alertManagers`                   | Alertmanager instances the Prometheus server sends alerts to configured via the static_configs parameter.                                                                             | `[]`                                 |
| `prometheus.args`                       |  Command line options for Prometheus binary                                                                                                                                             | `storage.tsdb.path: /data, storage.tsdb.retention.time: 6h, config.file: /etc/prometheus/prometheus.yml, log.level: info`                                 |
| `prometheus.globalConfig`             | The global configuration specifies parameters that are valid in all other configuration contexts.                                                                 | `scrape_interval: 10s, scrape_timeout: 10s, evaluation_interval: 10s`                                 |
| `prometheus.image`                | Docker image for the prometheus instance                                                                                                                                                 | `prom/prometheus:v2.19.3`                             |
| `prometheus.proxy.resources`                  | CPU and Memory resources required by proxy injected into prometheus pod (see `global.proxy.resources` for sub-fields)             | values in `global.proxy.resources`   |
| `prometheus.persistence.storageClass`        | Storage class used to create prometheus data PV.                                                                                                                                                                                                                        | `nil`                                |
| `prometheus.persistence.accessMode`          | PVC access mode.                                                                                                                                                                                                                                                        | `ReadWriteOnce`                      |
| `prometheus.persistence.size`                | Prometheus data volume size.                                                                                                                                                                                                                                            | `8Gi`                                |
| `prometheus.remoteWrite`       | Allows transparently sending samples to an endpoint. Mostly used for long term storage.                  ||
| `prometheus.resources.cpu.limit`       | Maximum amount of CPU units that the prometheus container can use                                                                                                                     ||
| `prometheus.resources.cpu.request`     | Amount of CPU units that the prometheus container requests                                                                                                                            ||
| `prometheus.resources.memory.limit`    | Maximum amount of memory that prometheus container can use                                                                                                                        ||
| `prometheus.resources.memory.request`  | Amount of memory that the prometheus container requests                                                                                                                               ||
| `prometheus.ruleConfigMapMounts`             | Alerting/recording rule ConfigMap mounts (sub-path names must end in `_rules.yml` or `_rules.yaml`)                                                                                   | `[]`                                 |
| `prometheus.scrapeConfigs`             | A scrape_config section specifies a set of targets and parameters describing how to scrape them.                                                        | `[]`                                 |
| `prometheus.sidecarContainers`         | A sidecarContainers section specifies a list of secondary containers to run in the prometheus pod e.g. to export data to non-prometheus systems | `[]`                                 |

Most of the above configuration match directly with the official Prometheus
configuration which can be found [here](https://prometheus.io/docs/prometheus/latest/configuration/configuration)

### Tracing Add-On

The following table lists the configurable parameters for the Tracing Add-On.

| Parameter                                    | Description                                                            | Default                                |
|:---------------------------------------------|:-----------------------------------------------------------------------|:---------------------------------------|
| `tracing.enabled`                            | Flag to enable tracing components to be installed                      | `false`                                |
| `tracing.collector.image`                    | Docker image for the trace collector                                   | `omnition/opencensus-collector:0.1.10` |
| `tracing.collector.resources.cpu.limit`      | Maximum amount of CPU units that the trace collector container can use |                                 |
| `tracing.collector.resources.cpu.request`    | Amount of CPU units that the trace collector container requests        |                                  |
| `tracing.collector.resources.memory.limit`   | Maximum amount of memory that the trace collector container can use    |                                  |
| `tracing.collector.resources.memory.request` | Amount of memory that the trace collector container requests           |                                |
| `tracing.jaeger.image`                       | Docker image for the jaeger instance                                   | `jaegertracing/all-in-one:1.19.2`         |
| `tracing.jaeger.resources.cpu.limit`         | Maximum amount of CPU units that the jaeger container can use          |                                        |
| `tracing.jaeger.resources.cpu.request`       | Amount of CPU units that the jaeger container requests                 |                                        |
| `tracing.jaeger.resources.memory.limit`      | Maximum amount of memory that the jaeger container can use             |                                        |
| `tracing.jaeger.resources.memory.request`    | Amount of memory that the jaeger container requests                    |                                        |

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
