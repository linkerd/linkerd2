## v0.4.1

Conduit 0.4.1 builds on the telemetry work from 0.4.0, providing rich,
Kubernetes-aware observability and debugging.

* Web UI
  * **New** Automatically-configured Grafana dashboards for Services, Pods,
    ReplicationControllers, and Conduit mesh health.
  * **New** `conduit dashboard` Pod and ReplicationController views.
* Command-line interface
  * **Breaking change** `conduit tap` now operates on most Kubernetes resources.
  * `conduit stat` and `conduit tap` now both support kubectl-style resource
    strings (`deploy`, `deploy/web`, and `deploy web`), specifically:
    * `namespaces`
    * `deployments`
    * `replicationcontrollers`
    * `services`
    * `pods`
* Telemetry
  * **New** Tap support for filtering by and exporting destination metadata. Now
    you can sample requests from A to B, where A and B are any resource or group
    of resources.
  * **New** TCP-level stats, including connection counts and durations, and
    throughput, wired through to Grafana dashboards.
* Service Discovery
  * The proxy now uses the [trust-dns] DNS resolver. This fixes a number of DNS
    correctness issues.
  * The Destination service could sometimes return incorrect, stale, labels for an
    endpoint. This has been fixed!

[trust-dns]: https://github.com/bluejekyll/trust-dns

## v0.4.0

Conduit 0.4.0 overhauls Conduit's telemetry system and improves service discovery
reliability.

* Web UI
  * **New** automatically-configured Grafana dashboards for all Deployments.
* Command-line interface
  * `conduit stat` has been completely rewritten to accept arguments like `kubectl get`.
    The `--to` and `--from` filters can be used to filter traffic by destination and
    source, respectively.  `conduit stat` currently can operate on `Namespace` and
    `Deployment` Kubernetes resources. More resource types will be added in the next
    release!
* Proxy (data plane)
  * **New** Prometheus-formatted metrics are now exposed on `:4191/metrics`, including
    rich destination labeling for outbound HTTP requests. The proxy no longer pushes
    metrics to the control plane.
  * The proxy now handles `SIGINT` or `SIGTERM`, gracefully draining requests until all
    are complete or `SIGQUIT` is received.
  * SMTP and MySQL (ports 25 and 3306) are now treated as opaque TCP by default. You
    should no longer have to specify `--skip-outbound-ports` to communicate with such
    services.
  * When the proxy reconnected to the controller, it could continue to send requests to
    old endpoints. Now, when the proxy reconnects to the controller, it properly removes
    invalid endpoints.
  * A bug impacting some HTTP/2 reset scenarios has been fixed.
* Service Discovery
  * Previously, the proxy failed to resolve some domain names that could be misinterpreted
    as a Kubernetes Service name. This has been fixed by extending the _Destination_ API
    with a negative acknowledgement response.
* Control Plane
  * The _Telemetry_ service and associated APIs have been removed.
* Documentation
  * Updated [Roadmap](doc/roadmap.md)

Special thanks to @ahume, @alenkacz, & @xiaods for contributing to this release!

### Upgrading from v0.3.1

When upgrading from v0.3.1, it's important to upgrade proxies before upgrading the
controller. As you upgrade proxies, the controller will lose visibility into some data
plane stats. Once all proxies are updated, `conduit install |kubectl apply -f -` can be
run to upgrade the controller without causing any data plane disruptions. Once the
controller has been restarted, traffic stats should become available.

## v0.3.1

Conduit 0.3.1 improves Conduit's resilience and transparency.

* Proxy (data plane)
  * The proxy now makes fewer changes to requests and responses being proxied. In particular,
    requests and responses without bodies or with empty bodies are better supported.
  * HTTP/1 requests with different `Host` header fields are no longer sent on the same HTTP/1
    connection even when those hostnames resolve to the same IP address.
  * A connection leak during proxying of non-HTTP TCP connections was fixed.
  * The proxy now handles unavailable services more gracefully by timing out while waiting for an
    endpoint to become available for the service.
* Command-line interface
  * `$KUBECONFIG` with multiple paths is now supported. (PR #482 by @hypnoglow).
  * `conduit check` now checks for the availability of a Conduit update. (PR #460 by @ahume).
* Service Discovery
  * Kubernetes services with type `ExternalName` are now supported.
* Control Plane
  * The proxy is injected into the control plane during installation to improve the control plane's
    resilience and to "dogfood" the proxy.
  * The control plane is now more resilient regarding networking failures.
* Documentation
  * The markdown source for the documentation published at https://conduit.io/docs/ is now open
    source at https://github.com/runconduit/conduit/tree/master/doc.

## v0.3.0

Conduit 0.3 focused heavily on production hardening of Conduit's telemetry system. Conduit 0.3
should "just work" for most apps on Kubernetes 1.8 or 1.9 without configuration, and should support
Kubernetes clusters with hundreds of services, thousands of instances, and hundreds of RPS per
instance.

With this release, Conduit also moves from _experimental_ to _alpha_---meaning that we're ready
for some serious testing and vetting from you. As part of this, we've published the
[Conduit roadmap](https://conduit.io/roadmap/), and we've also launched some new mailing lists:
[conduit-users](https://groups.google.com/forum/#!forum/conduit-users),
[conduit-dev](https://groups.google.com/forum/#!forum/conduit-dev), and
[conduit-announce](https://groups.google.com/forum/#!forum/conduit-announce).

* CLI
  * CLI commands no longer depend on `kubectl`
  * `conduit dashboard` now runs on an ephemeral port, removing port 8001 conflicts
  * `conduit inject` now skips pods with `hostNetwork=true`
  * CLI commands now have friendlier error messages, and support a `--verbose` flag for debugging
* Web UI
  * All displayed metrics are now instantaneous snapshots rather than aggregated over 10 minutes
  * The sidebar can now be collapsed
  * UX refinements and bug fixes
* Conduit proxy (data plane)
  * Proxy does load-aware (P2C + least-loaded) L7 balancing for HTTP
  * Proxy can now route to external DNS names
  * Proxy now properly sheds load in some pathological cases when it cannot route
* Telemetry system
  * Many optimizations and refinements to support scale goals
  * Per-path and per-pod metrics have been removed temporarily to improve scalability and stability;
    they will be reintroduced in Conduit 0.4 (#405)
* Build improvements
  * The Conduit docker images are now much smaller.
  * Dockerfiles have been changed to leverage caching, improving build times substantially

Known Issues:
* Some DNS lookups to external domains fail (#62, #155, #392)
* Applications that use WebSockets, HTTP tunneling/proxying, or protocols such as MySQL and SMTP,
  require additional configuration (#339)

## v0.2.0

This is a big milestone! With this release, Conduit adds support for HTTP/1.x and raw TCP traffic,
meaning it should "just work" for most applications that are running on Kubernetes without
additional configuration.

* Data plane
  * Conduit now transparently proxies all TCP traffic, including HTTP/1.x and HTTP/2.
    (See caveats below.)
* Command-line interface
  * Improved error handling for the `tap` command
  * `tap` also now works with HTTP/1.x traffic
* Dashboard
  * Minor UI appearance tweaks
  * Deployments now searchable from the dashboard sidebar

Caveats:
* Conduit will automatically work for most protocols. However, applications that use WebSockets,
  HTTP tunneling/proxying, or protocols such as MySQL and SMTP, will require some additional
  configuration. See the [documentation](https://conduit.io/adding-your-service/#protocol-support)
  for details.
* Conduit doesn't yet support external DNS lookups. These will be addressed in an upcoming release.
* There are known issues with Conduit's telemetry pipeline that prevent it from scaling beyond a
  few nodes. These will be addressed in an upcoming release.
* Conduit is still in alpha! Please help us by
  [filing issues and contributing pull requests](https://github.com/runconduit/conduit/issues/new).

## v0.1.3

* This is a minor bugfix for some web dashboard UI elements that were not rendering correctly.

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
* The web UI has improved support for large deployments and deployments that don't have any
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
