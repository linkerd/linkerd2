## stable-2.4.0

* Blurbs
  * edge-19.4.5
    * Auto-inject on by default
    * Introduction of install stages
      * `linkerd install config` and `linkerd install control-plan` (applies
      to `linkerd upgrade` as well)
  * edge-19.6.4
    * Added support for [Traffic Split](https://github.com/deislabs/smi-spec/blob/master/traffic-split.md)

This stable release introduces several new features that help manage the complexity of Kubernetes deployments. 

For more details, see the announcement [blog post](TODO)

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**:

**Special thanks to**: @alenkacz, @codeman9, @dwj300, @jackprice, @liquidslr
@matej-g, @Pothulapati, @zaharidichev, 

**Full release notes**:

* CLI
  * **Breaking Change** Removed the `--proxy-auto-inject` flag, as the
    proxy injector is now always installed
  * **Breaking Change** Replaced the `--linkerd-version` flag with the
    `--proxy-version` flag in the `linkerd install` and `linkerd upgrade`
    commands, which allows setting the version for the injected proxy sidecar
    image, without changing the image versions for the control plane
  * Introduced install stages: `linkerd install config` and
    `linkerd install control-plane`
  * Introduced upgrade stages: `linkerd upgrade config` and
    `linkerd upgrade control-plane`
  * Introduced a new `--from-manifests` flag to `linkerd upgrade` allowing
    manually feeding a previously saved output of `linkerd install` into the
    command, instead of requiring a connection to the cluster to fetch the
    config
  * Introduced a new `--manual` flag to `linkerd inject` to output the proxy
    sidecar container spec
  * Introduced a new `--enable-debug-sidecar` option to `linkerd inject`, that
    injects a debug sidecar to inspect traffic to and from the meshed pod
  * Added a new check for unschedulable pods and PSP issues (thanks, @liquidslr!)
  * Disabled the spinner in `linkerd check` when running without a TTY
  * Ensured the ServiceAccount for the proxy injector is created before its
    Deployment to avoid warnings when installing the proxy injector
    (thanks, @dwj300!)
  * Added a `linkerd check config` command for verifying that
    `linkerd install config` was successful
  * Improved the help documentation of `linkerd install` to clarify flag usage
  * Added support for private Kubernetes clusters by changing the CLI to connect
    to the control plane using a port-forward (thanks, @jackprice!)
  * Fixed `linkerd check` and `linkerd dashboard` failing when any control plane
    pod is not ready, even when multiple replicas exist (as in HA mode)
  * **New** Added a `linkerd edges` command that shows the source and
    destination name and identity for proxied connections, to assist in
    debugging
  * Tap can now be disabled for specific pods during injection by using the
    `--disable-tap` flag, or by using the `config.linkerd.io/disable-tap`
    annotation
  * Introduced pre-install healthcheck for clock skew (thanks, @matej-g!)
  * Added a JSON option to the `linkerd edges` command so that output is
    scripting friendly and can be parsed easily (thanks @alenkacz!)
  * Fixed an issue where, when Linkerd is installed with `--ha`, running
    `linkerd upgrade` without `--ha` will disable the high availability
    control plane
  * Added a `--init-image-version` flag to `linkerd inject` to override the
    injected proxy-init container version
  * Added the `--linkerd-cni-enabled` flag to the `install` subcommands so that
    `NET_ADMIN` capability is omitted from the CNI-enabled control plane's PSP
  * Updated `linkerd check` to validate the caller can create
    `PodSecurityPolicy` resources
  * Added a check to `install` to prevent installing multiple control planes
    into different namespaces
  * Added support for passing a URL directly to `linkerd inject` (thanks
    @Pothulapati!)
  * Added the `--all-namespaces` flag to `linkerd edges`

* Controller
  * Added Go pprof HTTP endpoints to all control plane components' admin
    servers to better assist debugging efforts
  * Fixed bug in the proxy injector, where sporadically the pod workload owner
    wasn't properly determined, which would result in erroneous stats
  * Added support for a new `config.linkerd.io/disable-identity` annotation to
    opt out of identity for a specific pod
  * Fixed pod creation failure when a `ResourceQuota` exists by adding a default
    resource spec for the proxy-init init container
  * Fixed control plane components failing on startup when the Kubernetes API
    returns an `ErrGroupDiscoveryFailed`
  * Added Controller Component Labels to the webhook config resources (thanks,
    @Pothulapati!)
  * Moved the tap service into its own pod
  * **New** Control plane installations now generate a self-signed certificate
    and private key pair for each webhook, to prepare for future work to make
    the proxy injector and service profile validator HA
  * Added a debug container annotation, allowing the `--enable-debug-sidecar`
    flag to work when auto-injecting Linkerd proxies
  * Added multiple replicas for the `proxy-injector` and `sp-validator`
    controllers when run in high availability mode (thanks to @Pothulapati!)
  * Default to least-privilege security context values for the proxy container
    so that auto-inject does not fail on restricted PSPs (thanks @codeman9!)
  * Defined least privilege default security context values for the proxy
    container so that auto-injection does not fail on (thanks @codeman9!)
  * Default the webhook failure policy to `Fail` in order to account for
    unexpected errors during auto-inject; this ensures uninjected applications
    are not deployed
  * Introduced control plane's PSP and RBAC resources into Helm templates;
    these policies are only in effect if the PSP admission controller is
    enabled
  * Removed `UPDATE` operation from proxy-injector webhook because pod
    mutations are disallowed during update operations
  * Default the mutating and validating webhook configurations `sideEffects` 
    property to `None` to indicate that the webhooks have no side effects on
    other resources (thanks @Pothulapati!)
  * Added support for the SMI TrafficSplit API which allows users to define
    traffic splits in TrafficSplit custom resources

* Proxy
  * Replaced the fixed reconnect backoff with an exponential one (thanks,
    @zaharidichev!)
  * Fixed an issue where load balancers can become stuck
  * Added a dispatch timeout that limits the amount of time a request can be
    buffered in the proxy
  * Removed the limit on the number of concurrently active service discovery
    queries to the destination service
  * Fix an epoll notification issue that could cause excessive CPU usage
  * Added the ability to disable tap by setting an env var (thanks,
    @zaharidichev!)
  * Changed the proxy's routing behavior so that, when the control plane does
    not resolve a destination, the proxy forwards the request with minimal
    additional routing logic
  * Fixed a bug in the proxy's HPACK codec that could cause requests with very
    large header values to hang indefinitely
  * Fixed a memory leak that can occur if an HTTP/2 request with a payload
    ends before the entire payload is sent to the destination
  * The `l5d-override-dst` header is now used for inbound service profile
    discovery
  * Include errors in `response_total` metrics
  * Changed the load balancer to require that Kubernetes services are resolved
    via the control plane
  * Added the `NET_RAW` capability to the proxy-init container to be compatible
    with `PodSecurityPolicy`s that use `drop: all`
  * Fixed the proxy rejecting HTTP2 requests that don't have an `:authority`
  * Improved idle service eviction to reduce resource consumption for clients
    that send requests to many services

* Web UI
  * Added the Font Awesome stylesheet locally; this allows both Font Awesome
    and Material-UI sidebar icons to display consistently with no/limited
    internet access (thanks again, @liquidslr!)
  * Removed the Authorities table and sidebar link from the dashboard to prepare
    for a new, improved dashboard view communicating authority data
  * Fixed dashboard behavior that caused incorrect table sorting
  * Removed the "Debug" page from the Linkerd dashboard while the functionality
    of that page is being redesigned
  * Added an Edges table to the resource detail view that shows the source,
    destination name, and identity for proxied connections
  * Improved UI for Edges table in dashboard by changing column names, adding a
    "Secured" icon and showing an empty Edges table in the case of no returned
    edges

* Internal
  * Known container errors were hidden in the integration tests; now they are
    reported in the output without having the tests fail
  * Fixed integration tests by adding known proxy-injector log warning to tests
  * Modified the integration test for `linkerd upgrade` in order to test
    upgrading from the latest stable release instead of the latest edge and reflect the typical use case
  * Moved the proxy-init container to a separate `linkerd/proxy-init` Git
    repository
