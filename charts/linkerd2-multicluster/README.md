
# Linkerd2-multicluster Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes. This
chart provides the components needed to enable communication between clusters.

## Configuration

The following table lists the configurable parameters of the
linkerd2-multicluster chart and their default values.

| Parameter                       | Description                                                                                 | Default                               |
|---------------------------------|---------------------------------------------------------------------------------------------|---------------------------------------|
|`controllerComponentLabel`       | Control plane label. Do not edit                                                            |`linkerd.io/control-plane-component`   |
|`controllerImage`                | Docker image for the Service mirror component (uses the Linkerd controller image)           |`gcr.io/linkerd-io/controller`         |
|`controllerImageVersion`         | Tag for the Service Mirror container Docker image                                           |`latest version`                       |
|`createdByAnnotation`            | Annotation label for the proxy create. Do not edit.                                         |`linkerd.io/created-by`                |
|`gateway`                        | Whether the gateway component should be installed                                           |`true`                                 |
|`gatewayLocalProbePath`          | The path that will be used by the local liveness checks to ensure the gateway is alive      |`/health-local`                        |
|`gatewayLocalProbePort`          | The port that will be used by the local liveness checks to ensure the gateway is alive      |`8888`                                 |
|`gatewayName`                    | The name of the gateway that will be installed                                              |`linkerd-gateway`                      |
|`gatewayNginxImage`              | The Nginx image                                                                             |`nginx`                                |
|`gatewayNginxImageVersion`       | The version of the Nginx image                                                              |`1.17`                                 |
|`gatewayPort`                    | The port on which all the gateway will accept incoming traffic                              |`4180`                                 |
|`gatewayProbePath`               | The path that will be used by remote clusters for determining whether the gateway is alive  |`/health`                              |
|`gatewayProbePort`               | The port used for liveliness probing                                                        |`4181`                                 |
|`gatewayProbeSeconds`            | The interval (in seconds) between liveness probes                                           |`3`                                    |
|`identityTrustDomain`            | Trust domain used for identity of the existing linkerd installation                         |`cluster.local`                        |
|`linkerdNamespace`               | The namespace of the existing Linkerd installation                                          |`linkerd`                              |
| `linkerdVersion`                | Control plane version                                                                       | latest version                        |
|`namespace`                      | Service Mirror component namespace                                                          |`linkerd-service-mirror`               |
|`proxyOutboundPort`              | The port on which the proxy accepts outbound traffic                                        |`4140`                                 |
|`remoteAccessServiceAccountName` | The name of the service account used to allow remote clusters to mirror local services      |`linkerd-service-mirror-remote-access` |
|`serviceMirror`                  | Whether the service mirror component should be installed                                    |`true`                                 |
|`logLevel`          | Log level for the Multicluster components                                                  |`info`                                 |
|`serviceMirrorRetryLimit`        | Number of times update from the remote cluster is allowed to be requeued (retried)          |`3`                                    |
|`serviceMirrorUID`               | User id under which the Service Mirror shall be ran                                         |`2103`                                 |
