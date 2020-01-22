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

The following table lists the configurable parameters of the Linkerd2 chart and their default values.

| Parameter                             | Description                                                                                                                                                                           | Default                              |
|:--------------------------------------|:--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:-------------------------------------|
| `controllerImage`                     | Docker image for the controller, tap and identity components                                                                                                                          | `gcr.io/linkerd-io/controller`       |
| `controllerLogLevel`                  | Log level for the control plane components                                                                                                                                            | `info`                               |
| `controllerReplicas`                  | Number of replicas for each control plane pod                                                                                                                                         | `1`                                  |
| `controllerUID`                       | User ID for the control plane components                                                                                                                                              | `2103`                               |
| `dashboard.replicas`                  | Number of replicas of dashboard                                                                                                                                                       | `1`                                  |
| `debugContainer.image.name`           | Docker image for the debug container                                                                                                                                                  | `gcr.io/linkerd-io/debug`            |
| `debugContainer.image.pullPolicy`     | Pull policy for the debug container Docker image                                                                                                                                      | `IfNotPresent`                       |
| `debugContainer.image.version`        | Tag for the debug container Docker image                                                                                                                                              | latest version                       |
| `disableHeartBeat`                    | Set to true to not start the heartbeat cronjob                                                                                                                                        | `false`                              |
| `enableH2Upgrade`                     | Allow proxies to perform transparent HTTP/2 upgrading                                                                                                                                 | `true`                               |
| `global.clusterDomain`                       | Kubernetes DNS Domain name to use                                                                                                                                                     | `cluster.local`                      |
| `global.cniEnabled`                          | Omit the NET_ADMIN capability in the PSP and the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed                               | `false`                      |
| `global.controllerComponentLabel`            | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/control-plane-component` |
| `global.controllerNamespaceLabel`            | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/control-plane-component` |
| `global.createdByAnnotation`                 | Annotation label for the proxy create. Do not edit.                                                                                                                                   | `linkerd.io/created-by`              |
| `global.identityTrustAnchorsPEM`            | Trust root certificate (ECDSA). It must be provided during install.                                                                                                                                                         ||
| `global.identityTrustDomain`                | Trust domain used for identity                                                                                                                                                        | `cluster.local`                      |
| `global.imagePullPolicy`                     | Docker image pull policy                                                                                                                                                              | `IfNotPresent`                       |
| `global.linkerdNamespaceLabel`               | Control plane label. Do not edit                                                                                                                                                      | `linkerd.io/control-plane-component` |
| `global.linkerdVersion`                      | Control plane version                                                                                                                                                                 | latest version                       |
| `global.namespace`                           | Control plane namespace                                                                                                                                                               | `linkerd`                            |
| `global.proxy.enableExternalProfiles`        | Enable service profiles for non-Kubernetes services                                                                                                                                   | `false`                              |
| `global.proxy.image.name`                    | Docker image for the proxy                                                                                                                                                            | `gcr.io/linkerd-io/proxy`            |
| `global.proxy.image.pullPolicy`              | Pull policy for the proxy container Docker image                                                                                                                                      | `IfNotPresent`                       |
| `global.proxy.image.version`                 | Tag for the proxy container Docker image                                                                                                                                              | latest version                       |
| `global.proxy.logLevel`                      | Log level for the proxy                                                                                                                                                               | `warn,linkerd=info`           |
| `global.proxy.ports.admin`                   | Admin port for the proxy container                                                                                                                                                    | `4191`                               |
| `global.proxy.ports.control`                 | Control port for the proxy container                                                                                                                                                  | `4190`                               |
| `global.proxy.ports.inbound`                 | Inbound port for the proxy container                                                                                                                                                  | `4143`                               |
| `global.proxy.ports.outbound`                | Outbound port for the proxy container                                                                                                                                                 | `4140`                               |
| `global.proxy.resources.cpu.limit`           | Maximum amount of CPU units that the proxy can use                                                                                                                                                                          ||
| `global.proxy.resources.cpu.request`         | Amount of CPU units that the proxy requests                                                                                                                                                                                 ||
| `global.proxy.resources.memory.limit`        | Maximum amount of memory that the proxy can use                                                                                                                                                                             ||
| `global.proxy.resources.memory.request`      | Amount of memory that the proxy requests                                                                                                                                                                                    ||
| `global.proxy.trace.collectorSvcAccount`     | Service account associated with the Trace collector instance                                                                                                                                                                ||
| `global.proxy.trace.collectorSvcAddr`        | Collector Service address for the proxies to send Trace Data                                                                                                                                                                ||
| `global.proxy.uid`                           | User id under which the proxy runs                                                                                                                                                    | `2102`                               |
| `global.proxy.waitBeforeExitSeconds`         | The proxy sidecar will stay alive for at least the given period before receiving SIGTERM signal from Kubernetes but no longer than pod's `terminationGracePeriodSeconds`.             | `0`                                  |
| `global.proxyInit.ignoreInboundPorts`        | Inbound ports the proxy should ignore                                                                                                                                                                                       ||
| `global.proxyInit.ignoreOutboundPorts`       | Outbound ports the proxy should ignore                                                                                                                                                                                      ||
| `global.proxyInit.image.name`                | Docker image for the proxy-init container                                                                                                                                             | `gcr.io/linkerd-io/proxy-init`       |
| `global.proxyInit.image.pullPolicy`          | Pull policy for the proxy-init container Docker image                                                                                                                                 | `IfNotPresent`                       |
| `global.proxyInit.image.version`             | Tag for the proxy-init container Docker image                                                                                                                                         | latest version                       |
| `global.proxyInit.resources.cpu.limit`       | Maximum amount of CPU units that the proxy-init container can use                                                                                                                     | `100m`                               |
| `global.proxyInit.resources.cpu.request`     | Amount of CPU units that the proxy-init container requests                                                                                                                            | `10m`                                |
| `global.ProxyInit.resources.memory.limit`    | Maximum amount of memory that the proxy-init container can use                                                                                                                        | `50Mi`                               |
| `global.proxyInit.resources.memory.request`  | Amount of memory that the proxy-init container requests                                                                                                                               | `10Mi`                               |
| `global.proxyInjectAnnotation`               | Annotation label to signal injection. Do not edit.                                                                                                                                                                          ||
| `global.proxyInjectDisabled`                 | Annotation value to disable injection. Do not edit.                                                                                                                                   | `disabled`                           |
| `grafanaImage`                        | Docker image for the Grafana container                                                                                                                                                | `gcr.io/linkerd-io/grafana`          |
| `heartbeatSchedule`                   | Config for the heartbeat cronjob                                                                                                                                                      | `0 0 * * *`                          |
| `identity.issuer.clockSkewAllowance`  | Amount of time to allow for clock skew within a Linkerd cluster                                                                                                                       | `20s`                                |
| `identity.issuer.crtExpiry`           | Expiration timestamp for the issuer certificate. It must be provided during install                                                                                                                                         ||
| `identity.issuer.crtExpiryAnnotation` | Annotation used to identity the issuer certificate expiration timestamp. Do not edit.                                                                                                 | `linkerd.io/identity-issuer-expiry`  |
| `identity.issuer.issuanceLifetime`    | Amount of time for which the Identity issuer should certify identity                                                                                                                  | `86400s`                             |
| `identity.issuer.scheme`              | Which scheme is used for the identity issuer secret format                                                                                                                            | `linkerd.io/tls`                     |
| `identity.issuer.tls.crtPEM`          | Issuer certificate (ECDSA). It must be provided during install.                                                                                                                                                             ||
| `identity.issuer.tls.keyPEM`          | Key for the issuer certificate (ECDSA). It must be provided during install.                                                                                                                                                 ||
| `installNamespace`                    | Set to false when installing Linkerd in a custom namespace. See the [Linkerd documentation](https://linkerd.io/2/tasks/install-helm/#customizing-the-namespace) for more information. | `true`                               |
| `omitWebhookSideEffects`              | Omit the `sideEffects` flag in the webhook manifests                                                                                                                                  | `false`                              |
| `prometheusImage`                     | Docker image for the Prometheus container                                                                                                                                             | `prom/prometheus:v2.15.2`            |
| `prometheusLogLevel`                  | Log level for Prometheus                                                                                                                                                              | `info`                               |
| `proxyInjector.crtPEM`                | Certificate for the proxy injector. If not provided then Helm will generate one.                                                                                                                                            ||
| `proxyInjector.keyPEM`                | Certificate key for the proxy injector. If not provided then Helm will generate one.                                                                                                                                        ||
| `profileValidator.crtPEM`             | Certificate for the service profile validator. If not provided then Helm will generate one.                                                                                                                                 ||
| `profileValidator.keyPEM`             | Certificate key for the service profile validator. If not provided then Helm will generate one.                                                                                                                             ||
| `tap.crtPEM`                          | Certificate for the Tap component. If not provided then Helm will generate one.                                                                                                                                             ||
| `tap.keyPEM`                          | Certificate key for Tap component. If not provided then Helm will generate one.                                                                                                                                             ||
| `webhookFailurePolicy`                | Failure policy for the proxy injector                                                                                                                                                 | `Ignore`                             |
| `webImage`                            | Docker image for the web container                                                                                                                                                    | `gcr.io/linkerd-io/web`              |

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
