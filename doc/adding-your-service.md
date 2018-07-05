+++
title = "Adding your service to the mesh"
docpage = true
[menu.docs]
  parent = "adding-your-service"
+++

In order for your service to take advantage of Conduit, it needs to be added
to the service mesh. This is done by using the Conduit CLI to add the Conduit
proxy sidecar to each pod. By doing this as a rolling update, the availability
of your application will not be affected.

## Prerequisites

* gRPC applications that use grpc-go must use grpc-go version 1.3 or later due
  to a [bug](https://github.com/grpc/grpc-go/issues/1120) in earlier versions.

## Adding your service

### To add your service to the service mesh, run:
#### `conduit inject deployment.yml | kubectl apply -f -`

`deployment.yml` is the Kubernetes config file containing your
application. This will trigger a rolling update of your deployment, replacing
each pod with a new one that additionally contains the Conduit sidecar proxy.

You will know that your service has been successfully added to the service mesh
if its proxy status is green in the Conduit dashboard.

![](images/dashboard-data-plane.png "conduit dashboard")

### You can always get to the Conduit dashboard by running
#### `conduit dashboard`

## Protocol Support

Conduit is capable of proxying all TCP traffic, including WebSockets and HTTP
tunneling, and reporting top-line metrics (success rates, latencies, etc) for
all HTTP, HTTP/2, and gRPC traffic.
