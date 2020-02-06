
# Linkerd2-cni Helm Chart

Linkerd is a *service mesh*, designed to give platform-wide observability,
reliability, and security without requiring configuration or code changes.
The Linkerd Service Mirror makes it possible to mirror services located
on remote clusters with the purpose of routing traffic to them.

## Configuration

The following table lists the configurable parameters of the linkerd2-service-mirror chart and their default values.

namespace: linkerd-service-mirror
serviceMirrorVersion: edge-20.1.4
serviceMirrorImage: gcr.io/linkerd-io/controller
serviceMirrorUID: 2103

| Parameter                            | Description                                                           | Default                       |
|--------------------------------------|-----------------------------------------------------------------------|-------------------------------|
|`serviceMirrorImage`                  | Docker image for the Service mirror component                         |`gcr.io/linkerd-io/controller`|
|`serviceMirrorVersion`                | Tag for the Service Mirror container Docker image                     |latest version|
|`namespace`                           | Service Mirror component namespace                                    |`linkerd-service-mirror`|
|`serviceMirrorUID`                    | User id under which the Service Mirror shall be ran                   |`2103`|
|`logLevel`                            | Log level for the Service Mirror component                            |`info`|
