## edge-19.4.3

* CLI
  * **Fixed** `linkerd upgrade` command not upgrading proxy containers (thanks
    @jon-walton for the issue report!)
  * **Fixed** `linkerd upgrade` command not installing the identity service when
    it was not already installed
  * Eliminate false-positive vulnerability warnings related to go.uuid

Special thanks to @KatherineMelnyk for updating the web component to read the
UUID from the `linkerd-config` ConfigMap!

## edge-19.4.2

* CLI
  * Removed TLS metrics from the `stat` command; this is in preparation for
    surfacing identity metrics in a clearer way
  * The `upgrade` command now outputs a URL that explains next steps for
    upgrading
  * **Breaking Change:** The `--linkerd-cni-enabled` flag has been removed from
    the `inject` command; CNI is configured at the cluster level with the
    `install` command and no longer applies to the `inject` command
* Controller
  * Service profile validation is now performed via a webhook endpoint; this
    prevents Kubernetes from accepting invalid service profiles
  * Added support for the `config.linkerd.io/proxy-version` annotation on pod
    specs; this will override the injected proxy version
  * Changed the default CPU request from `10m` to `100m` for HA deployments;
    this will help some intermittent liveness/readiness probes from failing due
    to tight resource constraints
* Proxy
  * The `CommonName` field on CSRs is now set to the proxy's identity name
* Web UI
  * Removed TLS columns from the dashboard tables; this is in preparation for
    surfacing identity metrics in a clearer way

## edge-19.4.1

* CLI
  * Introduced an `upgrade` command! This allows an existing Linkerd control plane to be reinstalled or reconfigured; it is particularly useful for automatically reusing flags set in the previous `install` or `upgrade`
  * The `inject` command proxy options are now converted into config annotations; the annotations ensure that these configs are persisted in subsequent resource updates
  * The `stat` command now always shows the number of open TCP connections
  * **Breaking Change** Removed the `--disable-external-profiles` flag from the `install` command; external profiles are now disabled by default and can be enabled with the new `--enable-external-profiles` flag
* Controller
  * The auto-inject admission controller webhook is updated to watch pods creation and update events; with this change, proxy auto-injection now works for all kinds of workloads, including StatefulSets, DaemonSets, Jobs, etc
* Proxy
  * Some `l5d-*` informational headers have been temporarily removed from requests and responses because they could leak information to external clients
* Web UI
  * The topology graph now shows TCP stats if no HTTP stats are available
  * The resource detail page no longer shows blank tables if the resource only has TCP traffic
  * Added validation to the "new service profile" form (thanks @liquidslr!)

## edge-19.3.3

**Significant Update**

This edge release introduces a new TLS Identity system into the default Linkerd
installation, replacing `tls=optional` and the `linkerd-ca` controller. Now,
proxies generate ephemeral private keys into a tmpfs directory and dynamically
refresh certificates, authenticated by Kubernetes ServiceAccount tokens, via the
newly-introduced Identity controller.

Now, all meshed HTTP communication is private and authenticated by default.

* CLI
  * Changed `install` to accept or generate an issuer Secret for the Identity
    controller
  * Changed `install` to fail in the case of a conflict with an existing
    installation; this can be disabled with the `--ignore-cluster` flag
  * Changed `inject` to require fetching a configuration from the control plane;
    this can be disabled with the `--ignore-cluster` and `--disable-identity`
    flags, though this will prevent the injected pods from participating in mesh
    identity
  * Removed the `--tls=optional` flag from the `linkerd install` command, since
    TLS is now enabled by default
  * Added the ability to adjust the Prometheus log level
* Proxy
  * **Fixed** a stream leak between the proxy and the control plane that could
    cause the `linkerd-controller` pod to use an excessive amount of memory
  * Introduced per-proxy private key generation and dynamic certificate renewal
  * Added a readiness check endpoint on `:4191/ready` so that Kubernetes doesn't
    consider pods ready until they have acquired a certificate from the Identity
    controller
  * The proxy's connect timeouts have been updated, especially to improve
    reconnect behavior between the proxy and the control plane
* Web UI
  * Added TCP stats to the Linkerd Pod Grafana dashboard
  * Fixed the behavior of the Top query 'Start' button if a user's query returns
    no data
  * Added stable sorting for table rows
  * Fixed an issue with the order of tables returned from a Top Routes query
  * Added text wrap for paths in the modal for expanded Tap query data
* Internal
  * Improved the `bin/go-run` script for the build process so that on failure,
    all associated background processes are terminated

Special thanks to @liquidslr for many useful UI and log changes, and to @mmalone
and @sourishkrout at @smallstep for collaboration and advice on the Identity
system!

## edge-19.3.2

* Controller
  * **Breaking change** Removed support for running the control plane in
    single-namespace mode, which was severely limited in the number of features
    it supported due to not having access to cluster-wide resources
  * Updated automatic proxy injection and CLI injection to support overriding
    inject defaults via pod spec annotations
  * Added a new public API endpoint for fetching control plane configuration
* CLI
  * **Breaking change** Removed the `--api-port` flag from the `inject` and
    `install` commands, since there's no benefit to running the control plane's
    destination API on a non-default port (thanks, @paranoidaditya)
  * Introduced the `linkerd metrics` command for fetching proxy metrics
  * Updated the `linkerd routes` command to display rows for routes that are not
    receiving any traffic
  * Updated the `linkerd dashboard` command to serve the dashboard on a fixed
    port, allowing it to leverage browser local storage for user settings
* Web UI
  * **New** Added a Community page to surface news and updates from linkerd.io
  * Fixed a quoting issue with service profile downloads (thanks, @liquidslr!)
  * Added a Grafana dashboard and web tables for displaying Job stats
    (thanks, @Pothulapati!)
  * Updated sorting of route table to move default routes to the bottom
  * Added TCP stat tables on the namespace landing page and resource detail page

## edge-19.3.1

* CLI
  * Introduced a check for NET_ADMIN in `linkerd check`
  * Fixed permissions check for CRDs
  * Included kubectl version check as part of `linkerd check` (thanks @yb172!)
  * Added TCP stats to the stat command, under the `-o wide` and `-o json` flags
* Controller
  * Updated the `mutatingwebhookconfiguration` so that it is recreated when the
    proxy injector is restarted, so that the MWC always picks up the latest
    config template during version upgrade
* Proxy
  * Increased the inbound/router cap on MAX_CONCURRENT_STREAMS
  * The `l5d-remote-ip` header is now set on inbound requests and outbound
    responses
* Web UI
  * Fixed sidebar not updating when resources were added/deleted (thanks
    @liquidslr!)
  * Added filter functionality to the metrics tables
* Internal
  * Added more log errors to the integration tests
  * Removed the GOPATH dependence from the CLI dev environment
  * Consolidated injection code from CLI and admission controller code paths

## edge-19.2.5

* CLI
  * Updated `linkerd check` to ensure hint URLs are displayed for RPC checks
* Controller
  * Updated the auto-inject admission controller webhook to respond to UPDATE
    events for deployment workloads
  * Updated destination service to return TLS identities only when the
    destination pod is TLS-aware and is in the same controller namespace
  * Lessen klog level to improve security
  * Updated control-plane components to query Kubernetes at startup to determine
    authorized namespaces and if ServiceProfile support is available
  * Modified the stats payload to include the following TCP stats:
    `tcp_open_connections`, `tcp_read_bytes_total`, `tcp_write_bytes_total`
* Proxy
  * Fixed issue with proxy falling back to filesystem polling due to improperly
    sized inotify buffer
* Web UI
  * Removed 'Help' hierarchy and surfaced links on navigation sidebar
  * Added a Debug page to the web dashboard, allowing you to introspect service discovery state
  * Updated the resource detail page to start displaying a table with TCP stats
* Internal
  * Enabled the following linters: `unparam`, `unconvert`, `goimports`,
    `goconst`, `scopelint`, `unused`, `gosimple`
  * Bumped base Docker images

## stable-2.2.1

This stable release polishes some of the CLI help text and fixes two issues that
came up since the stable-2.2.0 release.

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Full release notes**:

* CLI
  * Fixed handling of kubeconfig server urls that include paths
  * Updated the description of the `--proxy-auto-inject` flag to indicate that
    it is no longer experimental
  * Updated the `profile` help text to match the other commands
  * Added the "ep" alias for the `endpoints` command
* Controller
  * Stopped logging an error when a route doesn't specify a timeout

## edge-19-2.4

* CLI
  * Implemented `--proxy-cpu-limit` and `--proxy-memory-limit` for setting the
    proxy resources limits (`--proxy-cpu` and `--proxy-memory` were deprecated in
    favor of `proxy-cpu-request` and `proxy-memory-request`) (thanks @TwinProduction!)
  * Updated the `inject` and `uninject` subcommands to issue warnings when
    resources lack a `Kind` property (thanks @Pothulapati!)
  * Unhid the `install-cni` command and its flags, and tweaked their descriptions
  * Fixed handling of kubeconfig server urls that include paths
  * Updated the description of the `--proxy-auto-inject` flag to indicate that
    it is no longer experimental
  * Updated the `profile` help text to match the other commands
  * Added the "ep" alias for the `endpoints` command (also @Pothulapati!)
  * Added a validator for the `--proxy-log-level` flag
  * Fixed sporadic (and harmless) race condition error in `linkerd check`
* Controller
  * Instrumented clients in the control plane connecting to Kubernetes, thus
    providing better visibility for diagnosing potential problems with those
    connections
  * Stopped logging an error when a route doesn't specify a timeout
  * Renamed the "linkerd-proxy-api" service to "linkerd-destination"
  * Bumped Prometheus to version 2.7.1 and Grafana to version 5.4.3
* Web UI
  * Modified the Grafana variable queries to use a TCP-based metric, so that
    if there is only TCP traffic then the dropdowns don't end up empty
  * Ensured that all the tooltips in Grafana displaying the series are shared
    across all the graphs
* Internals
  * Added the flags `-update` and `-pretty-diff` to tests to allow overwriting
    fixtures and to print the full text of the fixtures upon mismatches
  * Introduced golangci-lint tooling, using `.golangci.yml` to centralize
    the config
  * Added a `-cover` parameter to track code coverage in go tests
    (more info in TEST.md)
  * Added integration tests for `single-namespace`
  * Renamed a function in a test that was shadowing a go built-in function
    (thanks @huynq0911!)

## stable-2.2.0

This stable release introduces automatic request retries and timeouts, and
graduates auto-inject to be a fully-supported (non-experimental) feature. It
adds several new CLI commands, including `logs` and `endpoints`, that provide
diagnostic visibility into Linkerd's control plane. Finally, it introduces two
exciting experimental features: a cryptographically-secured client identity
header, and a CNI plugin that avoids the need for `NET_ADMIN` kernel
capabilities at deploy time.

For more details, see the announcement blog post:
https://blog.linkerd.io/2019/02/12/announcing-linkerd-2-2/

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: The default behavior for proxy auto injection and service
profile ownership has changed as part of this release. Please see the
[upgrade instructions](https://linkerd.io/2/upgrade/#upgrade-notice-stable-2-2-0)
for more details.

**Special thanks to**: @alenkacz, @codeman9, @jonrichards, @radu-matei, @yeya24,
and @zknill

**Full release notes**:

* CLI
  * Improved service profile validation when running `linkerd check` in order to
    validate service profiles in all namespaces
  * Added the `linkerd endpoints` command to introspect Linkerd's service
    discovery state
  * Added the `--tap` flag to `linkerd profile` to generate service profiles
    using the route results seen during the tap
  * Added support for the `linkerd.io/inject: disabled` annotation on pod specs
    to disable injection for specific pods when running `linkerd inject`
  * Added support for `basePath` in OpenAPI 2.0 files when running `linkerd
    profile --open-api`
  * Increased `linkerd check` client timeout from 5 seconds to 30 seconds to fix
    issues for clusters with slow API servers
  * Updated `linkerd routes` to no longer return rows for `ExternalName`
    services in the namespace
  * Broadened the set of valid URLs when connecting to the Kubernetes API
  * Added the `--proto` flag to `linkerd profile` to output a service profile
    based on a Protobuf spec file
  * Fixed CLI connection failures to clusters that use self-signed certificates
  * Simplified `linkerd install` so that setting up proxy auto-injection
    (flag `--proxy-auto-inject`) no longer requires enabling TLS (flag `--tls`)
  * Added links for each `linkerd check` failure, pointing to a relevant section
    in our new FAQ page with resolution steps for each case
  * Added optional `linkerd install-sp` command to generate service profiles for
    the control plane, providing per-route metrics for control plane components
  * Removed `--proxy-bind-timeout` flag from `linkerd install` and
    `linkerd inject`, as the proxy no longer accepts this environment variable
  * Improved CLI appearance on Windows systems
  * Improved `linkerd check` output, fixed bug with `--single-namespace`
  * Fixed panic when `linkerd routes` is called in single-namespace mode
  * Added `linkerd logs` command to surface logs from any container in the
    Linkerd control plane
  * Added `linkerd uninject` command to remove the Linkerd proxy from a
    Kubernetes config
  * Improved `linkerd inject` to re-inject a resource that already has a Linkerd
    proxy
  * Improved `linkerd routes` to list all routes, including those without
    traffic
  * Improved readability in `linkerd check` and `linkerd inject` outputs
  * Adjusted the set of checks that are run before executing CLI commands, which
    allows the CLI to be invoked even when the control plane is not fully ready
  * Fixed reporting of injected resources when the `linkerd inject` command is
    run on `List` type resources with multiple items
  * Updated the `linkerd dashboard` command to use port-forwarding instead of
    proxying when connecting to the web UI and Grafana
  * Added validation for the `ServiceProfile` CRD
  * Updated the `linkerd check` command to disallow setting both the `--pre` and
    `--proxy` flags simultaneously
  * Added `--routes` flag to the `linkerd top` command, for grouping table rows
    by route instead of by path
  * Updated Prometheus configuration to automatically load `*_rules.yml` files
  * Removed TLS column from the `linkerd routes` command output
  * Updated `linkerd install` output to use non-default service accounts,
    `emptyDir` volume mounts, and non-root users
  * Removed cluster-wide resources from single-namespace installs
  * Fixed resource requests for proxy-injector container in `--ha` installs
* Controller
  * Fixed issue with auto-injector not setting the proxy ID, which is required
    to successfully locate client service profiles
  * Added full stat and tap support for DaemonSets and StatefulSets in the CLI,
    Grafana, and web UI
  * Updated auto-injector to use the proxy log level configured at install time
  * Fixed issue with auto-injector including TLS settings in injected pods even
    when TLS was not enabled
  * Changed automatic proxy injection to be opt-in via the `linkerd.io/inject`
    annotation on the pod or namespace
  * Move service profile definitions to client and server namespaces, rather
    than the control plane namespace
  * Added `linkerd.io/created-by` annotation to the linkerd-cni DaemonSet
  * Added a 10 second keepalive default to resolve dropped connections in Azure
    environments
  * Improved node selection for installing the linkerd-cni DaemonSet
  * Corrected the expected controller identity when configuring pods with TLS
  * Modified klog to be verbose when controller log-level is set to `debug`
  * Added support for retries and timeouts, configured directly in the service
    profile for each route
  * Added an experimental CNI plugin to avoid requiring the NET_ADMIN capability
    when injecting proxies
  * Improved the API for `ListPods`
  * Fixed `GetProfiles` API call not returning immediately when no profile
    exists (resulting in proxies logging warnings)
  * Blocked controller initialization until caches have synced with kube API
  * Fixed proxy-api handling of named target ports in service configs
  * Added parameter to stats API to skip retrieving prometheus stats
* Web UI
  * Updated navigation to link the Linkerd logo back to the Overview page
  * Fixed console warnings on the Top page
  * Grayed-out the tap icon for requests from sources that are not meshed
  * Improved resource detail pages to show all resource types
  * Fixed stats not appearing for routes that have service profiles installed
  * Added "meshed" and "no traffic" badges on the resource detail pages
  * Fixed `linkerd dashboard` to maintain proxy connection when browser open fails
  * Fixed JavaScript bundling to avoid serving old versions after upgrade
  * Reduced the size of the webpack JavaScript bundle by nearly 50%
  * Fixed an indexing error on the top results page
  * Restored unmeshed resources in the network graph on the resource detail page
  * Adjusted label for unknown routes in route tables, added tooltip
  * Updated Top Routes page to persist form settings in URL
  * Added button to create new service profiles on Top Routes page
  * Fixed CLI commands displayed when linkerd is running in non-default
    namespace
* Proxy
  * Modified the way in which canonicalization warnings are logged to reduce the
    overall volume of error logs and make it clearer when failures occur
  * Added TCP keepalive configuration to fix environments where peers may
    silently drop connections
  * Updated the `Get` and `GetProfiles` APIs to accept a `proxy_id` parameter in
    order to return more tailored results
  * Removed TLS fallback-to-plaintext if handshake fails
  * Added the ability to override a proxy's normal outbound routing by adding an
   `l5d-override-dst` header
  * Added `LINKERD2_PROXY_DNS_CANONICALIZE_TIMEOUT` environment variable to
    customize the timeout for DNS queries to canonicalize a name
  * Added support for route timeouts in service profiles
  * Improved logging for gRPC errors and for malformed HTTP/2 request headers
  * Improved log readability by moving some noisy log messages to more verbose
    log levels
  * Fixed a deadlock in HTTP/2 stream reference counts
  * Updated the proxy-init container to exit with a non-zero exit code if
    initialization fails, making initialization errors much more visible
  * Fixed a memory leak due to leaked UDP sockets for failed DNS queries
  * Improved configuration of the PeakEwma load balancer
  * Improved handling of ports configured to skip protocol detection when the
    proxy is running with TLS enabled

## edge-19.2.3

* Controller
  * Fixed issue with auto-injector not setting the proxy ID, which is required
    to successfully locate client service profiles
* Web UI
  * Updated navigation to link the Linkerd logo back to the Overview page
  * Fixed console warnings on the Top page

## edge-19.2.2

* CLI
  * Improved service profile validation when running `linkerd check` in order to
    validate service profiles in all namespaces
* Controller
  * Added stat and tap support for StatefulSets in the CLI, Grafana, and web UI
  * Updated auto-injector to use the proxy log level configured at install time
  * Fixed issue with auto-injector including TLS settings in injected pods even
    when TLS was not enabled
* Proxy
  * Modified the way in which canonicalization warnings are logged to reduce the
    overall volume of error logs and make it clearer when failures occur

## edge-19.2.1

* Controller
  * **Breaking change** Changed automatic proxy injection to be opt-in via the
    `linkerd.io/inject` annotation on the pod or namespace. More info:
    https://linkerd.io/2/proxy-injection/
  * **Breaking change** `ServiceProfile`s are now defined in client and server
    namespaces, rather than the control plane namespace. `ServiceProfile`s
    defined in the client namespace take priority over ones defined in the
    server namespace
  * Added `linkerd.io/created-by` annotation to the linkerd-cni DaemonSet
    (thanks @codeman9!)
  * Added a 10 second keepalive default to resolve dropped connections in Azure
    environments
  * Improved node selection for installing the linkerd-cni DaemonSet (thanks
    @codeman9!)
  * Corrected the expected controller identity when configuring pods with TLS
  * Modified klog to be verbose when controller log-level is set to `Debug`
* CLI
  * Added the `linkerd endpoints` command to introspect Linkerd's service
    discovery state
  * Added the `--tap` flag to `linkerd profile` to generate a `ServiceProfile`
    by using the route results seen during the tap
  * Added support for the `linkerd.io/inject: disabled` annotation on pod specs
    to disable injection for specific pods when running `linkerd inject`
  * Added support for `basePath` in OpenAPI 2.0 files when running `linkerd
    profile --open-api`
  * Increased `linkerd check` client timeout from 5 seconds to 30 seconds to fix
    issues for clusters with a slower API server
  * `linkerd routes` will no longer return rows for `ExternalName` services in
    the namespace
  * Broadened set of valid URLs when connecting to the Kubernetes API
  * Improved `ServiceProfile` field validation in `linkerd check`
* Proxy
  * Added TCP keepalive configuration to fix environments where peers may
    silently drop connections
  * The `Get` and `GetProfiles` API now accept a `proxy_id` parameter in order
    to return more tailored results
  * Removed TLS fallback-to-plaintext if handshake fails

## edge-19.1.4

* Controller
  * Added support for timeouts! Configurable in the service profiles for each route
  * Added an experimental CNI plugin to avoid requiring the NET_ADMIN capability when
    injecting proxies (more details at https://linkerd.io/2/cni) (thanks @codeman9!)
  * Added more improvements to the API for `ListPods` (thanks @alenkacz!)
* Web UI
  * Grayed-out the tap icon for requests from sources that are not meshed
* CLI
  * Added the `--proto` flag to `linkerd profile` to output a service profile
    based on a Protobuf spec file
  * Fixed CLI connection failure to clusters that use self-signed certificates
  * Simplified `linkerd install` so that setting up proxy auto-injection
    (flag `--proxy-auto-inject`) no longer requires enabling TLS (flag `--tls`)
  * Added links for each `linkerd check` failure, pointing to a relevant section
    in our new FAQ page with resolution steps for each case

## edge-19.1.3

* Controller
  * Improved API for `ListPods` (thanks @alenkacz!)
  * Fixed `GetProfiles` API call not returning immediately when no profile
    exists (resulting in proxies logging warnings)
* Web UI
  * Improved resource detail pages now show all resource types
  * Fixed stats not appearing for routes that have service profiles installed
* CLI
  * Added optional `linkerd install-sp` command to generate service profiles for
    the control plane, providing per-route metrics for control plane components
  * Removed `--proxy-bind-timeout` flag from `linkerd install` and `linkerd inject`
    commands, as the proxy no longer accepts this environment variable
  * Improved CLI appearance on Windows systems
  * Improved `linkerd check` output, fixed check bug when using
    `--single-namespace` (thanks to @djeeg for the bug report!)
  * Improved `linkerd stat` now supports DaemonSets (thanks @zknill!)
  * Fixed panic when `linkerd routes` is called in single-namespace mode
* Proxy
  * Added the ability to override a proxy's normal outbound routing by adding an
   `l5d-override-dst` header
  * Added `LINKERD2_PROXY_DNS_CANONICALIZE_TIMEOUT` environment variable to
    customize the timeout for DNS queries to canonicalize a name
  * Added support for route timeouts in service profiles
  * Improved logging for gRPC errors and for malformed HTTP/2 request headers
  * Improved log readability by moving some noisy log messages to more verbose
    log levels

## edge-19.1.2

* Controller
  * Retry support! Introduce an `isRetryable` property to service profiles to
    enable configuring retries on a per-route basis
* Web UI
  * Add "meshed" and "no traffic" badges on the resource detail pages
  * Fix `linkerd dashboard` to maintain proxy connection when browser open fails
  * Fix JavaScript bundling to avoid serving old versions after upgrade
* CLI
  * Add `linkerd logs` command to surface logs from any container in the Linkerd
    control plane (shout out to [Stern](https://github.com/wercker/stern)!)
  * Add `linkerd uninject` command to remove the Linkerd proxy from a Kubernetes
    config
  * Improve `linkerd inject` to re-inject a resource that already has a Linkerd
    proxy
  * Improve `linkerd routes` to list all routes, including those without traffic
  * Improve readability in `linkerd check` and `linkerd inject` outputs
* Proxy
  * Fix a deadlock in HTTP/2 stream reference counts

## edge-19.1.1

* CLI
  * Adjust the set of checks that are run before executing CLI commands, which
    allows the CLI to be invoked even when the control plane is not fully ready
  * Fix reporting of injected resources when the `linkerd inject` command is run
    on `List` type resources with multiple items
  * Update the `linkerd dashboard` command to use port-forwarding instead of
    proxying when connecting to the web UI and Grafana
  * Add validation for the `ServiceProfile` CRD (thanks, @alenkacz!)
  * Update the `linkerd check` command to disallow setting both the `--pre` and
    `--proxy` flags simultaneously (thanks again, @alenkacz!)
* Web UI
  * Reduce the size of the webpack JavaScript bundle by nearly 50%!
  * Fix an indexing error on the top results page
* Proxy
  * **Fixed** The proxy-init container now exits with a non-zero exit code if
    initialization fails, making initialization errors much more visible
  * **Fixed** The proxy previously leaked UDP sockets for failed DNS queries,
    causing a memory leak; this has been fixed

## edge-18.12.4

Upgrade notes: The control plane components have been renamed as of the
edge-18.12.1 release to reduce possible naming collisions. To upgrade an
older installation, see the [Upgrade Guide](https://linkerd.io/2/upgrade/).

* CLI
  * Add `--routes` flag to the `linkerd top` command, for grouping table rows
    by route instead of by path
  * Update Prometheus configuration to automatically load `*_rules.yml` files
  * Remove TLS column from the `linkerd routes` command output
* Web UI
  * Restore unmeshed resources in the network graph on the resource detail page
  * Reduce the overall size of the asset bundle for the web frontend
* Proxy
  * Improve configuration of the PeakEwma load balancer

Special thanks to @radu-matei for cleaning up a whole slew of Go lint warnings,
and to @jonrichards for improving the Rust build setup!

## edge-18.12.3

Upgrade notes: The control plane components have been renamed as of the
edge-18.12.1 release to reduce possible naming collisions. To upgrade an
older installation, see the [Upgrade Guide](https://linkerd.io/2/upgrade/).

* CLI
  * Multiple improvements to the `linkerd install` config (thanks @codeman9!)
    * Use non-default service accounts for grafana and web deployments
    * Use `emptyDir` volume mount for prometheus and grafana pods
    * Set security context on control plane components to not run as root
  * Remove cluster-wide resources from single-namespace installs
    * Disable service profiles in single-namespace mode
    * Require that namespace already exist for single-namespace installs
  * Fix resource requests for proxy-injector container in `--ha` installs
* Controller
  * Block controller initialization until caches have synced with kube API
  * Fix proxy-api handling of named target ports in service configs
  * Add parameter to stats API to skip retrieving prometheus stats (thanks,
    @alpeb!)
* Web UI
  * Adjust label for unknown routes in route tables, add tooltip
  * Update Top Routes page to persist form settings in URL
  * Add button to create new service profiles on Top Routes page
  * Fix CLI commands displayed when linkerd is running in non-default namespace
* Proxy
  * Proxies with TLS enabled now honor ports configured to skip protocol detection

## stable-2.1.0

This stable release introduces several major improvements, including per-route
metrics, service profiles, and a vastly improved dashboard UI. It also adds
several significant experimental features, including proxy auto-injection,
single namespace installs, and a high-availability mode for the control plane.

For more details, see the announcement blog post:
https://blog.linkerd.io/2018/12/06/announcing-linkerd-2-1/

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: The control plane components have been renamed in this
release to reduce possible naming collisions. Please make sure to read the
[upgrade instructions](https://linkerd.io/2/upgrade/#upgrade-notice-stable-2-1-0)
if you are upgrading from the `stable-2.0.0` release.

**Special thanks to**: @alenkacz, @alpeb, @benjdlambert, @fahrradflucht,
@ffd2subroutine, @hypnoglow, @ihcsim, @lucab, and @rochacon

**Full release notes**:
* CLI
  * `linkerd routes` command displays per-route stats for *any resource*
  * Service profiles are now supported for external authorities
  * `linkerd routes --open-api` flag generates a service profile
    based on an OpenAPI specification (swagger) file
  * `linkerd routes` command displays per-route stats for services with
    service profiles
  * Add `--ha` flag to `linkerd install` command, for HA
    deployment of the control plane
  * Update stat command to accept multiple stat targets
  * Fix authority stat filtering when the `--from` flag is present
  * Various improvements to check command, including:
    * Emit warnings instead of errors when not running the latest version
    * Add retries if control plane health check fails initially
    * Run all pre-install RBAC checks, instead of stopping at first failure
  * Fixed an issue with the `--registry` install flag not accepting
    hosts with ports
  * Added an `--output` stat flag, for printing stats as JSON
  * Updated the `top` table to set column widths dynamically
  * Added a `--single-namespace` install flag for installing
    the control plane with Role permissions instead of ClusterRole permissions
  * Added a `--proxy-auto-inject` flag to the `install` command,
    allowing for auto-injection of sidecar containers
  * Added `--proxy-cpu` and `--proxy-memory` flags to the `install`
    and `inject` commands, giving the ability to configure CPU + Memory requests
  * Added a `--context` flag to specify the context to use to talk
    to the Kubernetes apiserver
  * The namespace in which Linkerd is installed is configurable via the
    `LINKERD_NAMESPACE` env var, in addition to the `--linkerd-namespace` flag
  * The wait time for the `check` and `dashboard` commands is
    configurable via the `--wait` flag
  * The `top` command now aggregates by HTTP method as well
* Controller
  * Rename snake case fields to camel case in service profile spec
  * Controller components are now prefixed with `linkerd-` to
    prevent name collisions with existing resources
  * `linkerd install --disable-h2-upgrade` flag has been added to
    control automatic HTTP/2 upgrading
  * Fix auto injection issue on Kubernetes `v1.9.11` that would
    merge, rather than append, the proxy container into the application
  * Fixed a few issues with auto injection via the proxy-injector webhook:
    * Injected pods now execute the linkerd-init container last, to avoid
      rerouting requests during pod init
    * Original pod labels and annotations are preserved when auto-injecting
  * CLI health check now uses unified endpoint for data plane checks
  * Include Licence files in all Docker images
* Proxy
  * The proxy's `tap` subsystem has been reimplemented to be more
    efficient and and reliable
    * The proxy now supports route metadata in tap queries and events
  * A potential HTTP/2 window starvation bug has been fixed
  * Prometheus counters now wrap properly for values greater than 2^53
  * Add controller client metrics, scoped under `control_`
  * Canonicalize outbound names via DNS for inbound profiles
  * Fix routing issue when a pod makes a request to itself
  * Only include `classification` label on `response_total` metric
  * Remove panic when failing to get remote address
  * Better logging in TCP connect error messages
* Web UI
  * Top routes page, served at `/routes`
  * Route metrics are now available in the resource detail pages for
    services with configured profiles
  * Service profiles can be created and downloaded from the Web UI
  * Top Routes page, served at `/routes`
  * Fixed a smattering of small UI issues
  * Added a new Grafana dashboard for authorities
  * Revamped look and feel of the Linkerd dashboard by switching
    component libraries from antd to material-ui
  * Added a Help section in the sidebar containing useful links
  * Tap and Top pages
    * Added clear button to query form
  * Resource Detail pages
    * Limit number of resources shown in the graph
  * Resource Detail page
    * Better rendering of the dependency graph at the top of the page
    * Unmeshed sources are now populated in the Inbound traffic table
    * Sources and destinations are aligned in the popover
  * Tap and Top pages
    * Additional validation and polish for the form controls
    * The top table clears older results when a new top call is started
    * The top table now aggregates by HTTP method as well

## edge-18.12.2

Upgrade notes: The control plane components have been renamed as of the
edge-18.12.1 release to reduce possible naming collisions. To upgrade an
older installation, see the [Upgrade Guide](https://linkerd.io/2/upgrade/).

* Controller
  * Rename snake case fields to camel case in service profile spec

## edge-18.12.1

Upgrade notes: The control plane components have been renamed in this release to
reduce possible naming collisions. To upgrade an existing installation:

* Install new CLI: `curl https://run.linkerd.io/install-edge | sh`
* Install new control plane: `linkerd install | kubectl apply -f -`
* Remove old deploys/cms:
  `kubectl -n linkerd get deploy,cm -oname | grep -v linkerd | xargs kubectl -n linkerd delete`
* Re-inject your applications: `linkerd inject my-app.yml | kubectl apply -f -`
* Remove old services:
  `kubectl -n linkerd get svc -oname | grep -v linkerd | xargs kubectl -n linkerd delete`

For more information, see the [Upgrade Guide](https://linkerd.io/2/upgrade/).

* CLI
  * **Improved** `linkerd routes` command displays per-route stats for *any resource*!
  * **New** Service profiles are now supported for external authorities!
  * **New** `linkerd routes --open-api` flag generates a service profile
    based on an OpenAPI specification (swagger) file
* Web UI
  * **New** Top routes page, served at `/routes`
  * **New** Route metrics are now available in the resource detail pages for
    services with configured profiles
  * **New** Service profiles can be created and downloaded from the Web UI
* Controller
  * **Improved** Controller components are now prefixed with `linkerd-` to
    prevent name collisions with existing resources
  * **New** `linkerd install --disable-h2-upgrade` flag has been added to
    control automatic HTTP/2 upgrading
* Proxy
  * **Improved** The proxy's `tap` subsystem has been reimplemented to be more
    efficient and and reliable
    * The proxy now supports route metadata in tap queries and events
  * **Fixed** A potential HTTP/2 window starvation bug has been fixed
  * **Fixed** Prometheus counters now wrap properly for values greater than
    2^53 (thanks, @lucab!)

## edge-18.11.3

* CLI
  * **New** `linkerd routes` command displays per-route stats for services with
    service profiles
  * **Experimental** Add `--ha` flag to `linkerd install` command, for HA
    deployment of the control plane (thanks @benjdlambert!)
* Web UI
  * **Experimental** Top Routes page, served at `/routes`
* Controller
  * **Fixed** Fix auto injection issue on Kubernetes `v1.9.11` that would
    merge, rather than append, the proxy container into the application
* Proxy
  * **Improved** Add controller client metrics, scoped under `control_`
  * **Improved** Canonicalize outbound names via DNS for inbound profiles

## edge-18.11.2

* CLI
  * **Improved** Update stat command to accept multiple stat targets
  * **Fixed** Fix authority stat filtering when the `--from` flag is present
  * Various improvements to check command, including:
    * Emit warnings instead of errors when not running the latest version
    * Add retries if control plane health check fails initially
    * Run all pre-install RBAC checks, instead of stopping at first failure
* Proxy / Proxy-Init
  * **Fixed** Fix routing issue when a pod makes a request to itself (#1585)
  * Only include `classification` label on `response_total` metric

## edge-18.11.1

* Proxy
  * **Fixed** Remove panic when failing to get remote address
  * **Improved** Better logging in TCP connect error messages
* Web UI
  * **Improved** Fixed a smattering of small UI issues

## edge-18.10.4

This release includes a major redesign of the web frontend to make use of the
Material design system. Additional features that leverage the new design are
coming soon! This release also includes the following changes:

* CLI
  * **Fixed** Fixed an issue with the `--registry` install flag not accepting
    hosts with ports (thanks, @alenkacz!)
* Web UI
  * **New** Added a new Grafana dashboard for authorities (thanks, @alpeb!)
  * **New** Revamped look and feel of the Linkerd dashboard by switching
    component libraries from antd to material-ui

## edge-18.10.3

* CLI
  * **New** Added an `--output` stat flag, for printing stats as JSON
  * **Improved** Updated the `top` table to set column widths dynamically
  * **Experimental** Added a `--single-namespace` install flag for installing
    the control plane with Role permissions instead of ClusterRole permissions
* Controller
  * Fixed a few issues with auto injection via the proxy-injector webhook:
    * Injected pods now execute the linkerd-init container last, to avoid
      rerouting requests during pod init
    * Original pod labels and annotations are preserved when auto-injecting
* Web UI
  * **New** Added a Help section in the sidebar containing useful links

## edge-18.10.2

This release brings major improvements to the CLI as described below, including
support for auto-injecting deployments via a Kubernetes Admission Controller.
Proxy auto-injection is **experimental**, and the implementation may change
going forward.

* CLI
  * **New** Added a `--proxy-auto-inject` flag to the `install` command,
    allowing for auto-injection of sidecar containers (Thanks @ihcsim!)
  * **Improved** Added `--proxy-cpu` and `--proxy-memory` flags to the `install`
    and `inject` commands, giving the ability to configure CPU + Memory requests
    (Thanks @benjdlambert!)
  * **Improved** Added a `--context` flag to specify the context to use to talk
    to the Kubernetes apiserver (Thanks @ffd2subroutine!)

## edge-18.10.1

* Web UI
  * **Improved** Tap and Top pages
    * Added clear button to query form
  * **Improved** Resource Detail pages
    * Limit number of resources shown in the graph
* Controller
  * CLI health check now uses unified endpoint for data plane checks
  * Include Licence files in all Docker images

Special thanks to @alenkacz for contributing to this release!

## edge-18.9.3

* Web UI
  * **Improved** Resource Detail page
    * Better rendering of the dependency graph at the top of the page
    * Unmeshed sources are now populated in the Inbound traffic table
    * Sources and destinations are aligned in the popover
  * **Improved** Tap and Top pages
    * Additional validation and polish for the form controls
    * The top table clears older results when a new top call is started
    * The top table now aggregates by HTTP method as well
* CLI
  * **New** The namespace in which Linkerd is installed is configurable via the
    `LINKERD_NAMESPACE` env var, in addition to the `--linkerd-namespace` flag
  * **New** The wait time for the `check` and `dashboard` commands is
    configurable via the `--wait` flag
  * **Improved** The `top` command now aggregates by HTTP method as well

Special thanks to @rochacon, @fahrradflucht and @alenkacz for contributing to
this release!

## stable-2.0.0

## edge-18.9.2

* **New** _edge_ and _stable_ release channels
* Web UI
  * **Improved** Tap & Top UIs with better layout and linking
* CLI
  * **Improved** `check --pre` command verifies the caller has sufficient
    permissions to install Linkerd
  * **Improved** `check` command verifies that Prometheus has data for proxied
    pods
* Proxy
  * **Fix** `hyper` crate dependency corrects HTTP/1.0 Keep-Alive behavior

## v18.9.1

* Web UI
  * **New** Default landing page provides namespace overview with expandable
    sections
  * **New** Breadcrumb navigation at the top of the dashboard
  * **Improved** Tap and Top pages
    * Table rendering performance improvements via throttling
    * Tables now link to resource detail pages
    * Tap an entire namespace when no resource is specified
    * Tap websocket errors provide more descriptive text
    * Consolidated source and destination columns
  * Misc ui updates
    * Metrics tables now include a small success rate chart
    * Improved latency formatting for seconds latencies
    * Renamed upstream/downstream to inbound/outbound
    * Sidebar scrolls independently from main panel, scrollbars hidden when not
      needed
    * Removed social links from sidebar
* CLI
  * **New** `linkerd check` now validates Linkerd proxy versions and readiness
  * **New** `linkerd inject` now provides an injection status report, and warns
    when resources are not injectable
  * **New** `linkerd top` now has a `--hide-sources` flag, to hide the source
    column and collapse top results accordingly
* Control Plane
  * Updated Prometheus to v2.4.0, Grafana to 5.2.4

## v18.8.4

* Web UI
  * **Improved** Tap and Top now have a better sampling rate
  * **Fixed** Missing sidebar headings now appear

## v18.8.3

* Web UI
  * **Improved** Kubernetes resource navigation in the sidebar
  * **Improved** resource detail pages:
    * **New** live request view
    * **New** success rate graphs
* CLI
  * `tap` and `top` have been improved to sample up to 100 RPS
* Control plane
  * Injected proxy containers now have readiness and liveness probes enabled

Special thanks to @sourishkrout for contributing a web readibility fix!

## v18.8.2

* CLI
  * **New** `linkerd top` command has been added, displays live traffic stats
  * `linkerd check` has been updated with additional checks, now supports a
    `--pre` flag for running pre-install checks
  * `linkerd check` and `linkerd dashboard` now support a `--wait` flag that
    tells the CLI to wait for the control plane to become ready
  * `linkerd tap` now supports a `--output` flag to display output in a wide
    format that includes src and dst resources and namespaces
  * `linkerd stat` includes additional validation for command line inputs
  * All commands that talk to the Linkerd API now show better error messages
    when the control plane is unavailable
* Web UI
  * **New** individual resources can now be viewed on a resource detail page,
    which includes stats for the resource itself and its nearest neighbors
  * **Experimental** web-based Top interface accessible at `/top`, aggregates
    tap data in real time to display live traffic stats
  * The `/tap` page has multiple improvements, including displaying additional
    src/dst metadata, improved form controls, and better latency formatting
  * All resource tables have been updated to display meshed pod counts, as well
    as an icon linking to the resource's Grafana dashboard if it is meshed
  * The UI now shows more useful information when server errors are encountered
* Proxy
  * The `h2` crate fixed a HTTP/2 window management bug
  * The `rustls` crate fixed a bug that could improperly fail TLS streams
* Control Plane
  * The tap server now hydrates metadata for both sources and destinations

## v18.8.1

* Web UI
  * **New** Tap UI makes it possible to query & inspect requests from the browser!
* Proxy
  * **New** Automatic, transparent HTTP/2 multiplexing of HTTP/1 traffic
    reduces the cost of short-lived HTTP/1 connections
* Control Plane
  * **Improved** `linkerd inject` now supports injecting all resources in a folder
  * **Fixed** `linkerd tap` no longer crashes when there are many pods
  * **New** Prometheus now only scrapes proxies belonging to its own linkerd install
  * **Fixed** Prometheus metrics collection for clusters with >100 pods

Special thanks to @ihcsim for contributing the `inject` improvement!

## v18.7.3

Linkerd2 v18.7.3 completes the rebranding from Conduit to Linkerd2, and improves
overall performance and stability.

* Proxy
  * **Improved** CPU utilization by ~20%
* Web UI
  * **Experimental** `/tap` page now supports additional filters
* Control Plane
  * Updated all k8s.io dependencies to 1.11.1

## v18.7.2

Linkerd2 v18.7.2 introduces new stability features as we work toward production
readiness.

* Control Plane
  * **Breaking change** Injected pod labels have been renamed to be more
    consistent with Kubernetes; previously injected pods must be re-injected
    with new version of linkerd CLI in order to work with updated control plane
  * The "ca-bundle-distributor" deployment has been renamed to "ca"
* Proxy
  * **Fixed** HTTP/1.1 connections were not properly reused, leading to
    elevated latencies and CPU load
  * **Fixed** The `process_cpu_seconds_total` was calculated incorrectly
* Web UI
  * **New** per-namespace application topology graph
  * **Experimental** web-based Tap interface accessible at  `/tap`
  * Updated favicon to the Linkerd logo

## v18.7.1

Linkerd2 v18.7.1 is the first release of the Linkerd2 project, which was
formerly hosted at github.com/runconduit/conduit.

* Packaging
  * Introduce new date-based versioning scheme, `vYY.M.n`
  * Move all Docker images to `gcr.io/linkerd-io` repo
* User Interface
  * Update branding to reference Linkerd throughout
  * The CLI is now called `linkerd`
* Production Readiness
  * Fix issue with Destination service sending back incomplete pod metadata
  * Fix high CPU usage during proxy shutdown
  * ClusterRoles are now unique per Linkerd install, allowing multiple instances
    to be installed in the same Kubernetes cluster

## v0.5.0

Conduit v0.5.0 introduces a new, experimental feature that automatically
enables Transport Layer Security between Conduit proxies to secure
application traffic. It also adds support for HTTP protocol upgrades, so
applications that use WebSockets can now benefit from Conduit.

* Security
  * **New** `conduit install --tls=optional` enables automatic, opportunistic
    TLS. See [the docs][auto-tls] for more info.
* Production Readiness
  * The proxy now transparently supports HTTP protocol upgrades to support, for
    instance, WebSockets.
  * The proxy now seamlessly forwards HTTP `CONNECT` streams.
  * Controller services are now configured with liveness and readiness probes.
* User Interface
  * `conduit stat` now supports a virtual `authority` resource that aggregates
    traffic by the `:authority` (or `Host`) header of an HTTP request.
  * `dashboard`, `stat`, and `tap` have been updated to describe TLS state for
    traffic.
  * `conduit tap` now has more detailed information, including the direction of
    each message (outbound or inbound).
  * `conduit stat` now more-accurately records histograms for low-latency services.
  * `conduit dashboard` now includes error messages when a Conduit-enabled pod fails.
* Internals
  * Prometheus has been upgraded to v2.3.1.
  * A potential live-lock has been fixed in HTTP/2 servers.
  * `conduit tap` could crash due to a null-pointer access. This has been fixed.

[auto-tls]: docs/automatic-tls.md

## v0.4.4

Conduit v0.4.4 continues to improve production suitability and sets up internals for the
upcoming v0.5.0 release.

* Production Readiness
  * The destination service has been mostly-rewritten to improve safety and correctness,
    especially during controller initialization.
  * Readiness and Liveness checks have been added for some controller components.
  * RBAC settings have been expanded so that Prometheus can access node-level metrics.
* User Interface
  * Ad blockers like uBlock prevented the Conduit dashboard from fetching API data. This
    has been fixed.
  * The UI now highlights pods that have failed to start a proxy.
* Internals
  * Various dependency upgrades, including Rust 1.26.2.
  * TLS testing continues to bear fruit, precipitating stability improvements to
    dependencies like Rustls.

Special thanks to @alenkacz for improving docker build times!

## v0.4.3

Conduit v0.4.3 continues progress towards production readiness. It features a new
latency-aware load balancer.

* Production Readiness
  * The proxy now uses a latency-aware load balancer for outbound requests. This
    implementation is based on Finagle's Peak-EWMA balancer, which has been proven to
    significantly reduce tail latencies. This is the same load balancing strategy used by
    Linkerd.
* User Interface
  * `conduit stat` is now slightly more predictable in the way it outputs things,
    especially for commands like `watch conduit stat all --all-namespaces`.
  * Failed and completed pods are no longer shown in stat summary results.
* Internals
  * The proxy now supports some TLS configuration, though these features remain disabled
    and undocumented pending further testing and instrumentation.

Special thanks to @ihcsim for contributing his first PR to the project and to @roanta for
discussing the Peak-EWMA load balancing algorithm with us.

## v0.4.2

Conduit v0.4.2 is a major step towards production readiness. It features a wide array of
fixes and improvements for long-running proxies, and several new telemetry features. It
also lays the groundwork for upcoming releases that introduce mutual TLS everywhere.

* Production Readiness
  * The proxy now drops metrics that do not update for 10 minutes, preventing unbounded
    memory growth for long-running processes.
  * The proxy now constrains the number of services that a node can route to
    simultaneously (default: 100). This protects long-running proxies from consuming
    unbounded resources by tearing down the longest-idle clients when the capacity is
    reached.
  * The proxy now properly honors HTTP/2 request cancellation.
  * The proxy could incorrectly handle requests in the face of some connection errors.
    This has been fixed.
  * The proxy now honors DNS TTLs.
  * `conduit inject` now works with `statefulset` resources.
* Telemetry
  * **New** `conduit stat` now supports the `all` Kubernetes resource, which
    shows traffic stats for all Kubernetes resources in a namespace.
  * **New** the Conduit web UI has been reorganized to provide namespace overviews.
  * **Fix** a bug in Tap that prevented the proxy from simultaneously satisfying more than
    one Tap request.
  * **Fix** a bug that could prevent stats from being reported for some TCP streams in
    failure conditions.
  * The proxy now measures response latency as time-to-first-byte.
* Internals
  * The proxy now supports user-friendly time values (e.g. `10s`) from environment
    configuration.
  * The control plane now uses client for Kubernetes 1.10.2.
  * Much richer proxy debug logging, including socket and stream metadata.
  * The proxy internals have been changed substantially in preparation for TLS support.

Special thanks to @carllhw, @kichristensen, & @sfroment for contributing to this release!

### Upgrading from v0.4.1

When upgrading from v0.4.1, we suggest that the control plane be upgraded to v0.4.2 before
injecting application pods to use v0.4.2 proxies.

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
