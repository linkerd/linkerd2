
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
|`cniPluginImage`                      | Docker image for the CNI plugin                                       |`gcr.io/linkerd-io/cni-plugin`|
|`cniPluginVersion`                    | Tag for the CNI container Docker image                                |latest version|
|`cniResourceAnnotation`               | CNI resource annotation. Do not edit                                  |`linkerd.io/cni-resource`
|`controllerNamespaceLabel`            | Control plane label. Do not edit                                      |`linkerd.io/control-plane-ns`|
|`createdByAnnotation`                 | Annotation label for the proxy create. Do not edit.                   |`linkerd.io/created-by`|
|`destCNIBinDir`                       | Directory on the host where the CNI plugin binaries reside            |`/opt/cni/bin`|
|`destCNINetDir`                       | Directory on the host where the CNI configuration will be placed      |`/etc/cni/net.d`|
|`ignoreInboundPorts`                  | Inbound ports the proxy should ignore                                 ||
|`ignoreOutboundPorts`                 | Outbound ports the proxy should ignore                                ||
|`inboundProxyPort`                    | Inbound port for the proxy container                                  |`4143`|
|`logLevel`                            | Log level for the CNI plugin                                          |`info`|
|`namespace`                           | CNI plugin plane namespace                                            |`linkerd-cni`|
|`outboundProxyPort`                   | Outbound port for the proxy container                                 |`4140`|
|`portsToRedirect`                     | Ports to redirect to proxy                                            ||
|`proxyUID`                            | User id under which the proxy shall be ran                            |`2102`|
|`useWaitFlag`                         | Configures the CNI plugin to use the -w flag for the iptables command |`false`|

