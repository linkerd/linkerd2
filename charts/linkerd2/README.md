# Linkerd2 Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes.

Linkerd is a Cloud Native Computing Foundation ([CNCF][cncf]) project.

## Quickstart and documentation

You can run Linkerd on any Kubernetes 1.12+ cluster in a matter of seconds. See
the [Linkerd Getting Started Guide][getting-started] for how.

For more comprehensive documentation, start with the [Linkerd
docs][linkerd-docs].

## Prerequisite: identity certificates

The identity component of Linkerd requires setting up a trust anchor
certificate, and an issuer certificate with its key. These need to be provided
to Helm by the user (unlike when using the `linkerd install` CLI which can
generate these automatically). You can provide your own, or follow [these
instructions](https://linkerd.io/2/tasks/generate-certificates/) to generate new ones.

Note that the provided certificates must be ECDSA certficates.

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
  --set-file identity.trustAnchorsPEM=ca.crt \
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
  --set-file identity.trustAnchorsPEM=ca.crt \
  --set-file identity.issuer.tls.crtPEM=issuer.crt \
  --set-file identity.issuer.tls.keyPEM=issuer.key \
  --set identity.issuer.crtExpiry=$(date -d '+8760 hour' +"%Y-%m-%dT%H:%M:%SZ") \
  -f linkerd2/values-ha.yaml
  linkerd/linkerd2
```

## Configuration

The following table lists the configurable parameters of the Linkerd2 chart and their default values.

| Parameter                             | Description                                                                                                                                                                           | Default                              |
|:--------------------------------------|:--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:-------------------------------------|
| `clusterDomain`                       | Kubernetes DNS Domain name to use                                                                                                                                                     | `cluster.local`                      |
| `enableH2Upgrade`                     | Allow proxies to perform transparent HTTP/2 upgrading                                                                                                                                 | `true`                               |
| `imagePullPolicy`                     | Docker image pull policy                                                                                                                                                              | `IfNotPresent`                       |
| `linkerdVersion`                      | Control plane version                                                                                                                                                                 | `stable-2.5.0`                       |
| `namespace`                           | Control plane namespace                                                                                                                                                               | `linkerd`                            |
| `omitWebhookSideEffects`              | Omit the `sideEffects` flag in the webhook manifests                                                                                                                                  | `false`                              |
| `webhookFailurePolicy`                | Failure policy for the proxy injector                                                                                                                                                 | `Ignore`                             |
| `controllerImage`                     | Docker image for the controller, tap and identity components                                                                                                                          | `gcr.io/linkerd-io/controller`       |
| `controllerLogLevel`                  | Log level for the control plane components                                                                                                                                            | `info`                               |
| `controllerReplicas`                  | Number of replicas for each control plane pod                                                                                                                                         | `1`                                  |
| `controllerUID`                       | User ID for the control plane components                                                                                                                                              | `2103`                               |
| `identity.issuer.clockSkewAllowance`  | Amount of time to allow for clock skew within a Linkerd cluster                                                                                                                       | `20s`                                |
| `dashboard.replicas`                  | Number of replicas of dashboard                                                                                                                                                       | `1`                                  |
| `identity.issuer.scheme`              | Which scheme is used for the identity issuer secret format                                                                                                                            | `linkerd.io/tls`                     |
| `identity.issuer.crtExpiry`           | Expiration timestamp for the issuer certificate. It must be provided during install                                                                                                                                         ||
| `identity.issuer.crtExpiryAnnotation` | Annotation used to identity the issuer certificate expiration timestamp. Do not edit.                                                                                                 | `linkerd.io/identity-issuer-expiry`  |
| `identity.issuer.issuanceLifeTime`    | Amount of time for which the Identity issuer should certify identity                                                                                                                  | `86400s`                             |
| `identity.issuer.tls.crtPEM`          | Issuer certificate (ECDSA). It must be provided during install.                                                                                                                                                             ||
| `identity.issuer.tls.keyPEM`          | Key for the issuer certificate (ECDSA). It must be provided during install.                                                                                                                                                 ||
| `identity.trustAnchorsPEM`            | Trust root certificate (ECDSA). It must be provided during install.                                                                                                                                                         ||
| `identity.trustDomain`                | Trust domain used for identity                                                                                                                                                        | `cluster.local`                      |
| `GatewayAnnotator.crtPEM`             | Certificate for the gateway annotator. If not provided then Helm will generate one.                                                                                                                                         ||
| `GatewayAnnotator.keyPEM`             | Certificate key for the gateway annotator. If not provided then Helm will generate one.                                                                                                                                     ||
| `grafanaImage`                        | Docker image for the Grafana container                                                                                                                                                | `gcr.io/linkerd-io/grafana`          |
| `disableHeartBeat`                    | Set to true to not start the heartbeat cronjob                                                                                                                                        | `false`                              |
| `heartbeatSchedule`                   | Config for the heartbeat cronjob                                                                                                                                                      | `0 0 * * *`                          |
| `prometheusImage`                     | Docker image for the Prometheus container                                                                                                                                             | `prom/prometheus:v2.11.1`            |
| `prometheusLogLevel`                  | Log level for Prometheus                                                                                                                                                              | `info`                               |
| `proxy.enableExternalProfiles`        | Enable service profiles for non-Kubernetes services                                                                                                                                   | `false`                              |
| `proxy.image.name`                    | Docker image for the proxy                                                                                                                                                            | `gcr.io/linkerd-io/proxy`            |
| `proxy.image.pullPolicy`              | Pull policy for the proxy container Docker image                                                                                                                                      | `IfNotPresent`                       |
| `proxy.image.version`                 | Tag for the proxy container Docker image                                                                                                                                              | `stable-2.5.0`                       |
| `proxy.logLevel`                      | Log level for the proxy                                                                                                                                                               | `warn,linkerd2_proxy=info`           |
| `proxy.ports.admin`                   | Admin port for the proxy container                                                                                                                                                    | `4191`                               |
| `proxy.ports.control`                 | Control port for the proxy container                                                                                                                                                  | `4190`                               |
| `proxy.ports.inbound`                 | Inbound port for the proxy container                                                                                                                                                  | `4143`                               |
| `proxy.ports.outbound`                | Outbound port for the proxy container                                                                                                                                                 | `4140`                               |
| `proxy.resources.cpu.limit`           | Maximum amount of CPU units that the proxy can use                                                                                                                                                                          ||
| `proxy.resources.cpu.request`         | Amount of CPU units that the proxy requests                                                                                                                                                                                 ||
| `proxy.resources.memory.limit`        | Maximum amount of memory that the proxy can use                                                                                                                                                                             ||
| `proxy.resources.memory.request`      | Amount of memory that the proxy requests                                                                                                                                                                                    ||
| `proxy.trace.collectorSvcAccount`     | Service account associated with the Trace collector instance                                                                                                                                                                ||
| `proxy.trace.collectorSvcAddr`        | Collector Service address for the proxies to send Trace Data                                                                                                                                                                ||
| `proxy.uid`                           | User id under which the proxy runs                                                                                                                                                    | `2102`                               |
| `proxyInit.ignoreInboundPorts`        | Inbound ports the proxy should ignore                                                                                                                                                                                       ||
| `proxyInit.ignoreOutboundPorts`       | Outbound ports the proxy should ignore                                                                                                                                                                                      ||
| `proxyInit.image.name`                | Docker image for the proxy-init container                                                                                                                                             | `gcr.io/linkerd-io/proxy-init`       |
| `proxyInit.image.pullPolicy`          | Pull policy for the proxy-init container Docker image                                                                                                                                 | `IfNotPresent`                       |
| `proxyInit.image.version`             | Tag for the proxy-init container Docker image                                                                                                                                         | `v1.1.0`                             |
| `proxyInit.resources.cpu.limit`       | Maximum amount of CPU units that the proxy-init container can use                                                                                                                     | `100m`                               |
| `proxyInit.resources.cpu.request`     | Amount of CPU units that the proxy-init container requests                                                                                                                            | `10m`                                |
| `ProxyInit.resources.memory.limit`    | Maximum amount of memory that the proxy-init container can use                                                                                                                        | `50Mi`                               |
| `proxyInit.resources.memory.request`  | Amount of memory that the proxy-init container requests                                                                                                                               | `10Mi`                               |
| `proxyInjector.crtPEM`                | Certificate for the proxy injector. If not provided then Helm will generate one.                                                                                                                                            ||
| `proxyInjector.keyPEM`                | Certificate key for the proxy injector. If not provided then Helm will generate one.                                                                                                                                        ||
| `profileValidator.crtPEM`             | Certificate for the service profile validator. If not provided then Helm will generate one.                                                                                                                                 ||
| `profileValidator.keyPEM`             | Certificate key for the service profile validator. If not provided then Helm will generate one.                                                                                                                             ||
| `tap.crtPEM`                          | Certificate for the Tap component. If not provided then Helm will generate one.                                                                                                                                             ||
| `tap.keyPEM`                          | Certificate key for Tap component. If not provided then Helm will generate one.                                                                                                                                             ||
| `webImage`                            | Docker image for the web container                                                                                                                                                    | `gcr.io/linkerd-io/web`              |
| `createdByAnnotation`                 | Annotation label for the proxy create. Do not edit.                                                                                                                                   | `linkerd.io/created-by`              |
| `proxyInjectAnnotation`               | Annotation label to signal injection. Do not edit.                                                                                                                                                                          ||
| `proxyInjectDisabled`                 | Annotation value to disable injection. Do not edit.                                                                                                                                   | `disabled`                           |
| `controllerComponentLabel`            | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/control-plane-component` |
| `controllerNamespaceLabel`            | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/control-plane-component` |
| `linkerdNamespaceLabel`               | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/control-plane-component` |
| `installNamespace`                    | Set to false when installing Linkerd in a custom namespace. See the [Linkerd documentation](https://linkerd.io/2/tasks/install-helm/#customizing-the-namespace) for more information. | `true`                               |

## Get involved

* Check out Linkerd's source code at [Github][linkerd2].
* Join Linkerd's [user mailing list][linkerd-users],
[developer mailing list][linkerd-dev], and [announcements mailing list][linkerd-announce].
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
