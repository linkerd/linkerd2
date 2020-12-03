# linkerd2

Linkerd gives you observability, reliability, and security
for your microservices — with no code change required.

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
| file://../add-ons/grafana | grafana | 0.1.0 |
| file://../add-ons/prometheus | prometheus | 0.1.0 |
| file://../partials | partials | 0.1.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controllerImage | string | `"ghcr.io/linkerd/controller"` | Docker image for the controller, tap and identity components |
| controllerReplicas | int | `1` | Number of replicas for each control plane pod |
| controllerUID | int | `2103` | User ID for the control plane components |
| dashboard.replicas | int | `1` | Number of replicas of dashboard |
| debugContainer.image.name | string | `"ghcr.io/linkerd/debug"` | Docker image for the debug container |
| debugContainer.image.pullPolicy | string | `"IfNotPresent"` | Pull policy for the debug container Docker image |
| debugContainer.image.version | string | `"linkerdVersionValue"` | Tag for the debug container Docker image |
| disableHeartBeat | bool | `false` | Set to true to not start the heartbeat cronjob  |
| enableH2Upgrade | bool | `true` | Allow proxies to perform transparent HTTP/2 upgrading   |
| enforcedHostRegexp | string | `""` | Host header validation regex for the dashboard. See the [Linkerd documentation](https://linkerd.io/2/tasks/exposing-dashboard) for more information |
| global.clusterDomain | string | `"cluster.local"` | Kubernetes DNS Domain name to use   |
| global.clusterNetworks | string | `"10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16"` | The cluster networks for which service discovery is performed. This should include the pod network but need not include the node network. By default, all private networks are specified so that resolution works in typical Kubernetes environments. |
| global.cniEnabled | bool | `false` | enabling this omits the NET_ADMIN capability in the PSP and the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed |
| global.controlPlaneTracing | bool | `false` | control plane trace configuration |
| global.controlPlaneTracingNamespace | string | `"linkerd-jaeger"` |  |
| global.controllerComponentLabel | string | `"linkerd.io/control-plane-component"` | Control plane label. Do not edit |
| global.controllerLogLevel | string | `"info"` | Log level for the control plane components |
| global.controllerNamespaceLabel | string | `"linkerd.io/control-plane-ns"` | Control plane label. Do not edit   |
| global.createdByAnnotation | string | `"linkerd.io/created-by"` | Annotation label for the proxy create. Do not edit.  |
| global.enableEndpointSlices | bool | `false` | enables the use of EndpointSlice informers for the destination service; enableEndpointSlices should be set to true only if EndpointSlice K8s feature gate is on; the feature is still experimental. |
| global.grafanaUrl | string | `""` | url of external grafana instance with reverse proxy configured. |
| global.identityTrustAnchorsPEM | string | `""` | Trust root certificate (ECDSA). It must be provided during install.  |
| global.identityTrustDomain | string | `"cluster.local"` | Trust domain used for identity  |
| global.imagePullPolicy | string | `"IfNotPresent"` | Docker image pull policy  |
| global.imagePullSecrets | list | `[]` | For Private docker registries, authentication is needed.  Registry secrets are applied to the respective service accounts |
| global.linkerdNamespaceLabel | string | `"linkerd.io/is-control-plane"` | Control plane label. Do not edit  |
| global.linkerdVersion | string | `"linkerdVersionValue"` | control plane version. See Proxy section for proxy version |
| global.namespace | string | `"linkerd"` | Control plane namespace |
| global.podAnnotations | object | `{}` | Additional annotations to add to all pods |
| global.podLabels | object | `{}` | Additional labels to add to all pods |
| global.prometheusUrl | string | `""` | url of existing prometheus |
| global.proxy.cores | int | `0` | The `cpu.limit` and `cores` should be kept in sync. The value of `cores` must be an integer and should typically be set by rounding up from the limit. E.g. if cpu.limit is '1500m', cores should be 2. |
| global.proxy.enableExternalProfiles | bool | `false` | Enable service profiles for non-Kubernetes services |
| global.proxy.image.name | string | `"ghcr.io/linkerd/proxy"` | Docker image for the proxy |
| global.proxy.image.pullPolicy | string | `"IfNotPresent"` | Pull policy for the proxy container Docker image |
| global.proxy.image.version | string | `"linkerdVersionValue"` | Tag for the proxy container Docker image |
| global.proxy.inboundConnectTimeout | string | `"100ms"` | Maximum time allowed for the proxy to establish an inbound TCP connection |
| global.proxy.logFormat | string | `"plain"` | Log format (`plain` or `json`) for the proxy |
| global.proxy.logLevel | string | `"warn,linkerd=info"` | Log level for the proxy |
| global.proxy.outboundConnectTimeout | string | `"1000ms"` | Maximum time allowed for the proxy to establish an outbound TCP connection |
| global.proxy.ports.admin | int | `4191` | Admin port for the proxy container |
| global.proxy.ports.control | int | `4190` | Control port for the proxy container |
| global.proxy.ports.inbound | int | `4143` | Inbound port for the proxy container |
| global.proxy.ports.outbound | int | `4140` | Outbound port for the proxy container   |
| global.proxy.requireIdentityOnInboundPorts | string | `""` |  |
| global.proxy.resources.cpu.limit | string | `""` | Maximum amount of CPU units that the proxy can use  |
| global.proxy.resources.cpu.request | string | `""` | Amount of CPU units that the proxy requests |
| global.proxy.resources.memory.limit | string | `""` | Maximum amount of memory that the proxy can use  |
| global.proxy.resources.memory.request | string | `""` | Maximum amount of memory that the proxy requests |
| global.proxy.uid | int | `2102` |  |
| global.proxy.waitBeforeExitSeconds | int | `0` | If set the proxy sidecar will stay alive for at least the given period before receiving SIGTERM signal from Kubernetes but no longer than pod's `terminationGracePeriodSeconds`. See [Lifecycle hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/#container-hooks) for more info on container lifecycle hooks. |
| global.proxyInit.closeWaitTimeoutSecs | int | `0` |  |
| global.proxyInit.ignoreInboundPorts | string | `"25,443,587,3306,11211"` | Default set of ports to skip via itpables: - SMTP (25,587) server-first - HTTPS (443) opaque TLS - MYSQL (3306) server-first - Memcached (11211) clients do not issue any preamble, which breaks detection |
| global.proxyInit.ignoreOutboundPorts | string | `"25,443,587,3306,11211"` | Default set of ports to skip via itpables, same defaults as InboudPorts |
| global.proxyInit.image.name | string | `"ghcr.io/linkerd/proxy-init"` | Docker image for the proxy-init container |
| global.proxyInit.image.pullPolicy | string | `"IfNotPresent"` | Pull policy for the proxy-init container Docker image |
| global.proxyInit.image.version | string | `"v1.3.8"` | Tag for the proxy-init container Docker image |
| global.proxyInit.resources.cpu.limit | string | `"100m"` | Maximum amount of CPU units that the proxy-init container can use |
| global.proxyInit.resources.cpu.request | string | `"10m"` | Amount of CPU units that the proxy-init container requests |
| global.proxyInit.resources.memory.limit | string | `"50Mi"` | Maximum amount of memory that the proxy-init container can use |
| global.proxyInit.resources.memory.request | string | `"10Mi"` | Amount of memory that the proxy-init container requests |
| global.proxyInit.xtMountPath.mountPath | string | `"/run"` |  |
| global.proxyInit.xtMountPath.name | string | `"linkerd-proxy-init-xtables-lock"` |  |
| global.proxyInjectAnnotation | string | `"linkerd.io/inject"` | Annotation label to signal injection. Do not edit. |
| global.proxyInjectDisabled | string | `"disabled"` | Annotation value to disable injection. Do not edit.  |
| global.workloadNamespaceLabel | string | `"linkerd.io/workload-ns"` |  |
| grafana.enabled | bool | `true` |  |
| heartbeatSchedule | string | `"0 0 * * *"` | Config for the heartbeat cronjob |
| identity.issuer.clockSkewAllowance | string | `"20s"` | Amount of time to allow for clock skew within a Linkerd cluster |
| identity.issuer.crtExpiry | string | `nil` | Expiration timestamp for the issuer certificate. It must be provided during install. Must match the expiry date in crtPEM |
| identity.issuer.crtExpiryAnnotation | string | `"linkerd.io/identity-issuer-expiry"` | Annotation used to identity the issuer certificate expiration timestamp. Do not edit. |
| identity.issuer.issuanceLifetime | string | `"24h0m0s"` | Amount of time for which the Identity issuer should certify identity |
| identity.issuer.scheme | string | `"linkerd.io/tls"` |  |
| identity.issuer.tls | object | `{"crtPEM":"","keyPEM":""}` | Which scheme is used for the identity issuer secret format  |
| identity.issuer.tls.crtPEM | string | `""` | Issuer certificate (ECDSA). It must be provided during install. |
| identity.issuer.tls.keyPEM | string | `""` | Key for the issuer certificate (ECDSA). It must be provided during install |
| installNamespace | bool | `true` | Set to false when installing Linkerd in a custom namespace. See the [Linkerd documentation](https://linkerd.io/2/tasks/install-helmcustomizing-the-namespace) for more information. |
| nodeSelector | object | `{"beta.kubernetes.io/os":"linux"}` | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information |
| omitWebhookSideEffects | bool | `false` | Omit the `sideEffects` flag in the webhook manifests |
| profileValidator.caBundle | string | `""` | Bundle of CA certificates for service profile validator. If not provided then Helm will use the certificate generated  for `profileValidator.crtPEM`. If `profileValidator.externalSecret` is set to true, this value must be set, as no certificate will be generated.  |
| profileValidator.crtPEM | string | `""` | Certificate for the service profile validator. If not provided then Helm will generate one. |
| profileValidator.externalSecret | bool | `false` | Do not create a secret resource for the profileValidator webhook. If this is set to `true`, the value `profileValidator.caBundle` must be set (see below). |
| profileValidator.keyPEM | string | `""` | Certificate key for the service profile validator. If not provided then Helm will generate one. |
| profileValidator.namespaceSelector | object | `{"matchExpressions":[{"key":"config.linkerd.io/admission-webhooks","operator":"NotIn","values":["disabled"]}]}` | Namespace selector used by admission webhook |
| prometheus.enabled | bool | `true` |  |
| proxyInjector.caBundle | string | `""` | Bundle of CA certificates for proxy injector. If not provided then Helm will use the certificate generated  for `proxyInjector.crtPEM`. If `proxyInjector.externalSecret` is set to true, this value must be set, as no certificate will be generated. |
| proxyInjector.crtPEM | string | `""` | Certificate for the proxy injector. If not provided then Helm will generate one. |
| proxyInjector.externalSecret | bool | `false` | Do not create a secret resource for the profileValidator webhook. If this is set to `true`, the value `proxyInjector.caBundle` must be set (see below) |
| proxyInjector.keyPEM | string | `""` | Certificate key for the proxy injector. If not provided then Helm will generate one.  |
| proxyInjector.namespaceSelector | object | `{"matchExpressions":[{"key":"config.linkerd.io/admission-webhooks","operator":"NotIn","values":["disabled"]}]}` | Namespace selector used by admission webhook. If not set defaults to all namespaces without the annotation config.linkerd.io/admission-webhooks=disabled |
| tap.caBundle | string | `""` | Bundle of CA certificates for Tap component. If not provided then Helm will use the certificate generated  for `tap.crtPEM`. If `tap.externalSecret` is set to true, this value must be set, as no certificate will be generated. |
| tap.crtPEM | string | `""` | Certificate for the Tap component. If not provided then Helm will generate one. |
| tap.externalSecret | bool | `false` | Do not create a secret resource for the Tap component. If this is set to `true`, the value `tap.caBundle` must be set (see below). |
| tap.keyPEM | string | `""` | Certificate key for Tap component. If not provided then Helm will generate one.  |
| webImage | string | `"ghcr.io/linkerd/web"` |  |
| webhookFailurePolicy | string | `"Ignore"` | Failure policy for the proxy injector  |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.4.0](https://github.com/norwoodj/helm-docs/releases/v1.4.0)
