# linkerd-base

Linkerd gives you observability, reliability, and security
for your microservices — with no code change required.

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square)
![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
![AppVersion: edge-XX.X.X](https://img.shields.io/badge/AppVersion-edge--XX.X.X-informational?style=flat-square)

**Homepage:** <https://linkerd.io>

## Quickstart and documentation

You can run Linkerd on any Kubernetes 1.20+ cluster in a matter of seconds. See
the [Linkerd Getting Started Guide][getting-started] for how.

For more comprehensive documentation, start with the [Linkerd
docs][linkerd-docs].

## Adding Linkerd's Helm repository

```bash
# To add the repo for Linkerd stable releases:
helm repo add linkerd https://helm.linkerd.io/stable
# To add the repo for Linkerd edge releases:
helm repo add linkerd-edge https://helm.linkerd.io/edge
```

The following instructions use the `linkerd` repo. For installing an edge
release, just replace with `linkerd-edge`.

## Installing the linkerd-base chart

This installs the `linkerd-base` chart, which only persists the cluster-level
resources, such as `ClusterRoles` and `CRD`s. This means the user performing
the installation requires cluster-level privileges.

After installing this chart, you need then to install the
`linkerd-control-plane` chart in the same namespace, which provides all the
linkerd core control components (and only requires namespace-level privileges).

```bash
helm install linkerd-base -n linkerd --create-namespace linkerd/linkerd-base
```

## Setting High-Availability

Besides the default `values.yaml` file, the chart provides a `values-ha.yaml`
file that overrides some default values as to set things up under a
high-availability scenario, analogous to the `--ha` option in `linkerd install`.

You can get ahold of `values-ha.yaml` by fetching the chart files:

```bash
helm fetch --untar linkerd/linkerd-base
```

Then use the `-f` flag to provide the override file, for example:

```bash
helm install linkerd-base -n linkerd --create-namespace \
  -f linkerd2/values-ha.yaml
  linkerd/linkerd-base
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

Kubernetes: `>=1.20.0-0`

| Repository | Name | Version |
|------------|------|---------|
| file://../partials | partials | 0.1.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| cniEnabled | bool | `false` | enabling this omits the NET_ADMIN capability in the PSP and the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed |
| disableHeartBeat | bool | `false` | Set to true to not start the heartbeat cronjob |
| enableEndpointSlices | bool | `true` | enables the use of EndpointSlice informers for the destination service; enableEndpointSlices should be set to true only if EndpointSlice K8s feature gate is on; the feature is still experimental. |
| enablePSP | bool | `false` | Add a PSP resource and bind it to the control plane ServiceAccounts. Note PSP has been deprecated since k8s v1.21 |
| imagePullSecrets | list | `[]` |  |
| linkerdVersion | string | `"linkerdVersionValue"` | control plane version. See Proxy section for proxy version |
| omitWebhookSideEffects | bool | `false` | Omit the `sideEffects` flag in the webhook manifests |
| policyValidator.caBundle | string | `""` | Bundle of CA certificates for policy validator. If not provided then Helm will use the certificate generated  for `policyValidator.crtPEM`. If `policyValidator.externalSecret` is set to true, this value must be set, as no certificate will be generated. |
| policyValidator.crtPEM | string | `""` | Certificate for the policy validator. If not provided then Helm will generate one. |
| policyValidator.externalSecret | bool | `false` | Do not create a secret resource for the policyValidator webhook. If this is set to `true`, the value `policyValidator.caBundle` must be set (see below). |
| policyValidator.keyPEM | string | `""` | Certificate key for the policy validator. If not provided then Helm will generate one. |
| policyValidator.namespaceSelector | object | `{"matchExpressions":[{"key":"config.linkerd.io/admission-webhooks","operator":"NotIn","values":["disabled"]}]}` | Namespace selector used by admission webhook |
| profileValidator.caBundle | string | `""` | Bundle of CA certificates for service profile validator. If not provided then Helm will use the certificate generated  for `profileValidator.crtPEM`. If `profileValidator.externalSecret` is set to true, this value must be set, as no certificate will be generated. |
| profileValidator.crtPEM | string | `""` | Certificate for the service profile validator. If not provided then Helm will generate one. |
| profileValidator.externalSecret | bool | `false` | Do not create a secret resource for the profileValidator webhook. If this is set to `true`, the value `profileValidator.caBundle` must be set (see below). |
| profileValidator.keyPEM | string | `""` | Certificate key for the service profile validator. If not provided then Helm will generate one. |
| profileValidator.namespaceSelector | object | `{"matchExpressions":[{"key":"config.linkerd.io/admission-webhooks","operator":"NotIn","values":["disabled"]}]}` | Namespace selector used by admission webhook |
| proxyInjector.caBundle | string | `""` | Bundle of CA certificates for proxy injector. If not provided then Helm will use the certificate generated  for `proxyInjector.crtPEM`. If `proxyInjector.externalSecret` is set to true, this value must be set, as no certificate will be generated. |
| proxyInjector.crtPEM | string | `""` | Certificate for the proxy injector. If not provided then Helm will generate one. |
| proxyInjector.externalSecret | bool | `false` | Do not create a secret resource for the profileValidator webhook. If this is set to `true`, the value `proxyInjector.caBundle` must be set (see below) |
| proxyInjector.keyPEM | string | `""` | Certificate key for the proxy injector. If not provided then Helm will generate one. |
| proxyInjector.namespaceSelector | object | `{"matchExpressions":[{"key":"config.linkerd.io/admission-webhooks","operator":"NotIn","values":["disabled"]}]}` | Namespace selector used by admission webhook. If not set defaults to all namespaces without the annotation config.linkerd.io/admission-webhooks=disabled |
| webhookFailurePolicy | string | `"Ignore"` | Failure policy for the proxy injector |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.4.0](https://github.com/norwoodj/helm-docs/releases/v1.4.0)
