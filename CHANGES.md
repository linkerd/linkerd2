## v0.1.2

Conduit 0.1.2 continues down the path of increasing usability and improving debugging and
introspection of the service mesh itself.

* Conduit CLI
  * New `conduit check` command reports on the health of your Conduit installation.
  * New `conduit completion` command provides shell completion.
* Dashboard
  * Added per-path metrics to the deployment detail pages.
  * Added animations to line graphs indicating server activity.
  * More descriptive CSS variable names. (Thanks @natemurthy!)
  * A variety of other minor UI bugfixes and improvements
* Fixes
  * Fixed Prometheus config when using RBAC. (Thanks @FaKod!)
  * Fixed `tap` failure when pods do not belong to a deployment. (Thanks @FaKod!)

## v0.1.1

Conduit 0.1.1 is focused on making it easier to get started with Conduit.

* Conduit can now be installed on Kubernetes clusters that use RBAC.
* The `conduit inject` command now supports a `--skip-outbound-ports` flag that directs Conduit to
  bypass proxying for specific outbound ports, making Conduit easier to use with non-gRPC or HTTP/2
  protocols.
* The `conduit tap` command output has been reformatted to be line-oriented, making it easier to
  parse with common UNIX command line utilities.
* Conduit now supports routing of non-fully qualified domain names.
* The web UI has improved support for large deployments and deployments that don’t have any
  inbound/outbound traffic.

## v0.1.0

Conduit 0.1.0 is the first public release of Conduit.

* This release supports services that communicate via gRPC only. non-gRPC HTTP/2 services should
  work. More complete HTTP support, including HTTP/1.0 and HTTP/1.1 and non-gRPC HTTP/2, will be
  added in an upcoming release.
* Kubernetes 1.8.0 or later is required.
* kubectl 1.8.0 or later is required. `conduit dashboard` will not work with earlier versions of
  kubectl.
* When deploying to Minikube, Minikube 0.23 or 0.24.1 or later are required. Earlier versions will
  not work.
* This release has been tested using Google Kubernetes Engine and Minikube. Upcoming releases will
  be tested on additional providers too.
* Configuration settings and protocols are not stable yet.
* Services written in Go must use grpc-go 1.3 or later to avoid
  [grpc-go bug #1120](https://github.com/grpc/grpc-go/issues/1120).
