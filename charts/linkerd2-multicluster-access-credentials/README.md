
# Linkerd2-multicluster-access-credentials Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes. This
chart provides the credentials that are needed to enable mirroring of the services
located on one cluster onto another.

## Configuration

The following table lists the configurable parameters of the
linkerd2-multicluster-access-credentials chart and their default values.

| Parameter                       | Description                                                                                 | Default                               |
|---------------------------------|---------------------------------------------------------------------------------------------|---------------------------------------|
|`remoteAccessServiceAccountName` | The name of the service account used to allow remote clusters to mirror local services      |`linkerd-service-mirror-remote-access` |
| `linkerdVersion`                | Control plane version                                                                       | latest version                        |
|`createdByAnnotation`            | Annotation label for the proxy create. Do not edit.                                         |`linkerd.io/created-by`                |
|`namespace`                      | Service Mirror component namespace                                                          |`linkerd-service-mirror`               |