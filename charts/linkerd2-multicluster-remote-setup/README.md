
# Linkerd2-multicluster-remote-setup Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes.
This chart provides a reference cluster gateway implementation, which coupled
with Linkerd and the Service Mirror component can enable multicluster
communication and service discovery

## Configuration

The following table lists the configurable parameters of the linkerd2-multicluster-remote-setup chart and their default values.

| Parameter                | Description                                                                                                     | Default                |
|--------------------------|-----------------------------------------------------------------------------------------------------------------|------------------------|
|`gatewayName`             | The name of the gateway that will be installed                                                                  | `linkerd-gateway`      |
|`namespace`               | The namespace in which the gateway and SA will be created                                                       |`linkerd-multicluster`  |
|`identityTrustDomain`     | Trust domain used for identity of the existing linkerd installation                                             |`cluster.local`         |
|`incomingPort`            | The port on which all the gateway will accept incoming traffic                                                  |`4180`                    |
|`linkerdNamespace`        | The namespace of the existing Linkerd installation                                                              |`linkerd`               |
|`nginxImage`              | The Nginx image                                                                                                 |`nginx`                 |
|`nginxImageVersion`       | The version of the Nginx image                                                                                  |`1.17`                  |
|`probePath`               | The path that will be used by remote clusters for determining whether the gateway is alive                  |`/health`               |
|`probePeriodSeconds`      | The interval (in seconds) between liveness probes                                                               |`3`                     |
|`probePort`               | The port used for liveliness probing                                                                            |`4181`                    |
|`serviceAccountName`      | The name of the service account that will be created and used by remote clusters, attempting to mirror services |`linkerd-service-mirror`|
