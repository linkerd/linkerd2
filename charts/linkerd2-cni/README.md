
# Linkerd2-cni Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes.
The Linkerd [CNI plugin](https://linkerd.io/2/features/cni/) takes care of
setting up your pod's network so  incoming and outgoing traffic is proxied
through the data plane.

## Configuration

The following table lists the configurable parameters of the Linkerd2-cni chart and their default values.

| Parameter                            | Description                                                           | Default                       |
|--------------------------------------|-----------------------------------------------------------------------|-------------------------------|
|`namespace`                           | Control plane namespace                                               | `linkerd`|
|`controllerNamespaceLabel`            | Control plane label. Do not edit                                      |`linkerd.io/control-plane-ns`|
|`cniResourceAnnotation`               | CNI resource annotation. Do not edit                                  |`linkerd.io/cni-resource`
|`inboundProxyPort`                    | Inbound port for the proxy container                                  |`4143`|
|`outboundProxyPort`                   | Outbound port for the proxy container                                 |`4140`|
|`ignoreInboundPorts`                  | Inbound ports the proxy should ignore                                 ||
|`ignoreOutboundPorts`                 | Outbound ports the proxy should ignore                                ||
|`createdByAnnotation`                 | Annotation label for the proxy create. Do not edit.                   |`linkerd.io/created-by`|
|`cniPluginImage`                      | Docker image for the cni plugin                                       |`gcr.io/linkerd-io/cni-plugin`|
|`cniPluginVersion`                    | Tag for the cni container Docker image                                |latest version|
|`logLevel`                            | Log level for the cni plugin                                          |`info`|
|`portsToRedirect`                     | Ports to redirect to proxy                                            ||
|`proxyUID`                            | User id under which the proxy shall be ran                            |`2102`|
|`destCNINetDir`                       | Directory on the host where the CNI configuration will be placed      |`/etc/cni/net.d`|
|`destCNIBinDir`                       | Directory on the host where the CNI plugin binaries reside            |`/opt/cni/bin`|
|`useWaitFlag`                         | Configures the CNI plugin to use the -w flag for the iptables command |`false`|

