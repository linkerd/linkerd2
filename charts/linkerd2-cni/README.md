# Linkerd2-cni Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes.
The linkerd CNI plugin takes care of setting up your pod's network so 
incoming and outgoing traffic is proxied through the data plane.

## Configuration

The following table lists the configurable parameters of the Linkerd2-cni chart and their default values.

| Parameter                            | Description                                                           | Default                       |
|--------------------------------------|-----------------------------------------------------------------------|-------------------------------|
|`Namespace`                           | Control plane namespace                                               | `linkerd`|
|`ControllerNamespaceLabel`            | Control plane label. Do not edit                                      |`linkerd.io/control-plane-component`|
|`CniResourceAnnotation`               | CNI resource annotation. Do not edit                                  |`linkerd.io/cni-resource`
|`InboundProxyPort`                    | Inbound port for the proxy container                                  |`4143`|
|`OutboundProxyPort`                   | Outbound port for the proxy container                                 |`4140`|
|`IgnoreInboundPorts`                  | Inbound ports the proxy should ignore                                 ||
|`IgnoreOutboundPorts`                 | Outbound ports the proxy should ignore                                ||
|`CreatedByAnnotation`                 | Annotation label for the proxy create. Do not edit.                   |`linkerd.io/created-by`|
|`CniPluginImage`                      | Docker image for the cni plugin                                       |`gcr.io/linkerd-io/cni-plugin`|
|`CniPluginVersion`                    | Tag for the cni container Docker image                                |`stable-2.6.0`|
|`LogLevel`                            | Log level for the cni plugin                                          |`info`|
|`PortsToRedirect`                     | Ports to redirect to proxy                                            || 
|`ProxyUID`                            | User id under which the proxy shall be ran                            |`2102`|
|`DestCNINetDir`                       | Directory on the host where the CNI configuration will be placed      |`/etc/cni/net.d`|
|`DestCNIBinDir`                       | Directory on the host where the CNI plugin binaries reside            |`/opt/cni/bin`|
|`UseWaitFlag`                         | Configures the CNI plugin to use the -w flag for the iptables command |`false`|

