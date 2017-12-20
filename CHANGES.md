## v0.1.1

Conduit 0.1.1 is focused on making it easier to get started with Conduit.

* Conduit can now be installed on Kubernetes clusters that use RBAC
* The new `--skip-outbound-ports` flag for the CLI `inject` command directs Conduit to bypass proxying for specific outbound ports, making Conduit easier to use with non-gRPC or HTTP/2 protocols.
* The CLI `tap` command output has been reformatted to be line-oriented, making it easier to parse with common UNIX command line utilities
* Proxies now support routing to non-fully qualified domain names, with routing rules that more closely resemble the default Kubernetes DNS search path
* The web UI has improved support for large deployments and deployments that don’t have any inbound/outbound traffic

## v0.1.0

Conduit 0.1.0 is the first public release of Conduit.

* This release supports services that communicate via gRPC only. non-gRPC HTTP/2 services should work. More complete HTTP support, including HTTP/1.0 and HTTP/1.1 and non-gRPC HTTP/2, will be added in an upcoming release.
* Kubernetes 1.8.0 or later is required.
* kubectl 1.8.0 or later is required. `conduit dashboard` will not work with earlier versions of kubectl.
* When deploying to Minikube, Minikube 0.23 or 0.24.1 or later are required. Earlier versions will not work.
* This release has been tested using Google Kubernetes Engine and Minikube. Upcoming releases will be tested on additional providers too.
* Configuration settings and protocols are not stable yet.
* Services written in Go must use grpc-go 1.3 or later to avoid [grpc-go bug #1120](https://github.com/grpc/grpc-go/issues/1120).
