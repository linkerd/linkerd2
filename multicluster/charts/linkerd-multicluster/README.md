# linkerd-multicluster

The Linkerd-Multicluster extension contains resources to support multicluster
linking to remote clusters

![Version: 30.3.5-edge](https://img.shields.io/badge/Version-30.3.5--edge-informational?style=flat-square)

![AppVersion: edge-XX.X.X](https://img.shields.io/badge/AppVersion-edge--XX.X.X-informational?style=flat-square)

**Homepage:** <https://linkerd.io>

## Quickstart and documentation

You can run Linkerd on any Kubernetes 1.21+ cluster in a matter of seconds. See
the [Linkerd Getting Started Guide][getting-started] for how.

For more comprehensive documentation, start with the [Linkerd
docs][linkerd-docs].

## Prerequisite: Linkerd Core Control-Plane

Before installing the Linkerd Multicluster extension, The core control-plane has
to be installed first by following the [Linkerd Install
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

## Installing the Multicluster Extension Chart

```bash
helm install linkerd-multicluster -n linkerd-multicluster --create-namespace linkerd/linkerd-multicluster
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
| commonLabels | object | `{}` | Labels to apply to all resources |
| enablePSP | bool | `false` | Create Roles and RoleBindings to associate this extension's ServiceAccounts to the control plane PSP resource. This requires that `enabledPSP` is set to true on the control plane install. Note PSP has been deprecated since k8s v1.21 |
| enablePodAntiAffinity | bool | `false` | Enables Pod Anti Affinity logic to balance the placement of replicas across hosts and zones for High Availability. Enable this only when you have multiple replicas of components. |
| gateway.UID | int | `2103` | User id under which the gateway shall be ran |
| gateway.enabled | bool | `true` | If the gateway component should be installed |
| gateway.loadBalancerIP | string | `""` | Set loadBalancerIP on gateway service |
| gateway.name | string | `"linkerd-gateway"` | The name of the gateway that will be installed |
| gateway.pauseImage | string | `"gcr.io/google_containers/pause:3.2"` | The pause container to use |
| gateway.port | int | `4143` | The port on which all the gateway will accept incoming traffic |
| gateway.probe.path | string | `"/ready"` | The path that will be used by remote clusters for determining whether the gateway is alive |
| gateway.probe.port | int | `4191` | The port used for liveliness probing |
| gateway.probe.seconds | int | `3` | The interval (in seconds) between liveness probes |
| gateway.replicas | int | `1` | Number of replicas for the gateway pod |
| gateway.serviceAnnotations | object | `{}` | Annotations to add to the gateway service |
| gateway.serviceType | string | `"LoadBalancer"` | Service Type of gateway Service |
| identityTrustDomain | string | `"cluster.local"` | Identity Trust Domain of the certificate authority |
| imagePullPolicy | string | `"IfNotPresent"` | Docker imagePullPolicy for all multicluster components |
| linkerdNamespace | string | `"linkerd"` | Namespace of linkerd installation |
| linkerdVersion | string | `"linkerdVersionValue"` | Control plane version |
| namespaceMetadata.image.name | string | `"curl"` | Docker image name for the namespace-metadata instance |
| namespaceMetadata.image.pullPolicy | string | imagePullPolicy | Pull policy for the namespace-metadata instance |
| namespaceMetadata.image.registry | string | `"curlimages"` | Docker registry for the namespace-metadata instance |
| namespaceMetadata.image.tag | string | `"7.78.0"` | Docker image tag for the namespace-metadata instance |
| podLabels | object | `{}` | Additional labels to add to all pods |
| proxyOutboundPort | int | `4140` | The port on which the proxy accepts outbound traffic |
| remoteMirrorServiceAccount | bool | `true` | If the remote mirror service account should be installed |
| remoteMirrorServiceAccountName | string | `"linkerd-service-mirror-remote-access-default"` | The name of the service account used to allow remote clusters to mirror local services |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.11.0](https://github.com/norwoodj/helm-docs/releases/v1.11.0)
