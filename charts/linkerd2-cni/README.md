# linkerd2-cni

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes. The
Linkerd [CNI plugin](https://linkerd.io/2/features/cni/) takes care of setting
up your pod's network so  incoming and outgoing traffic is proxied through the
data plane.

![Version: 0.0.0-undefined](https://img.shields.io/badge/Version-0.0.0--undefined-informational?style=flat-square)

![AppVersion: edge-XX.X.X](https://img.shields.io/badge/AppVersion-edge--XX.X.X-informational?style=flat-square)

## Requirements

Kubernetes: `>=1.22.0-0`

| Repository | Name | Version |
|------------|------|---------|
| file://../partials | partials | 0.1.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| commonLabels | object | `{}` | Labels to apply to all resources |
| destCNIBinDir | string | `"/opt/cni/bin"` | Directory on the host where the CNI configuration will be placed |
| destCNINetDir | string | `"/etc/cni/net.d"` | Directory on the host where the CNI plugin binaries reside |
| disableIPv6 | bool | `true` | Disables adding IPv6 rules on top of IPv4 rules |
| enablePSP | bool | `false` | Add a PSP resource and bind it to the linkerd-cni ServiceAccounts. Note PSP has been deprecated since k8s v1.21 |
| extraInitContainers | list | `[]` | Add additional initContainers to the daemonset |
| ignoreInboundPorts | string | `""` | Default set of inbound ports to skip via iptables |
| ignoreOutboundPorts | string | `""` | Default set of outbound ports to skip via iptables |
| image.name | string | `"cr.l5d.io/linkerd/cni-plugin"` | Docker image for the CNI plugin |
| image.pullPolicy | string | `"IfNotPresent"` | Pull policy for the linkerd-cni container |
| image.version | string | `"v1.6.0"` | Tag for the CNI container Docker image |
| imagePullSecrets | list | `[]` |  |
| inboundProxyPort | int | `4143` | Inbound port for the proxy container |
| iptablesMode | string | `"legacy"` | Variant of iptables that will be used to configure routing |
| logLevel | string | `"info"` | Log level for the CNI plugin |
| outboundProxyPort | int | `4140` | Outbound port for the proxy container |
| podLabels | object | `{}` | Additional labels to add to all pods |
| portsToRedirect | string | `""` | Ports to redirect to proxy |
| priorityClassName | string | `""` | Kubernetes priorityClassName for the CNI plugin's Pods |
| privileged | bool | `false` | Run the install-cni container in privileged mode |
| proxyAdminPort | int | `4191` | Admin port for the proxy container |
| proxyControlPort | int | `4190` | Control port for the proxy container |
| proxyGID | int | `-1` | Optional customisation of the group id under which the proxy shall be ran (the group ID will be omitted if lower than 0) |
| proxyUID | int | `2102` | User id under which the proxy shall be ran |
| repairController.enableSecurityContext | bool | `true` | Include a securityContext in the repair-controller container |
| repairController.enabled | bool | `false` | Enables the repair-controller container |
| repairController.logFormat | string | plain | Log format (`plain` or `json`) for the repair-controller container |
| repairController.logLevel | string | info | Log level for the repair-controller container |
| repairController.resources.cpu.limit | string | `""` | Maximum amount of CPU units that the repair-controller container can use |
| repairController.resources.cpu.request | string | `""` | Amount of CPU units that the repair-controller container requests |
| repairController.resources.ephemeral-storage.limit | string | `""` | Maximum amount of ephemeral storage that the repair-controller container can use |
| repairController.resources.ephemeral-storage.request | string | `""` | Amount of ephemeral storage that the repair-controller container requests |
| repairController.resources.memory.limit | string | `""` | Maximum amount of memory that the repair-controller container can use |
| repairController.resources.memory.request | string | `""` | Amount of memory that the repair-controller container requests |
| resources | object | `{"cpu":{"limit":"","request":""},"ephemeral-storage":{"limit":"","request":""},"memory":{"limit":"","request":""}}` | Resource requests and limits for linkerd-cni daemonset container |
| resources.cpu.limit | string | `""` | Maximum amount of CPU units that the cni container can use |
| resources.cpu.request | string | `""` | Amount of CPU units that the cni container requests |
| resources.ephemeral-storage.limit | string | `""` | Maximum amount of ephemeral storage that the cni container can use |
| resources.ephemeral-storage.request | string | `""` | Amount of ephemeral storage that the cni container requests |
| resources.memory.limit | string | `""` | Maximum amount of memory that the cni container can use |
| resources.memory.request | string | `""` | Amount of memory that the cni container requests |
| revisionHistoryLimit | int | `10` | Specifies the number of old ReplicaSets to retain to allow rollback. |
| tolerations[0] | object | `{"operator":"Exists"}` | toleration properties |
| useWaitFlag | bool | `false` | Configures the CNI plugin to use the -w flag for the iptables command |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
