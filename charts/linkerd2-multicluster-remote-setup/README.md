
# Linkerd2-multicluster-remote-sertup Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes.
This chart provides a reference cluster gateway implementation, which coupled
with Linkerd and the Service Mirror component can enable multicluster 
communication and service discovery

## Configuration

The following table lists the configurable parameters of the linkerd2-multicluster-remote-sertup chart and their default values.

| Parameter                | Description                                                                                                     | Default                |
|--------------------------|-----------------------------------------------------------------------------------------------------------------|------------------------|
|`gatewayName`             | The name of the gateway that will be installed                                                                  | `linkerd-gateway`      |
|`gatewayNamespace`        | The namespace in which the gateway will be created                                                              |`linkerd-gateway`       |
|`identityTrustDomain`     | Trust domain used for identity of the existing linkerd installation                                             |`cluster.local`         |
|`incomingPort`            | The port on which all the gateway will accept incoming traffic                                                  |`80`                    |
|`linkerdNamespace`        | The namespace of the existing Linkerd installation                                                              |`linkerd`               |
|`nginxImage`              | The Nginx image                                                                                                 |`nginx`                 |
|`nginxImageVersion`       | The version of the Nginx image                                                                                  |`1.17`                  |
|`probePath`               | The path tha that will be used by remote clusters for determining whether the gateway is alive                  |`/health`               |
|`probePeriodSeconds`      | The interval (in seconds) between liveness probes                                                               |`3`                     |
|`probePort`               | The port used for liveliness probing                                                                            |`81`                    |
|`serviceAccountName`      | The name of the service account that will be created and used by remote clusters, attempting to mirror services |`linkerd-service-mirror`|
|`serviceAccountNamespace` | The namespace in which the service account will be created                                                      |`linkerd-service-mirror`|
