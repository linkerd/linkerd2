+++
title = "Adding your service to the mesh"
docpage = true
[menu.docs]
  parent = "adding-your-service"
+++

In order for your service to take advantage of Linkerd, it needs to be added
to the service mesh. This is done by using the Linkerd CLI to add the Linkerd
proxy sidecar to each pod. By doing this as a rolling update, the availability
of your application will not be affected.

## Prerequisites

* Applications that use protocols where the server sends data before the client
  sends data may require additional configuration. See the
  [Protocol support](#protocol-support) section below.
* gRPC applications that use grpc-go must use grpc-go version 1.3 or later due
  to a [bug](https://github.com/grpc/grpc-go/issues/1120) in earlier versions.

## Adding your service

### To add your service to the service mesh, run:

#### `linkerd inject deployment.yml | kubectl apply -f -`

`deployment.yml` is the Kubernetes config file containing your
application. This will trigger a rolling update of your deployment, replacing
each pod with a new one that additionally contains the Linkerd sidecar proxy.

You will know that your service has been successfully added to the service mesh
if its proxy status is green in the Linkerd dashboard.

![dashboard](images/dashboard-data-plane.png "linkerd dashboard")

### You can always get to the Linkerd dashboard by running

#### `linkerd dashboard`

## Protocol support

Linkerd is capable of proxying all TCP traffic, including WebSockets and HTTP
tunneling, and reporting top-line metrics (success rates, latencies, etc) for
all HTTP, HTTP/2, and gRPC traffic.

### Server-speaks-first protocols

For protocols where the server sends data before the client sends data over
connections that aren't protected by TLS, Linkerd cannot automatically recognize
the protocol used on the connection. Two common examples of this type of
protocol are MySQL and SMTP. If you are using Linkerd to proxy plaintext MySQL
or SMTP requests on their default ports (3306 and 25, respectively), then Linkerd
is able to successfully identify these protocols based on the port. If you're
using non-default ports, or if you're using a different server-speaks-first
protocol, then you'll need to manually configure Linkerd to recognize these
protocols.

If you're working with a protocol that can't be automatically recognized by
Linkerd, use the `--skip-inbound-ports` and `--skip-outbound-ports` flags when
running `linkerd inject`.

### For example, if your application makes requests to a MySQL database running on port 4406, use the command:
#### `linkerd inject deployment.yml --skip-outbound-ports=4406 | kubectl apply -f -`

### Likewise if your application runs an SMTP server that accepts incoming requests on port 35, use the command:
#### `linkerd inject deployment.yml --skip-inbound-ports=35 | kubectl apply -f -`
