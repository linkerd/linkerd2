# Changes

## edge-21.4.4

This edge release further consolidates the control plane by removing the
linkerd-controller deployment and moving the sp-validator container into the
destination deployment.

Annotation inheritance has been added so that all Linkerd annotations
on a namespace resource will be inherited by pods within that namespace.
In addition, the `config.linkerd.io/proxy-await` annotation has been added which
enables the [linkerd-await](https://github.com/linkerd/linkerd-await)
functionality by default, simplifying the implementation of the await behavior.
Setting the annotation value to disabled will prevent this behavior.

Some of the `linkerd check` functionality has been updated. The command
ensures that annotations and labels are properly located in the YAML and adds
proxy checks for the control plane and extension pods.

Finally, the nginx container has been removed from the Multicluster gateway pod,
which will impact upgrades. Please see the note below.

**Upgrade note:** When the Multicluster extension is updated in both of the
source and target clusters there won't be any downtime because this change only
affects the readiness probe. The multicluster links must be re-generated with
the `linkerd mc link` command and the `linkerd mc gateways` will show
the target cluster as not alive until the `linkerd mc link` command is re-run,
however that shouldn't affect existing endpoints pointing to the target cluster.

* Added proxy checks for core control plane and extension pods
* Added support for awaiting proxy readiness using an annotation
* Added namespace annotation inheritance to pods
* Removed the linkerd-controller pod
* Moved sp-validator container into the destination deployment
* Added check verifying that labels and annotations are not mixed up
  (thanks @szymongib)
* Enabled support for extra initContainers to the linkerd-cni daemonset
  (thanks @mhulscher!)
* Removed nginx container from multicluster gateway pod
* Added an error message when there is nothing to uninstall

## stable-2.10.1

This stable release adds CLI support for Apple Silicon M1 chips and support for
SMI's TrafficSplit `v1alpha2`.

There are several proxy fixes: handling `FailedPrecondition` errors gracefully,
inbound TLS detection from non-meshed workloads, and using the correct cached
client when the proxy is in ingress mode. The logging infrastructure has also
been improved to reduce memory pressure in high-connection environments.

On the control-plane side, there have been several improvements to the
destination service such as support for Host IP lookups and ignoring pods
in "Terminating" state. It also updates the proxy-injector to add opaque ports
annotation to pods if their namespace has it set.

On the CLI side, `linkerd repair` has been updated to be aware about the control-plane
version and suggest the relevant version to generate the right config. Various
bugs have been fixed around `linkerd identity`, etc.

**Upgrade notes**: Please refer [2.10 upgrade instructions](https://linkerd.io/2/tasks/upgrade/#upgrade-notice-stable-2100)
if you are upgrading from `2.9.x` or below versions.

* Proxy:
  * Fixed an issue where proxies could infinitely retry failed requests to the
    `destination` controller when it returned a `FailedPrecondition`
  * The proxy's logging infrastructure has been updated to reduce memory pressure
    in high-connection environments.
  * Fixed a caching issue in the outbound proxy that would cause it to
    forward traffic to the wrong pod when running in ingress mode.
  * Fixed an issue where inbound TLS detection from non-meshed workloads
    could break
  * Fixed an issue where the admin server's HTTP detection would fail and
    not recover; these are now handled gracefully and without logging warnings
  * Control plane proxies no longer emit warnings about the resolution stream ending.
    This error was innocuous.
  * Bumped the proxy-init image to v1.3.11 which updates the go version to be 1.16.2

* Control Plane:
  * Fixed an issue where the destination service would respond with too big of a
    header and result in http2 protocol errors
  * Fixed an issue where the destination control plane component sometimes returned
    endpoint addresses with a 0 port number while pods were undergoing a rollout
    (thanks @riccardofreixo!)
  * Fixed an issue where pod lookups by host IP and host port fail even though
    the cluster has a matching pod
  * Updated the IP Watcher in destination to ignore pods in "Terminating" state
    (thanks @Wenliang-CHEN!)
  * Modified the proxy-injector to add the opaque ports annotation to pods
    if their namespace has it set
  * Added Support for TrafficSplit `v1alpha2`
  * Updated all the control-plane components to use go `1.16.2`.

* CLI:
  * Fixed an issue where the linkerd identity command returned the root
    certificate of a pod instead of its leaf certificates
  * Fixed an issue where the destination service would respond with too
    big of a header and result in http2 protocol errors
  * Updated the release process to build Linkerd CLI binaries for Apple
    Silicon M1 chips
  * Improved error messaging when trying to install Linkerd on a cluster
    that already had Linkerd installed
  * Added a loading spinner to the linkerd check command when running
    extension checks
  * Added installNamespace toggle in the jaeger extension's install.
    (thanks @jijeesh!)
  * Updated healthcheck pkg to have hintBaseURL configurable, useful
    for external extensions using that pkg
  * Fixed TCP read and write bytes/sec calculations to group by label
    based off inbound or outbound traffic
  * Fixed an issue in linkerd inject where the wrong annotation would
    be added when using --ingress flag
  * Updated `linkerd repair` to be aware of the client and server versions
  * Updated `linkerd uninstall` to print error message when there are no
    resources to uninstall.

* Helm:
  * Aligned the Helm installation heartbeat schedule to match that of the CLI

* Viz:
  * Fixed an issue where the topology graph in the dashboard was no
    longer draggable.
  * Updated dashboard build to use webpack v5
  * Added CA certs to the Viz extension's metrics-api container so
    that it can validate the certifcate of an external Prometheus
  * Removed components from the control plane dashboard that now
    are part of the Viz extension
  * Changed web's base image from debian to scratch

* Multicluster:
  * Fixed an issue with Multicluster's service mirror where its endpoint
    repair retries were not properly rate limited

* Jaeger:
  * Fixed components in the Jaeger extension to set the correct Prometheus
    scrape values

## edge-21.4.3

This edge supersedes `edge-21.4.2` as a release candidate for `stable-2.10.1`!

This release adds support for TrafficSplit `v1alpha2`. Additionally, It includes
improvements to the web and `proxy-init` images.

* Added Support for TrafficSplit `v1alpha2`
* Changed web base image from debian to scratch
* Bumped the `proxy-init` image to `v1.3.11` which updates
  the go version to be `1.16.2`

## edge-21.4.2

This edge release is another candidate for `stable-2.10.1`!

It includes some CLI fixes and addresses an issue where the outbound proxy
would forward traffic to the wrong pod when running in ingress mode.

Thank you to all of our users that have helped test and identify issues in 2.10!

* Fixed an issue in `linkerd inject` where the wrong annotation would be
  added when using `--ingress` flag
* Fixed a nil pointer dereference in `linkerd repair` caused by a mismatch
  between CLI and server versions
* Removed an unnecessary error handling condition in multicluster check
  (thanks @wangchenglong01!)
* Fixed a caching issue in the outbound proxy that would cause it to
  forward traffic to the wrong pod when running in ingress mode.
* Removed unsupported `matches` field from TrafficSplit CRD

## edge-21.4.1

This is a release candidate for `stable-2.10.1`!

This includes several fixes for the core installation as well the Multicluster,
Jaeger, and Viz extensions. There are two significant proxy fixes that address
TLS detection and admin server failures.

Thanks to all our 2.10 users who helped discover these issues!

* Fixed TCP read and write bytes/sec calculations to group by label based off
  inbound or outbound traffic
* Updated dashboard build to use webpack v5
* Modified the proxy-injector to add the opaque ports annotation to pods if
  their namespace has it set
* Added CA certs to the Viz extension's `metrics-api` container so that it can
  validate the certifcate of an external Prometheus
* Fixed an issue where inbound TLS detection from non-meshed workloads could
  break
* Fixed an issue where the admin server's HTTP detection would fail and not
  recover; these are now handled gracefully and without logging warnings
* Aligned the Helm installation heartbeat schedule to match that of the CLI
* Fixed an issue with Multicluster's serivce mirror where it's endpoint repair
  retries were not properly rate limited
* Removed components from the control plane dashboard that now are part of the
  Viz extension
* Fixed components in the Jaeger extension to set the correct Prometheus scrape
  values

## edge-21.3.4

This release fixes some issues around publishing of CLI binary
for Apple Silicon M1 Chips. This release also includes some fixes and
improvements to the dashboard, destination, and the CLI.

* Fixed an issue where the topology graph in the dashboard was no longer
  draggable
* Updated the IP Watcher in destination to ignore pods in "Terminating" state
  (thanks @Wenliang-CHEN!)
* Added `installNamespace` toggle in the jaeger extension's install.
  (thanks @jijeesh!)
* Updated `healthcheck` pkg to have `hintBaseURL` configurable, useful
  for external extensions using that pkg
* Added multi-arch support for RabbitMQ integration tests (thanks @barkardk!)

## edge-21.3.3

This release includes various bug fixes and improvements to the CLI, the
identity and destination control plane components as well as the proxy. This
release also ships with a new CLI binary for Apple Silicon M1 chips.

* Added new RabbitMQ integration tests (thanks @barkardk!)
* Updated the Go version to 1.16.2
* Fixed an issue where the `linkerd identity` command returned the root
  certificate of a pod instead of its leaf certificate
* Fixed an issue where the destination service would respond with too big of a
  header and result in http2 protocol errors
* Updated the release process to build Linkerd CLI binaries for Apple Silicon
  M1 chips
* Improved error messaging when trying to install Linkerd on a cluster that
  already had Linkerd installed
* Fixed an issue where the `destination` control plane component sometimes
  returned endpoint addresses with a `0` port number while pods were
  undergoing a rollout (thanks @riccardofreixo!)
* Added a loading spinner to the `linkerd check` command when running extension
  checks
* Fixed an issue where pod lookups by host IP and host port fail even though
  the cluster has a matching pod
* Control plane proxies no longer emit warnings about the resolution stream
  ending. This error was innocuous.
* Fixed an issue where proxies could infinitely retry failed requests to the
  `destination` controller when it returned a `FailedPrecondition`
* The proxy's logging infrastructure has been updated to reduce memory pressure
  in high-connection environments.

## stable-2.10.0

This release introduces Linkerd extensions. The default control plane no longer
includes Prometheus, Grafana, the dashboard, or several other components that
previously shipped by default.  This results in a much smaller and simpler set
of core functionalities.  Visibility and metrics functionality is now available
in the Viz extension under the `linkerd viz` command.  Cross-cluster
communication functionality is now available in the Multicluster extension
under the `linkerd multicluster` command.  Distributed tracing functionality is
now available in the Jaeger extension under the `linkerd jaeger` command.

This release also introduces the ability to mark certain ports as "opaque",
indicating that the proxy should treat the traffic as opaque TCP instead of
attempting protocol detection.  This allows the proxy to provide TCP metrics
and mTLS for server-speaks-first protocols.  It also enables support for
TCP traffic in the Multicluster extension.

**Upgrade notes**: Please see the [upgrade
instructions](https://linkerd.io/2/tasks/upgrade/#upgrade-notice-stable-2100).

* Proxy
  * Updated the proxy to use TLS version 1.3; support for TLS 1.2 remains
    enabled for compatibility with prior proxy versions
  * Improved support for server-speaks-first protocols by allowing ports to be
    marked as opaque, causing the proxy to skip protocol detection.  Ports can
    be marked as opaque by setting the `config.linkerd.io/opaque-ports`
    annotation on the Pod and Service or by using the `--opaque-ports` flag with
    `linkerd inject`
  * Ports `25,443,587,3306,5432,11211` have been removed from the default skip
    ports; all traffic through those ports is now proxied and handled opaquely
    by default
  * Fixed an issue that could cause proxies in "ingress mode"
    (`linkerd.io/inject: ingress`) to use an excessive amount of memory
  * Improved diagnostic logging around "fail fast" and "max-concurrency
    exhausted" error messages
  * Added a new `/shutdown` admin endpoint that may only be accessed over the
    loopback network allowing batch jobs to gracefully terminate the proxy on
    completion

* Control Plane
  * Removed all components and functionality related to visibility, tracing,
    or multicluster.  These have been moved into extensions
  * Changed the identity controller to receive the trust anchor via environment
    variable instead of by flag; this allows the certificate to be loaded from a
    config map or secret (thanks @mgoltzsche!)
  * Added PodDisruptionBudgets to the control plane components so that they
    cannot be all terminated at the same time during disruptions
    (thanks @tustvold!)

* CLI
  * Changed the `check` command to include each installed extension's `check`
    output; this allows users to check for proper configuration and installation
    of Linkerd without running a command for each extension
  * Moved the `metrics`, `endpoints`, and `install-sp` commands into subcommands
    under the `diagnostics` command
  * Added an `--opaque-ports` flag to `linkerd inject` to easily mark ports
    as opaque.
  * Added the `repair` command which will repopulate resources needed for
    properly upgrading a Linkerd installation
  * Added Helm-style `set`, `set-string`, `values`, `set-files` customization
    flags for the `linkerd install` and `linkerd upgrade` commands
  * Introduced the `linkerd identity` command, used to fetch the TLS certificates
    for injected pods (thanks @jimil749)
  * Removed the `get` and `logs` command from the CLI

* Helm
  * Changed many Helm values, please see the upgrade notes

* Viz
  * Introduced the `linkerd viz` subcommand which contains commands for
    installing the viz extension and all visibility commands
  * Updated the Web UI to only display the "Gateway" sidebar link when the
    multicluster extension is active
  * Added a `linkerd viz list` command to list pods with tap enabled
  * Fixed an issue where the `tap` APIServer would not refresh its certs
    automatically when provided externally—like through cert-manager

* Multicluster
  * Introduced the `linkerd multicluster` subcommand which contains commands for
    installing the multicluster extension and all multicluster commands
  * Added support for cross-cluster TCP traffic
  * Updated the service mirror controller to copy the
    `config.linkerd.io/opaque-ports` annotation when mirroring services so that
    cross-cluster traffic can be correctly handled as opaque
  * Added support for multicluster gateways of types other than LoadBalancer
    (thanks @DaspawnW!)

* Jaeger
  * Introduced the `linkerd jaeger` subcommand which contains commands for
    installing the jaeger extension and all tracing commands
  * Added a `linkerd jaeger list` command to list pods with tracing enabled

This release includes changes from a massive list of contributors. A special
thank-you to everyone who helped make this release possible:
[Lutz Behnke](https://github.com/cypherfox)
[Björn Wenzel](https://github.com/DaspawnW)
[Filip Petkovski](https://github.com/fpetkovski)
[Simon Weald](https://github.com/glitchcrab)
[GMarkfjard](https://github.com/GMarkfjard)
[hodbn](https://github.com/hodbn)
[Hu Shuai](https://github.com/hs0210)
[Jimil Desai](https://github.com/jimil749)
[jiraguha](https://github.com/jiraguha)
[Joakim Roubert](https://github.com/joakimr-axis)
[Josh Soref](https://github.com/jsoref)
[Kelly Campbell](https://github.com/kellycampbell)
[Matei David](https://github.com/mateiidavid)
[Mayank Shah](https://github.com/mayankshah1607)
[Max Goltzsche](https://github.com/mgoltzsche)
[Mitch Hulscher](https://github.com/mhulscher)
[Eugene Formanenko](https://github.com/mo4islona)
[Nathan J Mehl](https://github.com/n-oden)
[Nicolas Lamirault](https://github.com/nlamirault)
[Oleh Ozimok](https://github.com/oleh-ozimok)
[Piyush Singariya](https://github.com/piyushsingariya)
[Naga Venkata Pradeep Namburi](https://github.com/pradeepnnv)
[rish-onesignal](https://github.com/rish-onesignal)
[Shai Katz](https://github.com/shaikatz)
[Takumi Sue](https://github.com/tkms0106)
[Raphael Taylor-Davies](https://github.com/tustvold)
[Yashvardhan Kukreja](https://github.com/yashvardhan-kukreja)

## edge-21.3.2

This edge release is another release candidate for stable 2.10 and fixes some
final bugs found in testing. A big thank you to users who have helped us
identity these issues!

* Fixed an issue with the service profile validating webhook that prevented
  service profiles from being added or updated
* Updated the `check` command output hint anchors to match Linkerd component
  names
* Fixed a permission issue with the Viz extension's tap admin cluster role by
  adding namespace listing to the allowed actions
* Fixed an issue with the proxy where connections would not be torn down when
  communicating with a defunct endpoint
* Improved diagnostic logging in the proxy
* Fixed an issue with the Viz extension's Prometheus template that prevented
  users from specifying a log level flag for that component (thanks @n-oden!)
* Fixed a template parsing issue that prevented users from specifying additional
  ignored inbound parts through Helm's `--set` flag
* Fixed an issue with the proxy where non-HTTP streams could sometimes hang due
  to TLS buffering

## edge-21.3.1

This edge release is another release candidate, bringing us closer to
`stable-2.10.0`! It fixes the Helm install/upgrade procedure and ships some new
CLI commands, among other improvements.

* Fixed Helm install/upgrade, which was failing when not explicitly setting
  `proxy.image.version`
* Added a warning in the dashboard when viewing tap streams from resources that
  don't have tap enabled
* Added the command `linkerd viz list` to list meshed pods and indicate which can
  be tapped, which need to be restarted before they can be tapped, and which
  have tap disabled
* Similarly, added the command `linkerd jaeger list` to list meshed pods and
  indicate which will participate in tracing
* Added the `--opaque-ports` flag to `linkerd inject` to specify the list of
  opaque ports when injecting pods (and services)
* Simplified the output of `linkerd jaeger check`, combining the checks for the
  status of each component into a single check
* Changed the destination component to receive the list of default opaque ports
  set during install so that it's properly reflected during discovery
* Moved the level of the proxy server's I/O-related "Connection closed" messages
  from info to debug, which were not providing actionable information

## edge-21.2.4

This edge is a release candidate for `stable-2.10.0`! It wraps up the functional
changes planned for the upcoming stable release. We hope you can help us test
this in your staging clusters so that we can address anything unexpected before
an official stable.

This release introduces support for CLI extensions. The Linkerd `check` command
will now invoke each extension's `check` command so that users can check the
health of their Linkerd installation and extensions with one command. Additional
documentation will follow for developers interested in creating extensions.

Additionally, there is no longer a default list of ports skipped by the proxy.
These ports have been moved to opaque ports, meaning protocols like MySQL will
be encrypted by default and without user input.

* Cleaned up entries in `values.yaml` by removing `do not edit` entries; they
  are now hardcoded in the templates
* Added the count of service profiles installed in a cluster to the Heartbeat
  metrics
* Fixed CLI commands which would unnecessarily print usage instructions after
  encountering API errors (thanks @piyushsingariya!)
* Fixed the `install` command so that it errors after detecting there is an
  existing Linkerd installation in the cluster
* Changed the identity controller to receive the trust anchor via environment
  variable instead of by flag; this allows the certificate to be loaded from a
  config map or secret (thanks @mgoltzsche!)
* Updated the proxy to use TLS version 1.3; support for TLS 1.2 remains enabled
  for compatibility with prior proxy versions
* The opaque ports annotation is now supported on services and enables users to
  use this annotation on mirrored services in multicluster installations
* Reverted the renaming of the `mirror.linkerd.io` label
* Ports `25,443,587,3306,5432,11211` have been removed from the default skip
  ports; all traffic through those ports is now proxied and handled opaquely by
  default
* Errors configuring the firewall in CNI are propagated so that they can be
  handled by the user
* Removed Viz extension warnings from the `check --proxy` command when tap is
  not configured for pods; this is now handled by the `viz tap` command
* Added support for CLI extensions as well as ensuring their `check` commands
  are invoked by Linkerd's `check` command
* Moved the `metrics`, `endpoints`, and `install-sp` commands into subcommands
  under the `diagnostics` command.
* Removed the `linkerd-` prefix from non-cluster scoped resources in the Viz and
  Jaeger extensions
* Added the linkerd-await helper to all Linkerd containers so that the proxy can
  initialize before the components start making outbound connections
* Removed the `tcp_connection_duration_ms` histogram from the metrics export to
  fix high cardinality issues that surfaced through high memory usage

## edge-21.2.3

This release wraps up most of the functional changes planned for the upcoming
`stable-2.10.0` release. Try this edge release in your staging cluster and
let us know if you see anything unexpected!

* **Breaking change**: Changed the multicluster `Service`-export annotation
  from `mirror.linkerd.io/exported` to `multicluster.linkerd.io/export`
* Updated the proxy-injector to to set the `config.linkerd.io/opaque-ports`
  annotation on newly-created `Service` objects when the annotation is set on
  its parent `Namespace`
* Updated the proxy-injector to ignore pods that have disabled
  `automountServiceAccountToken` (thanks @jimil749)
* Updated the proxy to log warnings when control plane components are
  unresolveable
* Updated the Destination controller to cache node topology metadata (thanks
  @fpetkovski)
* Updated the CLI to handle API errors without printing the CLI usage (thanks
  @piyushsingariya)
* Updated the Web UI to only display the "Gateway" sidebar link when the
  multicluster extension is active
* Fixed the Web UI on Chrome v88 (thanks @kellycampbell)
* Improved `install` and `uninstall` behavior for extensions to prevent
  control-plane components from being left in a broken state
* Docker images are now hosted on the `cr.l5d.io` registry
* Updated base docker images to buster-20210208-slim
* Updated the Go version to 1.14.15
* Updated the proxy to prevent outbound connections to localhost to protect
  against traffic loops

## edge-21.2.2

This edge release introduces support for multicluster TCP!

The `repair` command was added which will repopulate resources needed for
upgrading from a `2.9.x` installation. There will be an error message during the
upgrade process indicating that this command should be run so that users do not
need to guess.

Lastly, it contains a breaking change for Helm users. The `global` field has
been removed from the Helm chart now that it is no longer needed. Users will
need to pass in the identity certificates again—along with any other
customizations, no longer rooted at `global`.

* **Breaking change**: Removed the `Global` field from the Linkerd Helm chart
  now that it is unused because of the extension model
* Added the `repair` command which will repopulate resources needed for properly
  upgrading a Linkerd installation
* Fixed the spelling of the `sidecarContainers` key in the Viz extension Helm
  chart to match that of the template (thanks @n-oden!)
* Added the `tapInjector.logLevel` key to the Viz extension helm chart so that
  the log level of the component can be configured
* Removed the `--disable-tap` flag from the `inject` command now that tap is no
  longer part of the core installation (thanks @mayankshah1607!)
* Changed proxy configuration to use fully-qualified DNS names to avoid extra
  search paths in DNS resolutions
* Changed the `check` command to include each installed extension's `check`
  output; this allows users to check for proper configuration and installation
  of Linkerd without running a command for each extension
* Added proxy support for TCP traffic to the multicluster gateways

## edge-21.2.1

This edge release continues improving the proxy's diagnostics and also avoids
timing out when the HTTP protocol detection fails. Additionally, old resource
versions were upgraded to avoid warnings in k8s v1.19. Finally, it comes with
lots of CLI improvements detailed below.

* Improved the proxy's diagnostic metrics to help us get better insights into
  services that are in fail-fast
* Improved the proxy's HTTP protocol detection to prevent timeout errors
* Upgraded CRD and webhook config resources to get rid of warnings in k8s v1.19
  (thanks @mateiidavid!)
* Added viz components into the Linkerd Health Grafana charts
* Had the tap injector add a `viz.linkerd.io/tap-enabled` annotation when
  injecting a pod, which allowed providing clearer feedback for the `linkerd
  tap` command
* Had the jaeger injector add a `jaeger.linkerd.io/tracing-enabled` annotation
  when injecting a pod, which also allowed providing better feedback for the
  `linkerd jaeger check` command
* Improved the `linkerd uninstall` command so it fails gracefully when there
  still are injected resources in the cluster (a `--force` flag was provided
  too)
* Moved the `linkerd profile --tap` functionality into a new command `linkerd
  viz profile --tap`, given tap now belongs to the viz extension
* Expanded the `linkerd viz check` command to include data-plane checks
* Cleaned-up YAML in templates that was incompatible with SOPS (thanks
  @tkms0106!)

## edge-21.1.4

This edge release continues to polish the Linkerd extension model and improves
the robustness of the opaque transport.

* Improved the consistency of behavior of the `check` commands between
  Linkerd extensions
* Fixed an issue where Linkerd extension commands could be run before the
  extension was fully installed
* Renamed some extension Helm charts for consistency:
  * jaeger -> linkerd-jaeger
  * linkerd2-multicluster -> linkerd-multicluster
  * linkerd2-multicluster-link -> linkerd-multicluster-link
* Fixed an issue that could cause the inbound proxy to fail meshed HTTP/1
  requests from older proxies (from the stable-2.8.x vintage)
* Changed opaque-port transport to be advertised via ALPN so that new proxies
  will not initiate opaque-transport connections to proxies from prior edge
  releases
* Added inbound proxy transport metrics with `tls="passhtru"` when forwarding
  non-mesh TLS connections
* Thanks to @hs0210 for adding new unit tests!

## edge-21.1.3

This edge release improves proxy diagnostics and recovery in situations where
the proxy is temporarily unable to route requests. Additionally, the `viz` and
`multicluster` CLI sub-commands have been updated for consistency.

Full release notes:

* Added Helm-style `set`, `set-string`, `values`, `set-files` customization
  flags for the `linkerd install` and `linkerd multicluster install` commands
* Fixed an issue where `linkerd metrics` could return metrics for the incorrect
  set of pods when there are overlapping label selectors
* Added tap-injector to linkerd-viz which is responsible for adding the tap
  service name environment variable to the Linkerd proxy container
* Improved diagnostics when the proxy is temporarily unable to route requests
* Made proxy recovery for a service more robust when the proxy is unable to
  route requests, even when new requests are being received
* Added `client` and `server` prefixes in the proxy logs for socket-level errors
  to indicate which side of the proxy encountered the error
* Improved jaeger-injector reliability in environments with many resources by
  adding watch RBAC permissions
* Added check to confirm whether the jaeger-injector pod is in running state
  (thanks @yashvardhan-kukreja!)
* Fixed a crash in the destination controller when EndpointSlices are enabled
  (thanks @oleh-ozimok!)
* Added a `linkerd viz check` sub-command to verify the states of the
  `linkerd-viz` components
* Added a `log-format` flag to optionally output the control plane component log
  output as JSON (thanks @mo4islona!)
* Updated the logic in the `metrics` and `profile` subcommands to use the
  `namespace` specified by the `current-context` of the KUBECONFIG so that it is
  no longer necessary to use the `--namespace` flag to query resources in the
  current namespace. Queries for resources in namespaces other than the
  current namespace still require the `--namespace` flag
* Added new pod 'linkerd-metrics-api' set up by `linkerd viz install` that
  manages all functionality dependent on Prometheus, thus removing most of the
  dependencies on Prometheus from the linkerd core installation
* Removed need to have linkerd-viz installed for the
  `linkerd multicluster check` command to properly work.

## edge-21.1.2

This edge release continues the work on decoupling non-core Linkerd components.
Commands that use the viz extension i.e, `dashboard`, `edges`, `routes`,
`stat`, `tap` and `top` are moved to the `viz` sub-command. These commands are still
available under root but are marked as deprecated and will be removed in a
later stable release.

This release also upgrades the proxy's dependencies to the Tokio v1 ecosystem.

* Moved sub-commands that use the viz extension under `viz`
* Started ignoring pods with `Succeeded` status when watching IP addresses
  in destination. This allows the re-use of IPs of terminated pods
* Support Bring your own Jaeger use-case by adding `collector.jaegerAddr` in
  the Jaeger extension.
* Fixed an issue with the generation of working manifests in the
  `podAntiAffinity` use-case
* Added support for the modification of proxy resources in the viz
  extension through `values.yaml` in Helm and flags in CLI.
* Improved error reporting for port-forward logic with namespace
  and pod data, used across dashboard, checks, etc
  (thanks @piyushsingariya)
* Added support to disable the rendering of `linkerd-viz` namespace
  resource in the viz extension (thanks @nlamirault)
* Made service-profile generation work offline with `--ignore-cluster`
  flag (thanks @piyushsingariya)
* Upgraded the proxy's dependencies to the Tokio v1 ecosystem

## edge-21.1.1

This edge release introduces a new "opaque transport" feature that allows the
proxy to securely transport server-speaks-first and otherwise opaque TCP
traffic. Using the `config.linkerd.io/opaque-ports` annotation on pods and
namespaces, users can configure ports that should skip the proxy's protocol
detection.

Additionally, a new `linkerd-viz` extension has been introduced that separates
the installation of the Grafana, Prometheus, web, and tap components. This
extension closely follows the Jaeger and multicluster extensions; users can
`install` and `uninstall` with the `linkerd viz ..` command as well as configure
for HA with the `--ha` flag.

The `linkerd viz install` command does not have any cli flags to customize the
install directly, but instead follows the Helm way of customization by using
flags such as `set`, `set-string`, `values`, `set-files`.

Finally, a new `/shutdown` admin endpoint that may only be accessed over the
loopback network has been added. This allows batch jobs to gracefully terminate
the proxy on completion. The `linkerd-await` utility can be used to automate
this.

* Added a new `linkerd multicluster check` command to validate that the
  `linkerd-multicluster` extension is working correctly
* Fixed description in the `linkerd edges` command (thanks @jsoref!)
* Moved the Grafana, Prometheus, web, and tap components into a new Viz chart,
  following the same extension model that multicluster and Jaeger follow
* Introduced a new "opaque transport" feature that allows the proxy to securely
  transport server-speaks-first and otherwise opaque TCP traffic
* Removed the check comparing the `ca.crt` field in the identity issuer secret
  and the trust anchors in the Linkerd config; these values being different is
  not a failure case for the `linkerd check` command (thanks @cypherfox!)
* Removed the Prometheus check from the `linkerd check` command since it now
  depends on a component that is installed with the Viz extension
* Fixed error messages thrown by the cert checks in `linkerd check` (thanks
  @pradeepnnv!)
* Added PodDisruptionBudgets to the control plane components so that they cannot
  be all terminated at the same time during disruptions (thanks @tustvold!)
* Fixed an issue that displayed the wrong `linkerd.io/proxy-version` when it is
  overridden by annotations (thanks @mateiidavid!)
* Added support for custom registries in the `linkerd-viz` helm chart (thanks
  @jimil749!)
* Renamed `proxy-mutator` to `jaeger-injector` in the `linkerd-jaeger` extension
* Added a new `/shutdown` admin endpoint that may only be accessed over the
  loopback network allowing batch jobs to gracefully terminate the proxy on
  completion
* Introduced the `linkerd identity` command, used to fetch the TLS certificates
  for injected pods (thanks @jimil749)
* Fixed an issue with the CNI plugin where it was incorrectly terminating and
  emitting error events (thanks @mhulscher!)
* Re-added support for non-LoadBalancer service types in the
  `linkerd-multicluster` extension

## edge-20.12.4

This edge release adds support for the `config.linkerd.io/opaque-ports`
annotation on pods and namespaces, to configure ports that should skip the
proxy's protocol detection. In addition, it adds new CLI commands related to the
`linkerd-jaeger` extension, fixes bugs in the CLI `install` and `upgrade`
commands and Helm charts, and fixes a potential false positive in the proxy's
HTTP protocol detection. Finally, it includes improvements in proxy performance
and memory usage, including an upgrade for the proxy's dependency on the Tokio
async runtime.

* Added support for the `config.linkerd.io/opaque-ports` annotation on pods and
  namespaces, to indicate to the proxy that some ports should skip protocol
  detection
* Fixed an issue where `linkerd install --ha` failed to honor flags
* Fixed an issue where `linkerd upgrade --ha` can override existing configs
* Added missing label to the `linkerd-config-overrides` secret to avoid breaking
  upgrades performed with the help of `kubectl apply --prune`
* Added a missing icon to Jaeger Helm chart
* Added new `linkerd jaeger check` CLI command to validate that the
  `linkerd-jaeger` extension is working correctly
* Added new `linkerd jaeger uninstall` CLI command to print the `linkerd-jaeger`
  extension's resources so that they can be piped into `kubectl delete`
* Fixed an issue where the `linkerd-cni` daemgitonset may not be installed on all
  intended nodes, due to missing tolerations to the `linkerd-cni` Helm chart
  (thanks @rish-onesignal!)
* Fixed an issue where the `tap` APIServer would not refresh its certs
  automatically when provided externally—like through cert-manager
* Changed the proxy's cache eviction strategy to reduce memory consumption,
  especially for busy HTTP/1.1 clients
* Fixed an issue in the proxy's HTTP protocol detection which could cause false
  positives for non-HTTP traffic
* Increased the proxy's default dispatch timeout to 5 seconds to accommodate
  connection pools which might open connections without immediately making a
  request
* Updated the proxy's Tokio dependency to v0.3

## edge-20.12.3

This edge release is functionally the same as `edge-20.12.2`. It fixes an issue
that prevented the release build from occurring.

## edge-20.12.2

* Fixed an issue where the `proxy-injector` and `sp-validator` did not refresh
  their certs automatically when provided externally—like through cert-manager
* Added support for overrides flags to the `jaeger install` command to allow
  setting Helm values when installing the Linkerd-jaeger extension
* Added missing Helm values to the multicluster chart (thanks @DaspawnW!)
* Moved tracing functionality to the `linkerd-jaeger` extension
* Fixed various issues in developer shell scripts (thanks @joakimr-axis!)
* Fixed an issue where `install --ha` was only partially applying the high
  availability config
* Updated RBAC API versions in the CNI chart (thanks @glitchcrab!)
* Fixed an issue where TLS credentials are changed during upgrades, but the
  Linkerd webhooks would not restart, leaving them to use older credentials and
  fail requests
* Stopped publishing the multicluster link chart as its primary use case is in
  the `multicluster link` command and not being installed through Helm
* Added service mirror error logs for when the multicluster gateway's hostname
  cannot be resolved.

## edge-20.12.1

This edge release continues the work of decoupling non-core Linkerd components
by moving more tracing related functionality into the Linkerd-jaeger extension.

* Continued work on moving tracing functionality from the main control plane
  into the `linkerd-jaeger` extension
* Fixed a potential panic in the proxy when looking up a socket's peer address
  while under high load
* Added automatic readme generation for charts (thanks @GMarkfjard!)
* Fixed zsh completion for the CLI (thanks @jiraguha!)
* Added support for multicluster gateways of types other than LoadBalancer
  (thanks @DaspawnW!)

## edge-20.11.5

This edge release improves the proxy's support high-traffic workloads. It also
contains the first steps towards decoupling non-core Linkerd components, the
first iteration being a new `linkerd jaeger` sub-command for installing tracing.
Please note this is still a work in progress.

* Addressed some issues reported around clients seeing max-concurrency errors by
  increasing the default in-flight request limit to 100K pending requests
* Have the proxy appropriately set `content-type` when synthesizing gRPC error
  responses
* Bumped the `proxy-init` image to `v1.3.8` which is based off of
  `buster-20201117-slim` to reduce potential security vulnerabilities
* No longer panic in rare cases when `linkerd-config` doesn't have an entry for
  `Global` configs (thanks @hodbn!)
* Work in progress: the `/jaeger` directory now contains the charts and commands
  for installing the tracing component.

## edge-20.11.4

* Fixed an issue in the destination service where endpoints always included a
  protocol hint, regardless of the controller label being present or not

## edge-20.11.3

This edge release improves support for CNI by properly handling parameters
passed to the `nsenter` command, relaxes checks on root and intermediate
certificates (following X509 best practices), and fixes two issues: one that
prevented installation of the control plane into a custom namespace and one
which failed to update endpoint information when a headless service is modified.
This release also improves linkerd proxy performance by eliminating unnecessary
endpoint resolutions for TCP traffic and properly tearing down serverside
connections when errors occur.

* Added HTTP/2 keepalive PING frames
* Removed logic to avoid redundant TCP endpoint resolution
* Fixed an issue where serverside connections were not torn down when an error
  occurs
* Updated `linkerd check` so that it doesn't attempt to validate the subject
  alternative name (SAN) on root and intermediate certificates. SANs for leaf
  certificates will continue to be validated
* Fixed a CLI issue where the `linkerd-namespace` flag is not honored when
  passed to the `install` and `upgrade` commands
* Fixed an issue where the proxy does not receive updated endpoint information
  when a headless service is modified
* Updated the control plane Docker images to use `buster-20201117-slim` to
  reduce potential security vulnerabilities
* Updated the proxy-init container to `v1.3.7` which fixes CNI issues in certain
  environments by properly parsing `nsenter` args

## edge-20.11.2

This edge release reduces memory consumption of Linkerd proxies which maintain
many idle connections (such as Prometheus).  It also removes some obsolete
commands from the CLI and allows setting custom annotations on multicluster
gateways.

* Reduced the default idle connection timeout to 5s for outbound clients and
  20s for inbound clients to reduce the proxy's memory footprint, especially on
  Prometheus instances
* Added support for setting annotations on the multicluster gateway in Helm
  which allows setting the load balancer as internal (thanks @shaikatz!)
* Removed the `get` and `logs` command from the CLI

## stable-2.9.0

This release extends Linkerd's zero-config mutual TLS (mTLS) support to all TCP
connections, allowing Linkerd to transparently encrypt and authenticate all TCP
connections in the cluster the moment it's installed. It also adds ARM support,
introduces a new multi-core proxy runtime for higher throughput, adds support
for Kubernetes service topologies, and lots, lots more, as described below:

* Proxy
  * Performed internal improvements for lower latencies under high concurrency
  * Reduced performance impact of logging, especially when the `debug` or
    `trace` log levels are disabled
  * Improved error handling for DNS errors encountered when discovering control
    plane addresses; this can be common during installation before all
    components have been started, allowing linkerd to continue to operate
    normally in HA during node outages

* Control Plane
  * Added support for [topology-aware service
    routing](https://kubernetes.io/docs/concepts/services-networking/service-topology/)
    to the Destination controller; when providing service discovery updates to
    proxies the Destination controller will now filter endpoints based on the
    service's topology preferences
  * Added support for the new Kubernetes
    [EndpointSlice](https://kubernetes.io/docs/concepts/services-networking/endpoint-slices/)
    resource to the Destination controller; Linkerd can be installed with
    `--enable-endpoint-slices` flag to use this resource rather than the
    Endpoints API in clusters where this new API is supported

* Dashboard
  * Added new Spanish translations (please help us translate into your
    language!)
  * Added new section for exposing multicluster gateway metrics

* CLI
  * Renamed the `--addon-config` flag to `--config` to clarify this flag can be
    used to set any Helm value
  * Added fish shell completions to the `linkerd` command

* Multicluster
  * Replaced the single `service-mirror` controller with separate controllers
    that will be installed per target cluster through `linkerd multicluster
    link`
  * Changed the mechanism for mirroring services: instead of relying on
    annotations on the target services, now the source cluster should specify
    which services from the target cluster should be exported by using a label
    selector
  * Added support for creating multiple service accounts when installing
    multicluster with Helm to allow more granular revocation
  * Added a multicluster `unlink` command for removing multicluster links

* Prometheus
  * Moved Linkerd's bundled Prometheus into an add-on (enabled by default); this
    makes the Linkerd Prometheus more configurable, gives it a separate upgrade
    lifecycle from the rest of the control plane, and allows users to
    disable the bundled Prometheus instance
  * The long-awaited Bring-Your-Own-Prometheus case has been finally addressed:
    added `global.prometheusUrl` to the Helm config to have linkerd use an
    external Prometheus instance instead of the one provided by default
  * Added an option to persist data to a volume instead of memory, so that
    historical metrics are available when Prometheus is restarted
  * The helm chart can now configure persistent storage and limits

* Other
  * Added a new `linkerd.io/inject: ingress` annotation and accompanying
    `--ingress` flag to the `inject` command, to configure the proxy to support
    service profiles and enable per-route metrics and traffic splits for HTTP
    ingress controllers
  * Changed the type of the injector and tap API secrets to `kubernetes.io/tls`
    so they can be provisioned by cert-manager
  * Changed default docker image repository to `ghcr.io` from `gcr.io`; **Users
    who pull the images into private repositories should take note of this
    change**
  * Introduced support for authenticated docker registries
  * Simplified the way that Linkerd stores its configuration; configuration is
    now stored as Helm values in the `linkerd-config` ConfigMap
  * Added support for Helm configuration of per-component proxy resources
    requests

This release includes changes from a massive list of contributors. A special
thank-you to everyone who helped make this release possible: [Abereham G
Wodajie](https://github.com/Abrishges), [Alexander
Berger](https://github.com/alex-berger), [Ali
Ariff](https://github.com/aliariff), [Arthur Silva
Sens](https://github.com/ArthurSens), [Chris
Campbell](https://github.com/campbel), [Daniel
Lang](https://github.com/mavrick), [David Tyler](https://github.com/DaveTCode),
[Desmond Ho](https://github.com/DesmondH0), [Dominik
Münch](https://github.com/muenchdo), [George
Garces](https://github.com/jgarces21), [Herrmann
Hinz](https://github.com/HerrmannHinz), [Hu Shuai](https://github.com/hs0210),
[Jeffrey N. Davis](https://github.com/penland365), [Joakim
Roubert](https://github.com/joakimr-axis), [Josh
Soref](https://github.com/jsoref), [Lutz Behnke](https://github.com/cypherfox),
[MaT1g3R](https://github.com/MaT1g3R), [Marcus Vaal](https://github.com/mvaal),
[Markus](https://github.com/mbettsteller), [Matei
David](https://github.com/mateiidavid), [Matt
Miller](https://github.com/mmiller1), [Mayank
Shah](https://github.com/mayankshah1607),
[Naseem](https://github.com/naseemkullah), [Nil](https://github.com/c-n-c),
[OlivierB](https://github.com/olivierboudet), [Olukayode
Bankole](https://github.com/rbankole), [Paul
Balogh](https://github.com/javaducky), [Rajat
Jindal](https://github.com/rajatjindal), [Raphael
Taylor-Davies](https://github.com/tustvold), [Simon
Weald](https://github.com/glitchcrab), [Steve
Gray](https://github.com/steve-gray), [Suraj
Deshmukh](https://github.com/surajssd), [Tharun
Rajendran](https://github.com/tharun208), [Wei Lun](https://github.com/WLun001),
[Zhou Hao](https://github.com/zhouhao3), [ZouYu](https://github.com/Hellcatlk),
[aimbot31](https://github.com/aimbot31),
[iohenkies](https://github.com/iohenkies), [memory](https://github.com/memory),
and [tbsoares](https://github.com/tbsoares)

## edge-20.11.1

This edge supersedes edge-20.10.6 as a release candidate for stable-2.9.0.

* Fixed issue where the `check` command would error when there is no Prometheus
  configured
* Fixed recent regression that caused multicluster on EKS to not work properly
* Changed the `check` command to warn instead of error when webhook certificates
  are near expiry
* Added the `--ingress` flag to the `inject` command which adds the recently
  introduced `linkerd.io/inject: ingress` annotation
* Fixed issue with upgrades where external certs would be fetched and stored
  even though this does not happen on fresh installs with externally created
  certs
* Fixed issue with upgrades where the issuer cert expiration was being reset
* Removed the `--registry` flag from the `multicluster install` command
* Removed default CPU limits for the proxy and control plane components in HA
  mode

## edge-20.10.6

This edge supersedes edge-20.10.5 as a release candidate for stable-2.9.0. It
adds a new `linkerd.io/inject: ingress` annotation to support service profiles
and enable per-route metrics and traffic splits for HTTP ingress controllers

* Added a new `linkerd.io/inject: ingress` annotation to configure the
  proxy to support service profiles and enable per-route metrics and traffic
  splits for HTTP ingress controllers
* Reduced performance impact of logging in the proxy, especially when the
  `debug` or `trace` log levels are disabled
* Fixed spurious warnings logged by the `linkerd profile` CLI command

## edge-20.10.5

This edge supersedes edge-20.10.4 as a release candidate for stable-2.9.0. It
adds a fix for updating the destination service when there are no endpoints

* Added a fix to clear the EndpointTranslator state when it gets a
  `NoEndpoints` message. This ensures that the clients get the correct set of
  endpoints during an update.

## edge-20.10.4

This edge release is a release candidate for stable-2.9.0. For the proxy, there
have been changes to improve performance, remove unused code, and configure
ports that can be ignored by default. Also, this edge release adds enhancements
to the multicluster configuration and observability, adds more translations to
the dashboard, and addresses a bug in the CLI.

* Added more Spanish translations to the dashboard and more labels that can be
  translated
* Added support for creating multiple service accounts when installing
  multicluster with Helm to allow more granular revocation
* Renamed `global.proxy.destinationGetNetworks` to `global.clusterNetworks`.
  This is a cluster-wide setting and can no longer be overridden per-pod
* Fixed an empty multicluster Grafana graph which used a deprecated label
* Added the control plane tracing ServiceAccounts to the linkerd-psp
  RoleBinding so that it can be used in environments where PodSecurityPolicy
  is enabled
* Enhanced EKS support by adding `100.64.0.0/10` to the set of discoverable
  networks
* Fixed a bug in the way that the `--all-namespaces` flag is handled by the
  `linkerd edges` command
* Added a default set of ports to bypass the proxy for server-first, https,
  and memcached traffic

## edge-20.10.3

This edge release is a release candidate for stable-2.9.0.  It overhauls the
discovery and routing logic implemented by the proxy, simplifies the way that
Linkerd stores configuration, and adds new Helm values to configure additional
labels, annotations, and namespace selectors for webhooks.

* Added podLabels and podAnnotations Helm values to allow adding additional
  labels or annotations to Linkerd control plane pods (thanks @tustvold!)
* Added namespaceSelector Helm value for configuring the namespace selector
  used by admission webhooks (thanks @tustvold!)
* Expanded the 'linkerd edges' command to show TCP connections
* Overhauled the discovery and routing logic implemented by the proxy:
  * The `l5d-dst-override` header is no longer honored
  * When the application attempts to connect to a pod IP, the proxy no
    longer load balances these requests among all pods in the service.
    The proxy will now honor session-stickiness as selected by an
    application-level load balancer
  * `TrafficSplits` are only applied when a client targets a service's IP
  * The proxy no longer performs DNS "canonicalization" to translate
    relative host header names to a fully-qualified form
* Simplified the way that Linkerd stores its configuration.  Configuration is
  now stored as Helm values in the linkerd-config ConfigMap
* Renamed the --addon-config flag to --config to clarify this flag can be used
  to set any Helm value

## edge-20.10.2

This edge release adds more improvements for mTLS for all TCP traffic.
It also includes significant internal improvements to the way Linkerd
configuration is stored within the cluster.

* Changed TCP metrics exported by the proxy to ensure that peer
  identities are encoded via the `client_id` and `server_id` labels.
* Removed the dependency of control plane components on `linkerd-config`
* Updated the data structure `proxy-injector` uses to derive the configuration
  used when injecting workloads

## edge-20.10.1

This edge release includes a couple of external contributions towards
improved cert-manager support and Grafana charts fixes, among other
enhancements.

* Changed the type of the injector and tap API secrets to `kubernetes.io/tls`,
  so they can be provisioned by cert-manager (thanks @cypherfox!)
* Fixed the "Kubernetes cluster monitoring" Grafana dashboard that had a few
  charts with incomplete data (thanks @aimbot31!)
* Fixed the `service-mirror` multicluster component so that it retries
  connections to the target cluster's Kubernetes API when it's not reachable,
  instead of blocking
* Increased the proxy's default timeout for DNS resolution to 500ms, as there
  were reports that 100ms was too restrictive

## edge-20.9.4

This edge release introduces support for authenticated docker registries and
fixes a recent multicluster regression.

* Fixed a regression in multicluster gateway configurations that would forbid
  inbound gateway traffic
* Upgraded bundled Grafana to v7.1.5
* Enabled Jaeger receiver in collector configuration in Helm chart (thanks
  @olivierboudet!)
* Fixed skip port configuration being skipped in CNI plugin
* Introduced support for authenticated docker registries (thanks @c-n-c!)

## edge-20.9.3

This edge release includes fixes and updates for the control plane and CLI.

* Added `--dest-cni-bin-dir` flag to the `linkerd install-cni` command, to
  configure the directory on the host where the CNI binary will be placed
* Removed `collector.name` and `jaeger.name` config fields from the tracing
  addon
* Updated Jaeger to 1.19.2
* Fixed a warning about deprecated Go packages in controller container logs

## edge-20.9.2

This edge release continues the work of adding support for mTLS for all TCP
traffic and changes the default container registry to `ghcr.io` from `gcr.io`.

If you are upgrading from `stable-2.8.x` with the Linkerd CLI using the
`linkerd upgrade` command, you must add the `--addon-overwrite` flag to ensure
that the grafana image is properly set.

* Removed the default timeout for ServiceProfiles so that ServiceProfile routes
  behave the same as when there is no ServiceProfile definition
* Changed default docker image repository to ghcr.io from gcr.io. **Users who
  pull the images into private repositories should take note of this change**
* Added endpoint labels to outbound TCP metrics to provide more context and
  detail for the metrics, add load balancing to TCP connections
  (bypassing kube-proxy), and secure the connection with mTLS when both
  endpoints are meshed
* Made unnamed ServiceProfile discovery configurable using the
  `proxy.destinationGetNetworks` variable to set the
  `LINKERD2_PROXY_DESTINATION_PROFILE_NETWORKS` variable in the proxy chart
  template
* Added TLS certificate validation for the Injector, SP Validator, and Tap
  webhooks to the `linkerd check` command

## edge-20.9.1

This edge release contains an important proxy update that allows linkerd to
continue to operate normally in HA during node outages. We're also adding full
Kubernetes 1.19 support!

* Improved the proxy's error handling for DNS errors encountered when
  discovering control plane addresses, which can be common during installation,
  before all components have been started
* The destination and identity services had to be made headless in order to
  support that new controller discovery (which now can leverage SRV records)
* Use SAN fields when generating the linkerd webhook configs; this completes the
  Kubernetes 1.19 support which enforces them
* Fixed `linkerd check` for multicluster that was spuriously claiming the
  absence of some resources
* Improved the injection test cleanup (thanks @zhouhao3!)
* Added ability to run the integration test suite using a cluster in an ARM
  architecture (thanks @aliariff!)

## edge-20.8.4

* Fixed a problem causing the `enable-endpoint-slices` flag to not be persisted
  when set via `linkerd upgrade` (thanks @Matei207!)
* Removed SMI-Metrics templates and experimental sub-commands
* Use `--frozen-lockfile` to avoid accidental update of dashboard JS
  dependencies in CI (thanks @tharun208!)

## edge-20.8.3

This edge release adds support for [topology-aware service routing][topology] to
the Destination controller. When providing service discovery updates to proxies,
the Destination controller will now filter endpoints based on the service's
topology preferences. Additionally, this release includes bug fixes for the
`linkerd check` CLI command and web dashboard.

* CLI
  * `linkerd check` will no longer warn about a looser webhook failure policy in
    HA mode
* Controller
  * Added support for [topology-aware service routing][topology] to the Destination
    controller (thanks @Matei207)
  * Changed the Destination controller to always return destination overrides
    for service profiles when no traffic split is present
* Web UI
  * Fixed Tap `Authority` dropdown not being populated (thanks to @tharun208!)

[topology]: https://kubernetes.io/docs/concepts/services-networking/service-topology/

## edge-20.8.2

This edge release adds an internationalization framework to the dashboard,
Spanish translations to the dashboard UI, and a `linkerd multicluster uninstall`
command for graceful removal of the multicluster components.

* Web UI
  * Added Spanish translations to the dashboard
  * Added a framework and documentation to simplify creation of new
    translations
* Multicluster
  * Added a multicluster uninstall command
  * Added a warning from `linkerd check --multicluster` if the multicluster
    support is not installed

## edge-20.8.1

This edge adds multi-arch support to Linkerd! Our docker images and CLI now
support the amd64, arm64, and arm architectures.

* Multicluster
  * Added a multicluster unlink command for removing multicluster links
  * Improved multicluster checks to be more informative when the remote API is
    not reachable
* Proxy
  * Enabled a multi-threaded runtime to substantially improve latency especially
    when the proxy is serving requests for many concurrent connections
* Other
  * Fixed an issue where the debug sidecar image was missing during upgrades
    (thanks @javaducky!)
  * Updated all control plane plane and proxy container images to be multi-arch
    to support amd64, arm64, and arm (thanks @aliariff!)
  * Fixed an issue where check was failing when DisableHeartBeat was set to true
    (thanks @mvaal!)

## edge-20.7.5

This edge brings a new approach to multicluster service mirror controllers and
the way services in target clusters are selected for mirroring.

The long-awaited Bring-Your-Own-Prometheus case has been finally addressed.

Many other improvements from our great contributors are described below. Also
note progress is still being made under the covers for future support for Service
Topologies (by @Matei207) and delivering image builds in multiple platforms (by
@aliariff).

* Multicluster
  * Replaced the single `service-mirror` controller, with separate controllers
    that will be installed per target cluster through `linkerd multicluster
    link`. More info [here](https://github.com/linkerd/linkerd2/pull/4710).
  * Changed the mechanism for mirroring services: instead of relying on
    annotations on the target services, now the source cluster should specify
    which services from the target cluster should be exported by using a label
    selector. More info [here](https://github.com/linkerd/linkerd2/pull/4795).
  * Added new section in the dashboard for exposing multicluster gateway metrics
    (thanks @tharun208!)
* Prometheus
  * Added `global.prometheusUrl` to the Helm config to have linkerd use an
    external Prometheus instance instead of the one provided by default.
  * Added ability to declare sidecar containers in the Prometheus Helm config.
    This allows adding components for cases like exporting logs to services
    such as Cloudwatch, Stackdriver, Datadog, etc. (thanks @memory!)
  * Upgraded Prometheus to the latest version (v2.19.3), which should consume
    substantially less memory, among other benefits.
* Other
  * Fixed bug in `linkerd check` that was failing to wait for Prometheus to be
    available right after having installed linkerd.
  * Added ability to set `priorityClassName` for CNI DaemonSet pods, and to
    install CNI in an existing namespace (both options provided through the CLI
    and as Helm configs) (thanks @alex-berger!)
  * Added support for overriding the proxy's inbound and outbound TCP connection
    timeouts (thanks @mmiller1!)
  * Added library support for dashboard i18n. Strings still need to be tagged
    and translations to be added. More info
    [here](https://github.com/linkerd/linkerd2/pull/4803).
  * In some Helm charts, replaced the non-standard
    `linkerd.io/helm-release-version` annotation with `checksum/config` for
    forcing restarting the component during upgrades (thanks @naseemkullah!)
  * Upgraded the proxy init-container to v1.3.4, which comes with an updated
    debian-buster distro and will provide cleaner logs listing the iptables
    rules applied.

## edge-20.7.4

This edge release adds support for the new Kubernetes
[EndpointSlice](https://kubernetes.io/docs/concepts/services-networking/endpoint-slices/)
resource to the Destination controller. Using the EndpointSlice API is more
efficient for the Kubernetes control plane than using the Endpoints API. If
the cluster supports EndpointSlices (a beta feature in Kubernetes 1.17),
Linkerd can be installed with `--enable-endpoint-slices` flag to use this
resource rather than the Endpoints API.

* Added fish shell completions to the `linkerd` command (thanks @WLun001!)
* Enabled the support for EndpointSlices (thanks @Matei207!)
* Separated Prometheus checks and made them runnable only when the add-on
  is enabled

## edge-20.7.3

* Add preliminary support for EndpointSlices which will be usable in future
  releases (thanks @Matei207!)
* Internal improvements to the CI process for testing Helm installations

## edge-20.7.2

This edge release moves Linkerd's bundled Prometheus into an add-on. This makes
the Linkerd Prometheus more configurable, gives it a separate upgrade lifecycle
from the rest of the control plane, and will allow users to disable the bundled
Prometheus instance. In addition, this release includes fixes for several
issues, including a regression where the proxy would fail to report OpenCensus
spans.

* Prometheus is now an optional add-on, enabled by default
* Custom tolerations can now be specified for control plane resources when
  installing with Helm (thanks @DesmondH0!)
* Evicted data plane pods are no longer considered to be failed by `linkerd
  check --proxy`, fixing an issue where the check would be retried indefinitely
  as long as evicted pods are present
* Fixed a regression where proxy spans were not reported to OpenCensus
* Fixed a bug where the proxy injector would fail to render skipped port lists
  when installed with Helm
* Internal improvements to the proxy for lower latencies under high concurrency
* Thanks to @Hellcatlk and @surajssd for adding new unit tests and spelling
  fixes!

## edge-20.7.1

This edge release features the option to persist prometheus data to a volume
instead of memory, so that historical metrics are available when prometheus is
restarted. Additional changes are outlined in the bullet points below.

* Some commands like `linkerd stat` would fail if any control plane components
  were unhealthy, even when other replicas are healthy. The check conditions
  for these commands have been improved
* The helm chart can now configure persistent storage for Prometheus
  (thanks @naseemkullah!)
* The proxy log output format can now be configured to `plain` or `json` using
  the `config.linkerd.io/proxy-log-format` annotation or the
  `global.proxy.logFormat` value in the helm chart
  (thanks again @naseemkullah!)
* `linkerd install --addon-config=` now supports URLs in addition to local
  files
* The CNI Helm chart used the incorrect variable name to determine the `createdBy`
  version tag. This is now controlled by `cniPluginVersion` in the helm chart
* The proxy's default buffer size has been increased, which reduces latency when
  the proxy has many concurrent clients

## edge-20.6.4

This edge release moves the proxy onto a new version of the Tokio runtime. This
allows us to more easily integrate with the ecosystem and may yield performance
benefits as well.

* Upgraded the proxy's underlying Tokio runtime and its related libraries
* Added support for PKCS8 formatted ECDSA private keys
* Added support for Helm configuration of per-component proxy resources requests
  and limits (thanks @cypherfox!)
* Updated the `linkerd inject` command to throw an error while injecting
  non-compliant pods (thanks @mayankshah1607)

## stable-2.8.1

This release fixes multicluster gateways support on EKS.

* The multicluster service-mirror has been extended to resolve DNS names for
  target clusters when an IP address is not known.
* Linkerd checks could fail when run from the dashboard. Thanks to @alex-berger
  for providing a fix!
* Have the service mirror controller check in `linkerd check` retry on failures.
* As of this version we're including a Chocolatey package (Windows) next to the
  other binaries in the release assets in GitHub.
* Base images have been updated:
  * debian:buster-20200514-slim
  * grafana/grafana:7.0.3
* The shell scripts under `bin` continued to be improved, thanks to @joakimr-axis!

## edge-20.6.3

This edge release is a release candidate for stable-2.8.1. It includes a fix
to support multicluster gateways on EKS.

* The `config.linkerd.io/proxy-destination-get-networks` annotation configures
  the networks for which a proxy can discover metadata. This is an advanced
  configuration option that has security implications.
* The multicluster service-mirror has been extended to resolve DNS names for
  target clusters when an IP address it not known.
* Linkerd checks could fail when run from the dashboard. Thanks to @alex-berger
  for providing a fix!
* The CLI will be published for Chocolatey (Windows) on future stable releases.
* Base images have been updated:
  * debian:buster-20200514-slim
  * grafana/grafana:7.0.3

## stable-2.8.0

This release introduces new a multi-cluster extension to Linkerd, allowing it
to establish connections across Kubernetes clusters that are secure,
transparent to the application, and work with any network topology.

* The CLI has a new set of `linkerd multicluster` sub-commands that provide
  tooling to create the resources needed to discover services across
  Kubernetes clusters.
* The `linkerd multicluster gateways` command exposes gateway-specific
  telemetry to supplement the existing `stat` and `tap` commands.
* The Linkerd-provided Grafana instance remains enabled by default, but it can
  now be disabled. When it is disabled, the Linkerd dashboard can be
  configured to link to an alternate, externally-managed Grafana instance.
* Jaeger & OpenCensus are configurable as an [add-on][addon-2.8.0]; and the
  proxy has been improved to emit spans with labels that reflect its pod's
  metadata.
* The `linkerd-cni` component has been promoted from _experimental_ to
  _stable_.
* `linkerd profile --open-api` now honors the `x-linkerd-retryable` and
  `x-linkerd-timeout` OpenAPI annotations.
* The Helm chart continues to become more flexible and modular, with new
  Prometheus configuration options. More information is available in the
  [Helm chart README][helm-2.8.0].
* gRPC stream error handling has been improved so that transport errors
  are indicated to the client with a `grpc-status: UNAVAILABLE` trailer.
* The proxy's memory footprint could grow significantly when
  server-speaks-first-protocol connections hit the proxy. Now, a timeout is
  in place to prevent these connections from consuming resources.
* After benchmarking the proxy in high-concurrency situations, the inbound
  proxy has been improved to reduce contention, improving latency and
  reducing spurious timeouts.
* The proxy could fail requests to services that had only 1 request every 60
  seconds. This race condition has been eliminated.
* Finally, users reported that ingress misconfigurations could cause the proxy
  to consume an entire CPU which could lead to timeouts. The proxy now
  attempts to prevent the most common traffic-loop scenarios to protect against
  this.

***NOTE***: Linkerd's `multicluster` extension does not yet work on Amazon
EKS. We expect to follow this release with a stable-2.8.1 to address this
issue. Follow [#4582](https://github.com/linkerd/linkerd2/pull/4582) for updates.

This release includes changes from a massive list of contributors. A special
thank-you to everyone who helped make this release possible: @aliariff,
@amariampolskiy, @arminbuerkle, @arthursens, @christianhuening,
@christyjacob4, @cypherfox, @daxmc99, @dr0pdb, @drholmie, @hydeenoble,
@joakimr-axis, @jpresky, @kohsheen1234, @lewiscowper, @lundbird, @matei207,
@mayankshah1607, @mmiller1, @naseemkullah, @sannimichaelse, & @supra08.

[addon-2.8.0]: https://github.com/linkerd/linkerd2/blob/4219955bdb5441c5fce192328d3760da13fb7ba1/charts/linkerd2/README.md#add-ons-configuration
[helm-2.8.0]: https://github.com/linkerd/linkerd2/blob/4219955bdb5441c5fce192328d3760da13fb7ba1/charts/linkerd2/README.md

## edge-20.6.2

This edge release is our second release candidate for `stable-2.8`, including
various fixes and improvements around multicluster support.

* CLI
  * Fixed bad output in the `linkerd multicluster gateways` command
  * Improved the error returned when running the CLI with no KUBECONFIG path set
    (thanks @Matei207!)
* Controller
  * Fixed issue where mirror service wasn't created when paired to a gateway
    whose external IP wasn't yet provided
  * Fixed issue where updating the gateway identity annotation wasn't propagated
    back into the mirror gateway endpoints object
  * Fixed issue where updating the gateway ports wasn't reflected in the gateway
    mirror service
  * Increased the log level for some of the service mirror events
  * Changed the nginx gateway config so that it runs as non-root and denies all
    requests to locations other than the probe path
* Web UI
  * Fixed multicluster Grafana dashboard
* Internal
  * Added flag in integration tests to dump fixture diffs into a separate
    directory (thanks @cypherfox!)

## edge-20.6.1

This edge release is a release candidate for `stable-2.8`! It introduces several
improvements and fixes for multicluster support.

* CLI
  * Added multicluster daisy chain checks to `linkerd check`
  * Added list of successful gateways in multicluster checks section of `linkerd
    check`
* Controller
  * Renamed `nginx-configuration` ConfigMap to `linkerd-gateway-config` (please
    manually remove the former if upgrading from an earlier multicluster
    install, thanks @mayankshah1607!)
  * Renamed multicluster gateway ports to `mc-gateway` and `mc-probe`
  * Fixed Service Profiles routes for `linkerd-prometheus`
* Internal
  * Fixed shellcheck errors in all `bin/` scripts (thanks @joakimr-axis!)
* Helm
  * Added support for `linkerd mc allow`
  * Added ability to disable secret resources for self-signed certs (thanks
    @cypherfox!)
* Proxy
  * Modified the `linkerd-gateway` component to use the inbound proxy, rather
    than nginx, for gateway; this allows Linkerd to detect loops and propagate
    identity

## edge-20.5.5

This edge release adds refinements to the Linkerd multicluster implementation,
adds new health checks for the tracing add-on, and addresses an issue in which
outbound requests from the proxy result in looping behavior.

* CLI
  * Added the `multicluster` command along with subcommands to configure and
    deploy Linkerd workloads which enable services to be mirrored across
    clusters
  * Added health-checks for tracing add-on
* Proxy
  * Added logic to prevent loops in outbound requests

## edge-20.5.4

* CLI
  * Fixed the display of the meshed pod column for non-selector services in
    `linkerd stat` output
  * Added an `addon-overwrite` upgrade flag which allows users to overwrite the
    existing addon config rather than merging into it
  * Added a `--close-wait-timeout` inject flag which sets the
    `nf_conntrack_tcp_timeout_close_wait` property which can be used to mitigate
    connection issues with application that hold half-closed sockets
* Controller
  * Restricted the service-mirror's RBAC permissions so that it no longer is
    able to read secrets in all namespaces
  * Moved many multicluster components into the `linkerd-multicluster` namespace
    by default
  * Added multicluster gateway mirror services to allow multicluster liveness
    probes to work in private networks
  * Fixed an issue where multicluster gateway mirror services could be
    incorrectly deleted during a resync
* Internal
  * Fixed many style issues in build scripts (thanks @joakimr-axis!)
* Helm
  * Added `global.grafanaUrl` variable to allow using an existing Grafana
    installation

## edge-20.5.3

* Controller
  * Added a Grafana dashboard for tracking multi-cluster traffic metrics
  * Added health checks for the Grafana add-on, under a separate section
  * Fixed issues when updating a remote multi-cluster gateway

* Proxy
  * Added special special handling for I/O errors in HTTP responses so that an
    `errno` label is included to describe the underlying errors in the proxy's
    metrics

* Internal
  * Started gathering stats of CI runs for aggregating CI health metrics

## edge-20.5.2

This edge release contains everything required to get up and running with
multicluster. For a tutorial on how to do that, check out the
[documentation](https://linkerd.io/2/features/multicluster_support/).

* CLI
  * Added a section to the `linkerd check` that validates that all clusters
    part of a multicluster setup have compatible trust anchors
  * Modified the `inkerd cluster export-service` command to work by
    transforming yaml instead of modifying cluster state
  * Added functionality that allows the `linkerd cluster export-service`
    command to operate on lists of services
* Controller
  * Changed the multicluster gateway to always require TLS on connections
    originating from outside the cluster
  * Removed admin server timeouts from control plane components, thereby
    fixing a bug that can cause liveness checks to fail
* Helm
  * Moved Grafana templates into a separate add-on chart
* Proxy
  * Improved latency under high-concurrency use cases.

## edge-20.5.1

* CLI
  * Fixed all commands to use kubeconfig's default namespace if specified
    (thanks @Matei207!)
  * Added multicluster checks to the `linkerd check` command
  * Hid development flags in the `linkerd install` command for release builds
* Controller
  * Added ability to configure Prometheus Alertmanager as well as recording
    and alerting rules on the Linkerd Prometheus (thanks @naseemkullah!)
  * Added ability to add more commandline flags to the Prometheus command
    (thanks @naseemkullah!)
* Web UI
  * Fixed TrafficSplit detail page not loading
  * Added Jaeger links to the dashboard when the tracing addon is enabled
* Proxy
  * Modified internal buffering to avoid idling out services as a request
    arrives, fixing failures for requests that are sent exactly once per
    minute--such as Prometheus scrapes

## edge-20.4.5

This edge release includes several new CLI commands for use with multi-cluster
gateways, and adds liveness checks and metrics for gateways. Additionally, it
makes the proxy's gRPC error-handling behavior more consistent with other
implementations, and includes a fix for a bug in the web UI.

* CLI
  * Added `linkerd cluster setup-remote` command for setting up a
    multi-cluster gateway
  * Added `linkerd cluster gateways` command to display stats for
    multi-cluster gateways
  * Changed `linkerd cluster export-service` to modify a provided YAML file
    and output it, rather than mutating the cluster
* Controller
  * Added liveness checks and Prometheus metrics for multi-cluster gateways
  * Changed the proxy injector to configure proxies to do destination lookups
    for IPs in the private IP range
* Web UI
  * Fixed errors when viewing resource detail pages
* Internal
  * Created script and config to build a Linkerd CLI Chocolatey package for
    Windows users, which will be published with stable releases (thanks to
    @drholmie!)
* Proxy
  * Changed the proxy to set a `grpc-status: UNAVAILABLE` trailer when a gRPC
    response stream is interrupted by a transport error

## edge-20.4.4

This edge release fixes a packaging issue in `edge-20.4.3`.

_From `edge.20.4.3` release notes_:

This edge release adds functionality to the CLI to output more detail and
includes changes which support the multi-cluster functionality. Also, the helm
support has been expanded to make installation more configurable. Finally, the
HA reliability is improved by ensuring that control plane pods are restarted
with a rolling strategy

* CLI
  * Added output to the `linkerd check --proxy` command to list all data plane
    pods which are not up-to-date rather than just printing the first one it
    encounters
  * Added a `--proxy` flag to the `linkerd version` command which lists all
    proxy versions running in the cluster and the number of pods running each
    version
  * Lifted requirement of using --unmeshed for linkerd stat when querying
    TrafficSplit resources
  * Added support for multi-stage installs with Add-Ons
* Controller
  * Added a rolling update strategy to Linkerd deployments that have multiple
    replicas during HA deployments to ensure that at most one pod begins
    terminating before a new pod ready is ready
  * Added a new label for the proxy injector to write to the template,
    `linkerd.io/workload-ns` which indicates the namespace of the workload/pod
* Internal
  * Added a [security
    policy](https://help.github.com/en/github/managing-security-vulnerabilities/adding-a-security-policy-to-your-repository)
    to facilitate conversations around security
* Helm
  * Changed charts to use downwardAPI to mount labels to the proxy container
    making them easier to identify
* Proxy
  * Changed the Linkerd proxy endpoint for liveness to use the new `/live`
    admin endpoint instead of the `/metrics` endpoint, because the `/live`
    endpoint returns a smaller payload
  * Added a per-endpoint authority-override feature to support multi-cluster
    gateways

## edge-20.4.3

**This release is superseded by `edge-20.4.4`**

This edge release adds functionality to the CLI to output more detail and
includes changes which support the multi-cluster functionality. Also, the helm
support has been expanded to make installation more configurable. Finally, the
HA reliability is improved by ensuring that control plane pods are restarted
with a rolling strategy

* CLI
  * Added output to the `linkerd check --proxy` command to list all data plane
    pods which are not up-to-date rather than just printing the first one it
    encounters
  * Added a `--proxy` flag to the `linkerd version` command which lists all
    proxy versions running in the cluster and the number of pods running each
    version
  * Lifted requirement of using --unmeshed for linkerd stat when querying
    TrafficSplit resources
  * Added support for multi-stage installs with Add-Ons
* Controller
  * Added a rolling update strategy to Linkerd deployments that have multiple
    replicas during HA deployments to ensure that at most one pod begins
    terminating before a new pod ready is ready
  * Added a new label for the proxy injector to write to the template,
    `linkerd.io/workload-ns` which indicates the namespace of the workload/pod
* Internal
  * Added a [security
    policy](https://help.github.com/en/github/managing-security-vulnerabilities/adding-a-security-policy-to-your-repository)
    to facilitate conversations around security
* Helm
  * Changed charts to use downwardAPI to mount labels to the proxy container
    making them easier to identify
* Proxy
  * Changed the Linkerd proxy endpoint for liveness to use the new `/live`
    admin endpoint instead of the `/metrics` endpoint, because the `/live`
    endpoint returns a smaller payload
  * Added a per-endpoint authority-override feature to support multi-cluster
    gateways

## edge-20.4.2

This release brings a number of CLI fixes and Controller improvements.

* CLI
  * Fixed a bug that caused pods to crash after upgrade if
    `--skip-outbound-ports` or `--skip-inbound-ports` were used
  * Added `unmeshed` flag to the `stat` command, such that unmeshed resources
    are only displayed if the user opts-in
  * Added a `--smi-metrics` flag to `install`, to allow installation of the
    experimental `linkerd-smi-metrics` component
  * Fixed a bug in `linkerd stat`, causing incorrect output formatting when
    using the `--o wide` flag
  * Fixed a bug, causing `linkerd uninstall` to fail when attempting to delete
    PSPs
* Controller
  * Improved the anti-affinity of `linkerd-smi-metrics` deployment to avoid
    pod scheduling problems during `upgrade`
  * Improved endpoints change detection in the `linkerd-destination` service,
    enabling mirrored remote services to change cluster gateways
  * Added `operationID` field to tap OpenAPI response to prevent issues during
    upgrade from 2.6 to 2.7
* Proxy
  * Added a new protocol detection timeout to prevent clients from consuming
    resources indefinitely when not sending any data

## edge-20.4.1

This release introduces some cool new functionalities, all provided by our
awesome community of contributors! Also two bugs were fixed that were
introduced since edge-20.3.2.

* CLI
  * Added `linkerd uninstall` command to uninstall the control plane (thanks
    @Matei207!)
  * Fixed a bug causing `linkerd routes -o wide` to not show the proper actual
    success rate
* Controller
  * Fail proxy injection if the pod spec has `automountServiceAccountToken`
    disabled (thanks @mayankshah1607!)
* Web UI
  * Added a route dashboard to Grafana (thanks @lundbird!)
* Proxy
  * Fixed a bug causing the proxy's inbound to spuriously return 503 timeouts

## edge-20.3.4

This release introduces several fixes and improvements to the CLI.

* CLI
  * Added support for kubectl-style label selectors in many CLI commands
    (thanks @mayankshah1607!)
  * Fixed the path regex in service profiles generated from proto files
    without a package name (thanks @amariampolskiy!)
  * Fixed an error when injecting Cronjobs that have no metadata
  * Relaxed the clock skew check to match the default node heartbeat interval
    on Kubernetes 1.17 and made this check a warning
  * Fixed a bug where the linkerd-smi-metrics pod could not be created on
    clusters with pod security policy enabled
* Internal
  * Upgraded tracing components to more recent versions and improved resource
    defaults (thanks @Pothulapati!)

## edge-20.3.3

This release introduces new experimental CLI commands for querying metrics
using the Service Mesh Interface (SMI) and for multi-cluster support via
service mirroring.

If you would like to learn more about service mirroring or SMI, or are
interested in experimenting with these features, please join us in [Linkerd
Slack](https://slack.linkerd.io) for help and feedback.

* CLI
  * Added experimental `linkerd cluster` commands for managing multi-cluster
    service mirroring
  * Added the experimental `linkerd alpha clients` command, which uses the
    smi-metrics API to display client-side metrics from each of a resource's
    clients
  * Added retries to some `linkerd check` checks to prevent spurious failures
    when run immediately after cluster creation or Linkerd installation

## edge-20.3.2

This release introduces substantial proxy improvements as well as new
observability and security functionality.

* CLI
  * Added the `linkerd alpha stat` command, which uses the smi-metrics API;
    the latter enables access to metrics to be controlled with RBAC
* Controller
  * Added support for configuring service profile timeouts
    `(x-linkerd-timeout)` via OpenAPI spec (thanks @lewiscowper!)
* Web UI
  * Improved the Grafana dashboards to use a globing operator for Prometheus
    in order to avoid producing queries that are too large (thanks @mmiller1!)
* Helm
  * Improved the `linkerd2` chart README (thanks @lundbird!)
* Proxy
  * Fixed a bug that could cause log levels to be processed incorrectly

## edge-20.3.1

This release introduces new functionality mainly focused around observability
and multi-cluster support via `service mirroring`.

If you would like to learn more about `service mirroring` or are interested in
experimenting with this feature, please join us in [Linkerd
Slack](https://slack.linkerd.io) for help and feedback.

* CLI
  * Improved the `linkerd check` command to check for extension server
    certificate (thanks @christyjacob4!)
* Controller
  * Removed restrictions preventing Linkerd from injecting proxies into
    Contour (thanks @alfatraining!)
  * Added an experimental version of a service mirroring controller, allowing
    discovery of services on remote clusters.
* Web UI
  * Fixed a bug causing incorrect Grafana links to be rendered in the web
    dashboard.
* Proxy
  * Fixed a bug that could cause the proxy's load balancer to stop processing
    updates from service discovery.

## edge-20.2.3

This release introduces the first optional add-on `tracing`, added through the
new add-on model!

The existing optional `tracing` components Jaeger and OpenCensus can now be
installed as add-on components.

There will be more information to come about the new add-on model, but please
refer to the details of [#3955](https://github.com/linkerd/linkerd2/pull/3955)
for how to get started.

* CLI
  * Added the `linkerd diagnostics` command to get metrics only from the
    control plane, excluding metrics from the data plane proxies (thanks
    @srv-twry!)
  * Added the `linkerd install --prometheus-image` option for installing a
    custom Prometheus image (thanks @christyjacob4!)
  * Fixed an issue with `linkerd upgrade` where changes to the `Namespace`
    object were ignored (thanks @supra08!)
* Controller
  * Added the `tracing` add-on which installs Jaeger and OpenCensus as add-on
    components (thanks @Pothulapati!!)
* Proxy
  * Increased the inbound router's default capacity from 100 to 10k to
    accommodate environments that have a high cardinality of virtual hosts
    served by a single pod
* Web UI
  * Fixed styling in the CallToAction banner (thanks @aliariff!)

## edge-20.2.2

This release includes the results from continued profiling & performance
analysis on the Linkerd proxy. In addition to modifying internals to prevent
unwarranted memory growth, new metrics were introduced to aid in debugging and
diagnostics.

Also, Linkerd's CNI plugin is out of experimental, check out the docs at
<https://linkerd.io/2/features/cni/> !

* CLI
  * Added support for label selectors in the `linkerd stat` command (thanks
    @mayankshah1607!)
  * Added scrolling functionality to the `linkerd top` output (thanks
    @kohsheen1234!)
  * Fixed bug in `linkerd metrics` that was causing a panic when
    port-forwarding failed (thanks @mayankshah1607!)
  * Added check to `linkerd check` verifying the number of replicas for
    Linkerd components in HA (thanks @mayankshah1607!)
  * Unified trust anchors terminology across the CLI commands
  * Removed some messages from `linkerd upgrade`'s output that are no longer
    relevant (thanks @supra08!)

* Controller
  * Added support for configuring service profile retries
    `(x-linkerd-retryable)` via OpenAPI spec (thanks @kohsheen1234!)
  * Improved traffic split metrics so sources in all namespaces are shown, not
    just traffic from the traffic split's own namespace
  * Improved linkerd-identity's logs and events to help diagnosing certificate
    validation issues (thanks @mayankshah1607!)

* Proxy
  * Added `request_errors_total` metric exposing the number of requests that
    receive synthesized responses due to proxy errors

* Helm
  * Added a new `enforcedHostRegexp` variable to allow configuring the
    linkerd-web component enforced host (that was previously introduced to
    protect against DNS rebinding attacks) (thanks @sannimichaelse!)

* Internal
  * Removed various es-lint warnings from the dashboard code (thanks
    @christyjacob4 and @kohsheen1234!)
  * Fixed go module file syntax (thanks @daxmc99!)

## stable-2.7.0

This release adds support for integrating Linkerd's PKI with an external
certificate issuer such as [`cert-manager`] as well as streamlining the
certificate rotation process in general. For more details about cert-manager
and certificate rotation, see the
[docs](https://linkerd.io/2/tasks/use_external_certs/). This release also
includes performance improvements to the dashboard, reduced memory usage of
the proxy, various improvements to the Helm chart, and much much more.

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: This release includes breaking changes to our Helm charts.
Please see the [upgrade
instructions](https://linkerd.io/2/tasks/upgrade/#upgrade-notice-stable-270).

**Special thanks to**: @alenkacz, @bmcstdio, @daxmc99, @droidnoob, @ereslibre,
@javaducky, @joakimr-axis, @JohannesEH, @KIVagant, @mayankshah1607,
@Pothulapati, and @StupidScience!

**Full release notes**:

* CLI
  * Updated the mTLS trust anchor checks to eliminate false positives caused
    by extra trailing spaces
  * Reduced the severity level of the Linkerd version checks, so that they
    don't fail when the external version endpoint is unreachable (thanks
    @mayankshah1607!)
  * Added a new `tap` APIService check to aid with uncovering Kubernetes API
    aggregation layer issues (thanks @droidnoob!)
  * Introduced CNI checks to confirm the CNI plugin is installed and ready;
    this is done through `linkerd check --pre --linkerd-cni-enabled` before
    installation and `linkerd check` after installation if the CNI plugin is
    present
  * Added support for the `--as-group` flag so that users can impersonate
    groups for Kubernetes operations (thanks @mayankshah1607!)
  * Added HA specific checks to `linkerd check` to ensure that the
    `kube-system` namespace has the
    `config.linkerd.io/admission-webhooks:disabled` label set
  * Fixed a problem causing the presence of unnecessary empty fields in
    generated resource definitions (thanks @mayankshah1607)
  * Added the ability to pass both port numbers and port ranges to
    `--skip-inbound-ports` and `--skip-outbound-ports` (thanks to @javaducky!)
  * Increased the comprehensiveness of `linkerd check --pre`
  * Added TLS certificate validation to `check` and `upgrade` commands
  * Added support for injecting CronJobs and ReplicaSets, as well as the
    ability to use them as targets in the CLI subcommands
  * Introduced the new flags `--identity-issuer-certificate-file`,
    `--identity-issuer-key-file` and `identity-trust-anchors-file` to `linkerd
    upgrade` to support trust anchor and issuer certificate rotation
  * Added a check that ensures using `--namespace` and `--all-namespaces`
    results in an error as they are mutually exclusive
  * Added a `Dashboard.Replicas` parameter to the Linkerd Helm chart to allow
    configuring the number of dashboard replicas (thanks @KIVagant!)
  * Removed redundant service profile check (thanks @alenkacz!)
  * Updated `uninject` command to work with namespace resources (thanks
    @mayankshah1607!)
  * Added a new `--identity-external-issuer` flag to `linkerd install` that
    configures Linkerd to use certificates issued by an external certificate
    issuer (such as `cert-manager`)
  * Added support for injecting a namespace to `linkerd inject` (thanks
    @mayankshah1607!)
  * Added checks to `linkerd check --preinstall` ensuring Kubernetes Secrets
    can be created and accessed
  * Fixed `linkerd tap` sometimes displaying incorrect pod names for unmeshed
    IPs that match multiple running pods
  * Made `linkerd install --ignore-cluster` and `--skip-checks` faster
  * Fixed a bug causing `linkerd upgrade` to fail when used with
    `--from-manifest`
  * Made `--cluster-domain` an install-only flag (thanks @bmcstdio!)
  * Updated `check` to ensure that proxy trust anchors match configuration
       (thanks @ereslibre!)
  * Added condition to the `linkerd stat` command that requires a window size
    of at least 15 seconds to work properly with Prometheus
* Controller
  * Fixed an issue where an override of the Docker registry was not being
    applied to debug containers (thanks @javaducky!)
  * Added check for the Subject Alternate Name attributes to the API server
    when access restrictions have been enabled (thanks @javaducky!)
  * Added support for arbitrary pod labels so that users can leverage the
    Linkerd provided Prometheus instance to scrape for their own labels
    (thanks @daxmc99!)
  * Fixed an issue with CNI config parsing
  * Fixed a race condition in the `linkerd-web` service
  * Updated Prometheus to 2.15.2 (thanks @Pothulapati)
  * Increased minimum kubernetes version to 1.13.0
  * Added support for pod ip and service cluster ip lookups in the destination
    service
  * Added recommended kubernetes labels to control-plane
  * Added the `--wait-before-exit-seconds` flag to linkerd inject for the
    proxy sidecar to delay the start of its shutdown process (a huge commit
    from @KIVagant, thanks!)
  * Added a pre-sign check to the identity service
  * Fixed inject failures for pods with security context capabilities
  * Added `conntrack` to the `debug` container to help with connection
    tracking debugging
  * Fixed a bug in `tap` where mismatch cluster domain and trust domain caused
    `tap` to hang
  * Fixed an issue in the `identity` RBAC resource which caused start up
    errors in k8s 1.6 (thanks @Pothulapati!)
  * Added support for using trust anchors from an external certificate issuer
    (such as `cert-manager`) to the `linkerd-identity` service
  * Added support for headless services (thanks @JohannesEH!)
* Helm
  * **Breaking change**: Renamed `noInitContainer` parameter to `cniEnabled`
  * **Breaking Change** Updated Helm charts to follow best practices (thanks
    @Pothulapati and @javaducky!)
  * Fixed an issue with `helm install` where the lists of ignored inbound and
    outbound ports would not be reflected
  * Fixed the `linkerd-cni` Helm chart not setting proper namespace
    annotations and labels
  * Fixed certificate issuance lifetime not being set when installing through
    Helm
  * Updated the helm build to retain previous releases
  * Moved CNI template into its own Helm chart
* Proxy
  * Fixed an issue that could cause the OpenCensus exporter to stall
  * Improved error classification and error responses for gRPC services
  * Fixed a bug where the proxy could stop receiving service discovery
    updates, resulting in 503 errors
  * Improved debug/error logging to include detailed contextual information
  * Fixed a bug in the proxy's logging subsystem that could cause the proxy to
    consume memory until the process is OOM killed, especially when the proxy
    was configured to log diagnostic information
  * Updated proxy dependencies to address RUSTSEC-2019-0033,
    RUSTSEC-2019-0034, and RUSTSEC-2020-02
* Web UI
  * Fixed an error when refreshing an already open dashboard when the Linkerd
    version has changed
  * Increased the speed of the dashboard by pausing network activity when the
    dashboard is not visible to the user
  * Added support for CronJobs and ReplicaSets, including new Grafana
    dashboards for them
  * Added `linkerd check` to the dashboard in the `/controlplane` view
  * Added request and response headers to the `tap` expanded view in the
    dashboard
  * Added filter to namespace select button
  * Improved how empty tables are displayed
  * Added `Host:` header validation to the `linkerd-web` service, to protect
    against DNS rebinding attacks
  * Made the dashboard sidebar component responsive
  * Changed the navigation bar color to the one used on the
    [Linkerd](https://linkerd.io/) website
* Internal
  * Added validation to incoming sidecar injection requests that ensures the
    value of `linkerd.io/inject` is either `enabled` or `disabled` (thanks
    @mayankshah1607)
  * Upgraded the Prometheus Go client library to v1.2.1 (thanks @daxmc99!)
  * Fixed an issue causing `tap`, `injector` and `sp-validator` to use old
    certificates after `helm upgrade` due to not being restarted
  * Fixed incomplete Swagger definition of the tap api, causing benign error
    logging in the kube-apiserver
  * Removed the destination container from the linkerd-controller deployment
    as it now runs in the linkerd-destination deployment
  * Allowed the control plane to be injected with the `debug` container
  * Updated proxy image build script to support HTTP proxy options (thanks
    @joakimr-axis!)
  * Updated the CLI `doc` command to auto-generate documentation for the proxy
    configuration annotations (thanks @StupidScience!)
  * Added new `--trace-collector` and `--trace-collector-svc-account` flags to
    `linkerd inject` that configures the OpenCensus trace collector used by
    proxies in the injected workload (thanks @Pothulapati!)
  * Added a new `--control-plane-tracing` flag to `linkerd install` that
    enables distributed tracing in the control plane (thanks @Pothulapati!)
  * Added distributed tracing support to the control plane (thanks
    @Pothulapati!)

## edge-20.2.1

This edge release is a release candidate for `stable-2.7` and fixes an issue
where the proxy could consume inappropriate amounts of memory.

* Proxy
  * Fixed a bug in the proxy's logging subsystem that could cause the proxy to
    consume memory until the process is OOM killed, especially when the proxy
    was configured to log diagnostic information
  * Fixed properly emitting `grpc-status` headers when signaling proxy errors
    to gRPC clients
  * Updated certain proxy dependencies to address RUSTSEC-2019-0033,
    RUSTSEC-2019-0034, and RUSTSEC-2020-02

## edge-20.1.4

This edge release is a release candidate for `stable-2.7`.

The `linkerd check` command has been updated to improve the control plane
debugging experience.

* CLI
  * Updated the mTLS trust anchor checks to eliminate false positives caused
    by extra trailing spaces
  * Reduced the severity level of the Linkerd version checks, so that they
    don't fail when the external version endpoint is unreachable (thanks
    @mayankshah1607!)
  * Added a new `tap` APIService check to aid with uncovering Kubernetes API
    aggregation layer issues (thanks @droidnoob!)

## edge-20.1.3

This edge release is a release candidate for `stable-2.7`.

An update to the Helm charts has caused a **breaking change** for users who
have installed Linkerd using Helm. In order to make the purpose of the
`noInitContainer` parameter more explicit, it has been renamed to
`cniEnabled`.

* CLI
  * Introduced CNI checks to confirm the CNI plugin is installed and ready;
    this is done through `linkerd check --pre --linkerd-cni-enabled` before
    installation and `linkerd check` after installation if the CNI plugin is
    present
  * Added support for the `--as-group` flag so that users can impersonate
    groups for Kubernetes operations (thanks @mayankshah160!)
* Controller
  * Fixed an issue where an override of the Docker registry was not being
    applied to debug containers (thanks @javaducky!)
  * Added check for the Subject Alternate Name attributes to the API server
    when access restrictions have been enabled (thanks @javaducky!)
  * Added support for arbitrary pod labels so that users can leverage the
    Linkerd provided Prometheus instance to scrape for their own labels
    (thanks @daxmc99!)
  * Fixed an issue with CNI config parsing
* Helm
  * **Breaking change**: Renamed `noInitContainer` parameter to `cniEnabled`
  * Fixed an issue with `helm install` where the lists of ignored inbound and
    outbound ports would not be reflected

## edge-20.1.2

* CLI
  * Added HA specific checks to `linkerd check` to ensure that the
    `kube-system` namespace has the
    `config.linkerd.io/admission-webhooks:disabled` label set
  * Fixed a problem causing the presence of unnecessary empty fields in
    generated resource definitions (thanks @mayankshah1607)
* Proxy
  * Fixed an issue that could cause the OpenCensus exporter to stall
* Internal
  * Added validation to incoming sidecar injection requests that ensures the
    value of `linkerd.io/inject` is either `enabled` or `disabled` (thanks
    @mayankshah1607)

## edge-20.1.1

This edge release includes experimental improvements to the Linkerd proxy's
request buffering and backpressure infrastructure.

Additionally, we've fixed several bugs when installing Linkerd with Helm,
updated the CLI to allow using both port numbers _and_ port ranges with the
`--skip-inbound-ports` and `--skip-outbound-ports`  flags, and fixed a
dashboard error that can occur if the dashboard is open in a browser while
updating Linkerd.

**Note**: The `linkerd-proxy` version included with this release is more
experimental than usual. We'd love your help testing, but be aware that there
might be stability issues.

* CLI
  * Added the ability to pass both port numbers and port ranges to
    `--skip-inbound-ports` and `--skip-outbound-ports` (thanks to @javaducky!)
* Controller
  * Fixed a race condition in the `linkerd-web` service
  * Updated Prometheus to 2.15.2 (thanks @Pothulapati)
* Web UI
  * Fixed an error when refreshing an already open dashboard when the Linkerd
    version has changed
* Proxy
  * Internal changes to the proxy's request buffering and backpressure
    infrastructure
* Helm
  * Fixed the `linkerd-cni` Helm chart not setting proper namespace
    annotations and labels
  * Fixed certificate issuance lifetime not being set when installing through
    Helm
  * More improvements to Helm best practices (thanks to @Pothulapati!)

## edge-19.12.3

This edge release adds support for pod IP and service cluster IP lookups,
improves performance of the dashboard, and makes `linkerd check --pre` perform
more comprehensive checks.

The `--wait-before-exit-seconds` flag has been added to allow Linkerd users to
 opt in to `preStop hooks`. The details of this change are in
 [#3798](https://github.com/linkerd/linkerd2/pull/3798).

Also, the proxy has been updated to `v2.82.0` which improves gRPC error
classification and [ensures that
resolutions](https://github.com/linkerd/linkerd2/pull/3848) are released when
the associated balancer becomes idle.

Finally, an update to follow best practices in the Helm charts has caused a
*breaking change*. Users who have installed Linkerd using Helm must be certain
to read the details of
[#3822](https://github.com/linkerd/linkerd2/issues/3822)

* CLI
  * Increased the comprehensiveness of `linkerd check --pre`
  * Added TLS certificate validation to `check` and `upgrade` commands
* Controller
  * Increased minimum kubernetes version to 1.13.0
  * Added support for pod ip and service cluster ip lookups in the destination
    service
  * Added recommended kubernetes labels to control-plane
  * Added the `--wait-before-exit-seconds` flag to linkerd inject for the
    proxy sidecar to delay the start of its shutdown process (a huge commit
    from @KIVagant, thanks!)
  * Added a pre-sign check to the identity service
* Web UI
  * Increased the speed of the dashboard by pausing network activity when the
    dashboard is not visible to the user
* Proxy
  * Added a timeout to release resolutions to idle balancers
  * Improved error classification for gRPC services
* Internal
  * **Breaking Change** Updated Helm charts to follow best practices using
    proper casing (thanks @Pothulapati!)

## edge-19.12.2

* CLI
  * Added support for injecting CronJobs and ReplicaSets, as well as the
    ability to use them as targets in the CLI subcommands
  * Introduced the new flags `--identity-issuer-certificate-file`,
    `--identity-issuer-key-file` and `identity-trust-anchors-file` to `linkerd
    upgrade` to support trust anchor and issuer certificate rotation
* Controller
  * Fixed inject failures for pods with security context capabilities
* Web UI
  * Added support for CronJobs and ReplicaSets, including new Grafana
    dashboards for them
* Proxy
  * Fixed a bug where the proxy could stop receiving service discovery
    updates, resulting in 503 errors
* Internal
  * Moved CNI template into a Helm chart to prepare for future publication
  * Upgraded the Prometheus Go client library to v1.2.1 (thanks @daxmc99!)
  * Reenabled certificates rotation integration tests

## edge-19.12.1

* CLI
  * Added condition to the `linkerd stat` command that requires a window size
    of at least 15 seconds to work properly with Prometheus
* Internal
  * Fixed whitespace path handling in non-docker build scripts (thanks
    @joakimr-axis!)
  * Removed Calico logutils dependency that was incompatible with Go 1.13
  * Updated Helm templates to use fully-qualified variable references based
    upon Helm best practices (thanks @javaducky!)

## edge-19.11.3

* CLI
  * Added a check that ensures using `--namespace` and `--all-namespaces`
    results in an error as they are mutually exclusive
* Internal
  * Fixed an issue causing `tap`, `injector` and `sp-validator` to use old
    certificates after `helm upgrade` due to not being restarted
  * Fixed incomplete Swagger definition of the tap api, causing benign error
    logging in the kube-apiserver

## edge-19.11.2

* CLI
  * Added a `Dashboard.Replicas` parameter to the Linkerd Helm chart to allow
    configuring the number of dashboard replicas (thanks @KIVagant!)
  * Removed redundant service profile check (thanks @alenkacz!)
* Web UI
  * Added `linkerd check` to the dashboard in the `/controlplane` view
  * Added request and response headers to the `tap` expanded view in the
    dashboard
* Internal
  * Removed the destination container from the linkerd-controller deployment
    as it now runs in the linkerd-destination deployment
  * Upgraded Go to version 1.13.4

## edge-19.11.1

* CLI
  * Updated `uninject` command to work with namespace resources (thanks
    @mayankshah1607!)
* Controller
  * Added `conntrack` to the `debug` container to help with connection
    tracking debugging
  * Fixed a bug in `tap` where mismatch cluster domain and trust domain caused
    `tap` to hang
  * Fixed an issue in the `identity` RBAC resource which caused start up
    errors in k8s 1.6 (thanks @Pothulapati!)
* Proxy
  * Improved debug/error logging to include detailed contextual information
* Web UI
  * Added filter to namespace select button
  * Improved how empty tables are displayed
* Internal
  * Added integration test for custom cluster domain
  * Allowed the control plane to be injected with the `debug` container
  * Updated proxy image build script to support HTTP proxy options (thanks
    @joakimr-axis!)
  * Updated the CLI `doc` command to auto-generate documentation for the proxy
    configuration annotations (thanks @StupidScience!)

## edge-19.10.5

This edge release adds support for integrating Linkerd's PKI with an external
certificate issuer such as [`cert-manager`], adds distributed tracing support
to the Linkerd control plane, and adds protection against DNS rebinding
attacks to the web dashboard. In addition, it includes several improvements to
the Linkerd CLI.

* CLI
  * Added a new `--identity-external-issuer` flag to `linkerd install` that
    configures Linkerd to use certificates issued by an external certificate
    issuer (such as `cert-manager`)
  * Added support for injecting a namespace to `linkerd inject` (thanks
    @mayankshah1607!)
  * Added checks to `linkerd check --preinstall` ensuring Kubernetes Secrets
    can be created and accessed
  * Fixed `linkerd tap` sometimes displaying incorrect pod names for unmeshed
    IPs that match multiple running pods
* Controller
  * Added support for using trust anchors from an external certificate issuer
    (such as `cert-manager`) to the `linkerd-identity` service
* Web UI
  * Added `Host:` header validation to the `linkerd-web` service, to protect
    against DNS rebinding attacks
* Internal
  * Added new `--trace-collector` and `--trace-collector-svc-account` flags to
    `linkerd inject` that configures the OpenCensus trace collector used by
    proxies in the injected workload (thanks @Pothulapati!)
  * Added a new `--control-plane-tracing` flag to `linkerd install` that
    enables distributed tracing in the control plane (thanks @Pothulapati!)
  * Added distributed tracing support to the control plane (thanks
    @Pothulapati!)

Also, thanks to @joakimr-axis for several fixes and improvements to internal
build scripts!

[`cert-manager`]: https://github.com/jetstack/cert-manager

## edge-19.10.4

This edge release adds dashboard UX enhancements, and improves the speed of
the CLI.

* CLI
  * Made `linkerd install --ignore-cluster` and `--skip-checks` faster
  * Fixed a bug causing `linkerd upgrade` to fail when used with
    `--from-manifest`
* Web UI
  * Made the dashboard sidebar component responsive
  * Changed the navigation bar color to the one used on the
    [Linkerd](https://linkerd.io/) website

## edge-19.10.3

This edge release adds support for headless services, improves the upgrade
process after installing Linkerd with a custom cluster domain, and enhances
the `check` functionality to report invalid trust anchors.

* CLI
  * Made `--cluster-domain` an install-only flag (thanks @bmcstdio!)
  * Updated `check` to ensure that proxy trust anchors match configuration
       (thanks @ereslibre!)
* Controller
  * Added support for headless services (thanks @JohannesEH!)
* Helm
  * Updated the helm build to retain previous releases

## stable-2.6.0

This release introduces distributed tracing support, adds request and response
headers to `linkerd tap`, dramatically improves the performance of the
dashboard on large clusters, adds traffic split visualizations to the
dashboard, adds a public Helm repo, and many more improvements!

For more details, see the announcement blog post:
<https://linkerd.io/2019/10/10/announcing-linkerd-2.6/>

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: Please see the [upgrade
instructions](https://linkerd.io/2/tasks/upgrade/#upgrade-notice-stable-2-6-0).

**Special thanks to**: @alenkacz, @arminbuerkle, @bmcstdio, @bourquep,
@brianstorti, @kevtaylor, @KIVagant, @pierDipi, and @Pothulapati!

**Full release notes**:

* CLI
  * Added a new `json` output option to the `linkerd tap` command, which
    exposes request and response headers
  * Added a public Helm repo - for full installation instructions, see our
    [Helm documentation](https://linkerd.io/2/tasks/install-helm/).
  * Added an `--address` flag to `linkerd dashboard`, allowing users to
    specify a port-forwarding address (thanks @bmcstdio!)
  * Added node selector constraints to Helm installation, so users can control
    which nodes the control plane is deployed to (thanks @bmcstdio!)
  * Added a `--cluster-domain` flag to the `linkerd install` command that
    allows setting a custom cluster domain (thanks @arminbuerkle!)
  * Added a `--disable-heartbeat` flag for `linkerd install | upgrade`
    commands
  * Allowed disabling namespace creation when installing Linkerd using Helm
    (thanks @KIVagant!)
  * Improved the error message when the CLI cannot connect to Kubernetes
    (thanks @alenkacz!)
* Controller
  * Updated the Prometheus config to keep only needed `cadvisor` metrics,
    substantially reducing the number of time-series stored in most clusters
  * Introduced `config.linkerd.io/trace-collector` and
    `config.alpha.linkerd.io/trace-collector-service-account` pod spec
    annotations to support per-pod tracing
  * Instrumented the proxy injector to provide additional metrics about
    injection (thanks @Pothulapati!)
  * Added Kubernetes events (and log lines) when the proxy injector injects a
    deployment, and when injection is skipped
  * Fixed a workload admission error between the Kubernetes apiserver and the
    HA proxy injector, by allowing workloads in a namespace to be omitted from
    the admission webhooks phase using the
    `config.linkerd.io/admission-webhooks: disabled` label (thanks
    @hasheddan!)
  * Fixed proxy injector timeout during a large number of concurrent
    injections
  * Added support for disabling the heartbeat cronjob (thanks @kevtaylor!)
* Proxy
  * Added distributed tracing support
  * Decreased proxy Docker image size by removing bundled debug tools
  * Added 587 (SMTP) to the list of ports to ignore in protocol detection
    (bound to server-speaks-first protocols) (thanks @brianstorti!)
* Web UI
  * Redesigned dashboard navigation so workloads are now viewed by namespace,
    with an "All Namespaces" option, in order to increase dashboard speed
  * Added Traffic Splits as a resource to the dashboard, including a Traffic
    Split detail page
  * Added a `Linkerd Namespace` Grafana dashboard, allowing users to view
    historical data for a given namespace, similar to CLI output for `linkerd
    stat deploy -n myNs` (thanks @bourquep!)
  * Fixed bad request in the top routes tab on empty fields (thanks
    @pierDipi!)
* Internal
  * Moved CI from Travis to GitHub Actions
  * Added requirement for Go `1.12.9` for controller builds to include
    security fixes
  * Added support for Kubernetes `1.16`
  * Upgraded client-go to `v12.0.0`

## edge-19.10.2

This edge release is a release candidate for `stable-2.6`.

* Controller
  * Added the destination container back to the controller; it had previously
    been separated into its own deployment. This ensures backwards
    compatibility and allows users to avoid data plane downtime during an
    upcoming upgrade to `stable-2.6`.

## edge-19.10.1

This edge release is a release candidate for `stable-2.6`.

* Proxy
  * Improved error logging when the proxy fails to emit trace spans
  * Fixed bug in distributed tracing where trace ids with fewer than 16 bytes
    were discarded
* Internal
  * Added integration tests for `linkerd edges` and `linkerd endpoints`

## edge-19.9.5

This edge release is a release candidate for `stable-2.6`.

* Helm
  * Added node selector constraints, so users can control which nodes the
    control plane is deployed to (thanks @bmcstdio!)
* CLI
  * Added request and response headers to the JSON output option for `linkerd
    tap`

## edge-19.9.4

This edge release introduces experimental support for distributed tracing as
well as a redesigned sidebar in the Web UI!

Experimental support for distributed tracing means that Linkerd data plane
proxies can now emit trace spans, allowing you to see the exact amount of time
spent in the Linkerd proxy for traced requests. The new
`config.linkerd.io/trace-collector` and
`config.alpha.linkerd.io/trace-collector-service-account` tracing annotations
allow specifying which pods should emit trace spans.

The goal of the dashboard's sidebar redesign was to reduce load on Prometheus
and simplify navigation by providing top-level views centered around
namespaces and workloads.

* CLI
  * Introduced a new `--cluster-domain` flag to the `linkerd install` command
    that allows setting a custom cluster domain (thanks @arminbuerkle!)
  * Fixed the `linkerd endpoints` command to use the correct Destination API
    address (thanks @Pothulapati!)
  * Added `--disable-heartbeat` flag for `linkerd` `install|upgrade` commands
* Controller
  * Instrumented the proxy-injector to provide additional metrics about
    injection (thanks @Pothulapati!)
  * Added support for `config.linkerd.io/admission-webhooks: disabled` label
    on namespaces so that the pods creation events in these namespaces are
    ignored by the proxy injector; this fixes situations in HA deployments
    where the proxy-injector is installed in `kube-system` (thanks
    @hasheddan!)
  * Introduced `config.linkerd.io/trace-collector` and
    `config.alpha.linkerd.io/trace-collector-service-account` pod spec
    annotations to support per-pod tracing
* Web UI
  * Workloads are now viewed by namespace, with an "All Namespaces" option, to
    improve dashboard performance
* Proxy
  * Added experimental distributed tracing support

## edge-19.9.3

* Helm
  * Allowed disabling namespace creation during install (thanks @KIVagant!)
* CLI
  * Added a new `json` output option to the `linkerd tap` command
* Controller
  * Fixed proxy injector timeout during a large number of concurrent
    injections
  * Separated the destination controller into its own separate deployment
  * Updated Prometheus config to keep only needed `cadvisor` metrics,
    substantially reducing the number of time-series stored in most clusters
* Web UI
  * Fixed bad request in the top routes tab on empty fields (thanks
    @pierDipi!)
* Proxy
  * Fixes to the client's backoff logic
  * Added 587 (SMTP) to the list of ports to ignore in protocol detection
    (bound to server-speaks-first protocols) (thanks @brianstorti!)

## edge-19.9.2

Much of our effort has been focused on improving our build and test
infrastructure, but this edge release lays the groundwork for some big new
features to land in the coming releases!

* Helm
  * There's now a public Helm repo! This release can be installed with: `helm
    repo add linkerd-edge https://helm.linkerd.io/edge && helm install
    linkerd-edge/linkerd2`
  * Improved TLS credential parsing by ignoring spurious newlines
* Proxy
  * Decreased proxy-init Docker image size by removing bundled debug tools
* Web UI
  * Fixed an issue where the edges table could end up with duplicates
  * Added an icon to more clearly label external links
* Internal
  * Upgraded client-go to v12.0.0
  * Moved CI from Travis to GitHub Actions

## edge-19.9.1

This edge release adds traffic splits into the Linkerd dashboard as well as a
variety of other improvements.

* CLI
  * Improved the error message when the CLI cannot connect to Kubernetes
    (thanks @alenkacz!)
  * Added `--address` flag to `linkerd dashboard` (thanks @bmcstdio!)
* Controller
  * Fixed an issue where the proxy-injector had insufficient RBAC permissions
  * Added support for disabling the heartbeat cronjob (thanks @kevtaylor!)
* Proxy
  * Decreased proxy Docker image size by removing bundled debug tools
  * Fixed an issue where the incorrect content-length could be set for GET
    requests with bodies
* Web UI
  * Added trafficsplits as a resource to the dashboard, including a
    trafficsplit detail page
* Internal
  * Added support for Kubernetes 1.16

## edge-19.8.7

* Controller
  * Added Kubernetes events (and log lines) when the proxy injector injects a
    deployment, and when injection is skipped
  * Additional preparation for configuring the cluster base domain (thanks
    @arminbuerkle!)
* Proxy
  * Changed the proxy to require the `LINKERD2_PROXY_DESTINATION_SVC_ADDR`
    environment variable when starting up
* Web UI
  * Increased dashboard speed by consolidating existing Prometheus queries

## edge-19.8.6

A new Grafana dashboard has been added which shows historical data for a
selected namespace. The build process for controller components now requires
`Go 1.12.9`. Additional contributions were made towards support for custom
cluster domains.

* Web UI
  * Added a `Linkerd Namespace` Grafana dashboard, allowing users to view
    historical data for a given namespace, similar to CLI output for `linkerd
    stat deploy -n myNs` (thanks @bourquep!)
* Internal
  * Added requirement for Go `1.12.9` for controller builds to include
    security fixes
  * Set `LINKERD2_PROXY_DESTINATION_GET_SUFFIXES` proxy environment variable,
    in preparation for custom cluster domain support (thanks @arminbuerkle!)

## stable-2.5.0

This release adds [Helm support](https://linkerd.io/2/tasks/install-helm/),
[tap authentication and authorization via RBAC](https://linkerd.io/tap-rbac),
traffic split stats, dynamic logging levels, a new cluster monitoring
dashboard, and countless performance enhancements and bug fixes.

For more details, see the announcement blog post:
<https://linkerd.io/2019/08/20/announcing-linkerd-2.5/>

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: Use the `linkerd upgrade` command to upgrade the control
plane. This command ensures that all existing control plane's configuration
and mTLS secrets are retained. For more details, please see the [upgrade
instructions](https://linkerd.io/2/tasks/upgrade/#upgrade-notice-stable-2-5-0).

**Special thanks to**: @alenkacz, @codeman9, @ethan-daocloud, @jonathanbeber,
and @Pothulapati!

**Full release notes**:

* CLI
  * **New** Updated `linkerd tap`, `linkerd top` and `linkerd profile --tap`
    to require `tap.linkerd.io` RBAC privileges. See
    <https://linkerd.io/tap-rbac> for more info
  * **New** Added traffic split metrics via `linkerd stat trafficsplits`
    subcommand
  * Made the `linkerd routes` command traffic split aware
  * Introduced the `linkerd --as` flag which allows users to impersonate
    another user for Kubernetes operations
  * Introduced the `--all-namespaces` (`-A`) option to the `linkerd get`,
    `linkerd edges` and `linkerd stat` commands to retrieve resources across
    all namespaces
  * Improved the installation report produced by the `linkerd check` command
    to include the control plane pods' live status
  * Fixed bug in the `linkerd upgrade config` command that was causing it to
    crash
  * Introduced `--use-wait-flag` to the `linkerd install-cni` command, to
    configure the CNI plugin to use the `-w` flag for `iptables` commands
  * Introduced `--restrict-dashboard-privileges` flag to `linkerd install`
    command, to disallow tap in the dashboard
  * Fixed `linkerd uninject` not removing `linkerd.io/inject: enabled`
    annotations
  * Fixed `linkerd stat -h` example commands (thanks @ethan-daocloud!)
  * Fixed incorrect "meshed" count in `linkerd stat` when resources share the
    same label selector for pods (thanks @jonathanbeber!)
  * Added pod status to the output of the `linkerd stat` command (thanks
    @jonathanbeber!)
  * Added namespace information to the `linkerd edges` command output and a
    new `-o wide` flag that shows the identity of the client and server if
    known
  * Added a check to the `linkerd check` command to validate the user has
    privileges necessary to create CronJobs
  * Added a new check to the `linkerd check --pre` command validating that if
    PSP is enabled, the NET_RAW capability is available
* Controller
  * **New** Disabled all unauthenticated tap endpoints. Tap requests now
    require [RBAC authentication and
    authorization](https://linkerd.io/tap-rbac)
  * The `l5d-require-id` header is now set on tap requests so that a
    connection is established over TLS
  * Introduced a new RoleBinding in the `kube-system` namespace to provide
    [access to tap](https://linkerd.io/tap-rbac)
  * Added HTTP security headers on all dashboard responses
  * Added support for namespace-level proxy override annotations (thanks
    @Pothulapati!)
  * Added resource limits when HA is enabled (thanks @Pothulapati!)
  * Added pod anti-affinity rules to the control plane pods when HA is enabled
    (thanks @Pothulapati!)
  * Fixed a crash in the destination service when an endpoint does not have a
    `TargetRef`
  * Updated the destination service to return `InvalidArgument` for external
    name services so that the proxy does not immediately fail the request
  * Fixed an issue with discovering StatefulSet pods via their unique hostname
  * Fixed an issue with traffic split where outbound proxy stats are missing
  * Upgraded the service profile CRD to v1alpha2. No changes required for
    users currently using v1alpha1
  * Updated the control plane's pod security policy to restrict workloads from
    running as `root` in the CNI mode (thanks @codeman9!)
  * Introduced optional cluster heartbeat cron job
  * Bumped Prometheus to 2.11.1
  * Bumped Grafana to 6.2.5
* Proxy
  * **New** Added a new `/proxy-log-level` endpoint to update the log level at
    runtime
  * **New** Updated the tap server to only admit requests from the control
    plane's tap controller
  * Added `request_handle_us` histogram to measure proxy overhead
  * Fixed gRPC client cancellations getting recorded as failures rather than
    as successful
  * Fixed a bug where tap would stop streaming after a short amount of time
  * Fixed a bug that could cause the proxy to leak service discovery
    resolutions to the Destination controller
* Web UI
  * **New** Added "Kubernetes cluster monitoring" Grafana dashboard with
    cluster and containers metrics
  * Updated the web server to use the new tap APIService. If the `linkerd-web`
    service account is not authorized to tap resources, users will see a link
    to documentation to remedy the error

## edge-19.8.5

This edge release is a release candidate for `stable-2.5`.

* CLI
  * Fixed CLI filepath issue on Windows
* Proxy
  * Fixed gRPC client cancellations getting recorded as failures rather than
    as successful

## edge-19.8.4

This edge release is a release candidate for `stable-2.5`.

* CLI
  * Introduced `--use-wait-flag` to the `linkerd install-cni` command, to
    configure the CNI plugin to use the `-w` flag for `iptables` commands
* Controller
  * Disabled the tap gRPC server listener. All tap requests now require RBAC
    authentication and authorization

## edge-19.8.3

This edge release introduces a new `linkerd stat trafficsplits` subcommand, to
show traffic split metrics. It also introduces a "Kubernetes cluster
monitoring" Grafana dashboard.

* CLI
  * Added traffic split metrics via `linkerd stat trafficsplits` subcommand
  * Fixed `linkerd uninject` not removing `linkerd.io/inject: enabled`
    annotations
  * Fixed `linkerd stat -h` example commands (thanks @ethan-daocloud!)
* Controller
  * Added support for namespace-level proxy override annotations
  * Removed unauthenticated tap from the Public API
* Proxy
  * Added `request_handle_us` histogram to measure proxy overhead
  * Updated the tap server to only admit requests from the control plane's tap
    controller
  * Fixed a bug where tap would stop streaming after a short amount of time
  * Fixed a bug that could cause the proxy to leak service discovery
    resolutions to the Destination controller
* Web UI
  * Added "Kubernetes cluster monitoring" Grafana dashboard with cluster and
    containers metrics
* Internal
  * Updated `linkerd install` and `linkerd upgrade` to use Helm charts for
    templating
  * Pinned Helm tooling to `v2.14.3`
  * Added Helm integration tests
  * Added container CPU and memory usage to `linkerd-heartbeat` requests
  * Removed unused inject code (thanks @alenkacz!)

## edge-19.8.2

This edge release introduces the new Linkerd control plane Helm chart, named
`linkerd2`. Helm users can now install and remove the Linkerd control plane by
using the `helm install` and `helm delete` commands. Proxy injection also now
uses Helm charts.

No changes were made to the existing `linkerd install` behavior.

For detailed installation steps using Helm, see the notes for
[#3146](https://github.com/linkerd/linkerd2/pull/3146).

* CLI
  * Updated `linkerd top` and `linkerd profile --tap` to require
    `tap.linkerd.io` RBAC privileges, see <https://linkerd.io/tap-rbac> for
    more info
  * Modified `tap.linkerd.io` APIService to enable usage in `kubectl auth
    can-i` commands
  * Introduced `--restrict-dashboard-privileges` flag to `linkerd install`
    command, to restrict the dashboard's default privileges to disallow tap
* Controller
  * Introduced a new ClusterRole, `linkerd-linkerd-tap-admin`, which gives
    cluster-wide tap privileges. Also introduced a new ClusterRoleBinding,
    `linkerd-linkerd-web-admin`, which binds the `linkerd-web` service account
    to the new tap ClusterRole
  * Removed successfully completed `linkerd-heartbeat` jobs from pod listing
    in the linkerd control plane to streamline `get po` output (thanks
    @Pothulapati!)
* Web UI
  * Updated the web server to use the new tap APIService. If the `linkerd-web`
    service account is not authorized to tap resources, users will see a link
    to documentation to remedy the error

## edge-19.8.1

### Significant Update

This edge release introduces a new tap APIService. The Kubernetes apiserver
authenticates the requesting tap user and then forwards tap requests to the
new tap APIServer. The `linkerd tap` command now makes requests against the
APIService.

With this release, users must be authorized via RBAC to use the `linkerd tap`
command. Specifically `linkerd tap` requires the `watch` verb on all resources
in the `tap.linkerd.io/v1alpha1` APIGroup. More granular access is also
available via sub-resources such as `deployments/tap` and `pods/tap`.

* CLI
  * Added a check to the `linkerd check` command to validate the user has
    privileges necessary to create CronJobs
  * Introduced the `linkerd --as` flag which allows users to impersonate
    another user for Kubernetes operations
  * The `linkerd tap` command now makes requests against the tap APIService
* Controller
  * Added HTTP security headers on all dashboard responses
  * Fixed nil pointer dereference in the destination service when an endpoint
    does not have a `TargetRef`
  * Added resource limits when HA is enabled
  * Added RSA support to TLS libraries
  * Updated the destination service to return `InvalidArgument` for external
    name services so that the proxy does not immediately fail the request
  * The `l5d-require-id` header is now set on tap requests so that a
    connection is established over TLS
  * Introduced the `APIService/v1alpha1.tap.linkerd.io` global resource
  * Introduced the `ClusterRoleBinding/linkerd-linkerd-tap-auth-delegator`
    global resource
  * Introduced the `Secret/linkerd-tap-tls` resource into the `linkerd`
    namespace
  * Introduced the `RoleBinding/linkerd-linkerd-tap-auth-reader` resource into
    the `kube-system` namespace
* Proxy
  * Added the `LINKERD2_PROXY_TAP_SVC_NAME` environment variable so that the
    tap server attempts to authorize client identities
* Internal
  * Replaced `dep` with Go modules for dependency management

## edge-19.7.5

* CLI
  * Improved the installation report produced by the `linkerd check` command
    to include the control plane pods' live status
  * Added the `--all-namespaces` (`-A`) option to the `linkerd get`, `linkerd
    edges` and `linkerd stat` commands to retrieve resources across all
    namespaces
* Controller
  * Fixed an issue with discovering StatefulSet pods via their unique hostname
  * Fixed an issue with traffic split where outbound proxy stats are missing
  * Bumped Prometheus to 2.11.1
  * Bumped Grafana to 6.2.5
  * Upgraded the service profile CRD to v1alpha2 where the openAPIV3Schema
    validation is replaced by a validating admission webhook. No changes
    required for users currently using v1alpha1
  * Updated the control plane's pod security policy to restrict workloads from
    running as `root` in the CNI mode (thanks @codeman9!)
  * Introduced cluster heartbeat cron job
* Proxy
  * Introduced the `l5d-require-id` header to enforce TLS outbound
    communication from the Tap server

## edge-19.7.4

* CLI
  * Made the `linkerd routes` command traffic-split aware
  * Fixed bug in the `linkerd upgrade config` command that was causing it to
    crash
  * Added pod status to the output of the `linkerd stat`command (thanks
    @jonathanbeber!)
  * Fixed incorrect "meshed" count in `linkerd stat` when resources share the
    same label selector for pods (thanks @jonathanbeber!)
  * Added namespace information to the `linkerd edges` command output and a
    new `-o wide` flag that shows the identity of the client and server if
    known
  * Added a new check to the `linkerd check --pre` command validating that if
    PSP is enabled, the NET_RAW capability is available
* Controller
  * Added pod anti-affinity rules to the control plane pods when HA is enabled
    (thanks @Pothulapati!)
* Proxy
  * Improved performance by using a constant-time load balancer
  * Added a new `/proxy-log-level` endpoint to update the log level at runtime

## stable-2.4.0

This release adds traffic splitting functionality, support for the Kubernetes
Service Mesh Interface (SMI), graduates high-availability support out of
experimental status, and adds a tremendous list of other improvements,
performance enhancements, and bug fixes.

Linkerd's new traffic splitting feature allows users to dynamically control
the percentage of traffic destined for a service. This powerful feature can be
used to implement rollout strategies like canary releases and blue-green
deploys. Support for the [Service Mesh Interface](https://smi-spec.io) (SMI)
makes it easier for ecosystem tools to work across all service mesh
implementations.

Along with the introduction of optional install stages via the `linkerd
install config` and `linkerd install control-plane` commands, the default
behavior of the `linkerd inject` command only adds annotations and defers
injection to the always-installed proxy injector component.

Finally, there have been many performance and usability improvements to the
proxy and UI, as well as production-ready features including:

* A new `linkerd edges` command that provides fine-grained observability into
  the TLS-based identity system
* A `--enable-debug-sidecar` flag for the `linkerd inject` command that
  improves debugging efforts

Linkerd recently passed a CNCF-sponsored security audit! Check out the
in-depth report
[here](https://github.com/linkerd/linkerd2/blob/master/SECURITY_AUDIT.pdf).

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: Use the `linkerd upgrade` command to upgrade the control
plane. This command ensures that all existing control plane's configuration
and mTLS secrets are retained. For more details, please see the [upgrade
instructions](https://linkerd.io/2/tasks/upgrade/#upgrade-notice-stable-2-4-0)
for more details.

**Special thanks to**: @alenkacz, @codeman9, @dwj300, @jackprice, @liquidslr,
@matej-g, @Pothulapati, @zaharidichev

**Full release notes**:

* CLI
  * **Breaking Change** Removed the `--proxy-auto-inject` flag, as the proxy
    injector is now always installed
  * **Breaking Change** Replaced the `--linkerd-version` flag with the
    `--proxy-version` flag in the `linkerd install` and `linkerd upgrade`
    commands, which allows setting the version for the injected proxy sidecar
    image, without changing the image versions for the control plane
  * Introduced install stages: `linkerd install config` and `linkerd install
    control-plane`
  * Introduced upgrade stages: `linkerd upgrade config` and `linkerd upgrade
    control-plane`
  * Introduced a new `--from-manifests` flag to `linkerd upgrade` allowing
    manually feeding a previously saved output of `linkerd install` into the
    command, instead of requiring a connection to the cluster to fetch the
    config
  * Introduced a new `--manual` flag to `linkerd inject` to output the proxy
    sidecar container spec
  * Introduced a new `--enable-debug-sidecar` flag to `linkerd inject`, that
    injects a debug sidecar to inspect traffic to and from the meshed pod
  * Added a new check for unschedulable pods and PSP issues (thanks,
    @liquidslr!)
  * Disabled the spinner in `linkerd check` when running without a TTY
  * Ensured the ServiceAccount for the proxy injector is created before its
    Deployment to avoid warnings when installing the proxy injector (thanks,
    @dwj300!)
  * Added a `linkerd check config` command for verifying that `linkerd install
    config` was successful
  * Improved the help documentation of `linkerd install` to clarify flag usage
  * Added support for private Kubernetes clusters by changing the CLI to
    connect to the control plane using a port-forward (thanks, @jackprice!)
  * Fixed `linkerd check` and `linkerd dashboard` failing when any control
    plane pod is not ready, even when multiple replicas exist (as in HA mode)
  * **New** Added a `linkerd edges` command that shows the source and
    destination name and identity for proxied connections, to assist in
    debugging
  * Tap can now be disabled for specific pods during injection by using the
    `--disable-tap` flag, or by using the `config.linkerd.io/disable-tap`
    annotation
  * Introduced pre-install healthcheck for clock skew (thanks, @matej-g!)
  * Added a JSON option to the `linkerd edges` command so that output is
    scripting friendly and can be parsed easily (thanks @alenkacz!)
  * Fixed an issue when Linkerd is installed with `--ha`, running `linkerd
    upgrade` without `--ha` will disable the high availability control plane
  * Fixed an issue with `linkerd upgrade` where running without `--ha` would
    unintentionally disable high availability features if they were previously
    enabled
  * Added a `--init-image-version` flag to `linkerd inject` to override the
    injected proxy-init container version
  * Added the `--linkerd-cni-enabled` flag to the `install` subcommands so
    that `NET_ADMIN` capability is omitted from the CNI-enabled control
    plane's PSP
  * Updated `linkerd check` to validate the caller can create
    `PodSecurityPolicy` resources
  * Added a check to `linkerd install` to prevent installing multiple control
    planes into different namespaces avoid conflicts between global resources
  * Added support for passing a URL directly to `linkerd inject` (thanks
    @Pothulapati!)
  * Added more descriptive output to the `linkerd check` output for control
    plane ReplicaSet readiness
  * Refactored the `linkerd endpoints` to use the same interface as used by
    the proxy for service discovery information
  * Fixed a bug where `linkerd inject` would fail when given a path to a file
    outside the current directory
  * Graduated high-availability support out of experimental status
  * Modified the error message for `linkerd install` to provide instructions
    for proceeding when an existing installation is found
* Controller
  * Added Go pprof HTTP endpoints to all control plane components' admin
    servers to better assist debugging efforts
  * Fixed bug in the proxy injector, where sporadically the pod workload owner
    wasn't properly determined, which would result in erroneous stats
  * Added support for a new `config.linkerd.io/disable-identity` annotation to
    opt out of identity for a specific pod
  * Fixed pod creation failure when a `ResourceQuota` exists by adding a
    default resource spec for the proxy-init init container
  * Fixed control plane components failing on startup when the Kubernetes API
    returns an `ErrGroupDiscoveryFailed`
  * Added Controller Component Labels to the webhook config resources (thanks,
    @Pothulapati!)
  * Moved the tap service into its own pod
  * **New** Control plane installations now generate a self-signed certificate
    and private key pair for each webhook, to prepare for future work to make
    the proxy injector and service profile validator HA
  * Added the `config.linkerd.io/enable-debug-sidecar` annotation allowing the
    `--enable-debug-sidecar` flag to work when auto-injecting Linkerd proxies
  * Added multiple replicas for the `proxy-injector` and `sp-validator`
    controllers when run in high availability mode (thanks to @Pothulapati!)
  * Defined least privilege default security context values for the proxy
    container so that auto-injection does not fail (thanks @codeman9!)
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
  * Added the `linkerd.io/control-plane-ns` label to all Linkerd resources
    allowing them to be identified using a label selector
  * Added Prometheus metrics for the Kubernetes watchers in the destination
    service for better visibility
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
  * Added errors totals to `response_total` metrics
  * Changed the load balancer to require that Kubernetes services are resolved
    via the control plane
  * Added the `NET_RAW` capability to the proxy-init container to be
    compatible with `PodSecurityPolicy`s that use `drop: all`
  * Fixed the proxy rejecting HTTP2 requests that don't have an `:authority`
  * Improved idle service eviction to reduce resource consumption for clients
    that send requests to many services
  * Fixed proxied HTTP/2 connections returning 502 errors when the upstream
    connection is reset, rather than propagating the reset to the client
  * Changed the proxy to treat unexpected HTTP/2 frames as stream errors
    rather than connection errors
  * Fixed a bug where DNS queries could persist longer than necessary
  * Improved router eviction to remove idle services in a more timely manner
  * Fixed a bug where the proxy would fail to process requests with obscure
    characters in the URI
* Web UI
  * Added the Font Awesome stylesheet locally; this allows both Font Awesome
    and Material-UI sidebar icons to display consistently with no/limited
    internet access (thanks again, @liquidslr!)
  * Removed the Authorities table and sidebar link from the dashboard to
    prepare for a new, improved dashboard view communicating authority data
  * Fixed dashboard behavior that caused incorrect table sorting
  * Removed the "Debug" page from the Linkerd dashboard while the
    functionality of that page is being redesigned
  * Added an Edges table to the resource detail view that shows the source,
    destination name, and identity for proxied connections
  * Improved UI for Edges table in dashboard by changing column names, adding
    a "Secured" icon and showing an empty Edges table in the case of no
    returned edges
* Internal
  * Known container errors were hidden in the integration tests; now they are
    reported in the output without having the tests fail
  * Fixed integration tests by adding known proxy-injector log warning to
    tests
  * Modified the integration test for `linkerd upgrade` in order to test
    upgrading from the latest stable release instead of the latest edge and
    reflect the typical use case
  * Moved the proxy-init container to a separate `linkerd/proxy-init` Git
    repository

## edge-19.7.3

* CLI
  * Graduated high-availability support out of experimental status
  * Modified the error message for `linkerd install` to provide instructions
    for proceeding when an existing installation is found
* Controller
  * Added Prometheus metrics for the Kubernetes watchers in the destination
    service for better visibility

## edge-19.7.2

* CLI
  * Refactored the `linkerd endpoints` to use the same interface as used by
    the proxy for service discovery information
  * Fixed a bug where `linkerd inject` would fail when given a path to a file
    outside the current directory
* Proxy
  * Fixed a bug where DNS queries could persist longer than necessary
  * Improved router eviction to remove idle services in a more timely manner
  * Fixed a bug where the proxy would fail to process requests with obscure
    characters in the URI

## edge-19.7.1

* CLI
  * Added more descriptive output to the `linkerd check` output for control
    plane ReplicaSet readiness
  * **Breaking change** Renamed `config.linkerd.io/debug` annotation to
    `config.linkerd.io/enable-debug-sidecar`, to match the
    `--enable-debug-sidecar` CLI flag that sets it
  * Fixed a bug in `linkerd edges` that caused incorrect identities to be
    displayed when requests were sent from two or more namespaces
* Controller
  * Added the `linkerd.io/control-plane-ns` label to the SMI Traffic Split CRD
* Proxy
  * Fixed proxied HTTP/2 connections returning 502 errors when the upstream
    connection is reset, rather than propagating the reset to the client
  * Changed the proxy to treat unexpected HTTP/2 frames as stream errors
    rather than connection errors

## edge-19.6.4

This release adds support for the SMI [Traffic
Split](https://github.com/deislabs/smi-spec/blob/master/traffic-split.md) API.
Creating a TrafficSplit resource will cause Linkerd to split traffic between
the specified backend services. Please see [the
spec](https://github.com/deislabs/smi-spec/blob/master/traffic-split.md) for
more details.

* CLI
  * Added a check to `install` to prevent installing multiple control planes
    into different namespaces
  * Added support for passing a URL directly to `linkerd inject` (thanks
    @Pothulapati!)
  * Added the `--all-namespaces` flag to `linkerd edges`
* Controller
  * Added support for the SMI TrafficSplit API which allows users to define
    traffic splits in TrafficSplit custom resources
* Web UI
  * Improved UI for Edges table in dashboard by changing column names, adding
    a "Secured" icon and showing an empty Edges table in the case of no
    returned edges

## edge-19.6.3

* CLI
  * Updated `linkerd check` to validate the caller can create
    `PodSecurityPolicy` resources
* Controller
  * Default the mutating and validating webhook configurations `sideEffects`
    property to `None` to indicate that the webhooks have no side effects on
    other resources (thanks @Pothulapati!)
* Proxy
  * Added the `NET_RAW` capability to the proxy-init container to be
    compatible with `PodSecurityPolicy`s that use `drop: all`
  * Fixed the proxy rejecting HTTP2 requests that don't have an `:authority`
  * Improved idle service eviction to reduce resource consumption for clients
    that send requests to many services
* Web UI
  * Removed the "Debug" page from the Linkerd dashboard while the
    functionality of that page is being redesigned
  * Added an Edges table to the resource detail view that shows the source,
    destination name, and identity for proxied connections

## edge-19.6.2

* CLI
  * Added the `--linkerd-cni-enabled` flag to the `install` subcommands so
    that `NET_ADMIN` capability is omitted from the CNI-enabled control
    plane's PSP
* Controller
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
* Proxy
  * The `l5d-override-dst` header is now used for inbound service profile
    discovery
  * Include errors in `response_total` metrics
  * Changed the load balancer to require that Kubernetes services are resolved
    via the control plane
* Web UI
  * Fixed dashboard behavior that caused incorrect table sorting

## edge-19.6.1

* CLI
  * Fixed an issue where, when Linkerd is installed with `--ha`, running
    `linkerd upgrade` without `--ha` will disable the high availability
    control plane
  * Added a `--init-image-version` flag to `linkerd inject` to override the
    injected proxy-init container version
* Controller
  * Added multiple replicas for the `proxy-injector` and `sp-validator`
    controllers when run in high availability mode (thanks to @Pothulapati!)
* Proxy
  * Fixed a memory leak that can occur if an HTTP/2 request with a payload
    ends before the entire payload is sent to the destination
* Internal
  * Moved the proxy-init container to a separate `linkerd/proxy-init` Git
    repository

## stable-2.3.2

This stable release fixes a memory leak in the proxy.

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Full release notes**:

* Proxy
  * Fixed a memory leak that can occur if an HTTP/2 request with a payload
    ends before the entire payload is sent to the destination

## edge-19.5.4

* CLI
  * Added a JSON option to the `linkerd edges` command so that output is
    scripting friendly and can be parsed easily (thanks @alenkacz!)
* Controller
  * **New** Control plane installations now generate a self-signed certificate
    and private key pair for each webhook, to prepare for future work to make
    the proxy injector and service profile validator HA
  * Added a debug container annotation, allowing the `--enable-debug-sidecar`
    flag to work when auto-injecting Linkerd proxies
* Proxy
  * Changed the proxy's routing behavior so that, when the control plane does
    not resolve a destination, the proxy forwards the request with minimal
    additional routing logic
  * Fixed a bug in the proxy's HPACK codec that could cause requests with very
    large header values to hang indefinitely
* Web UI
  * Removed the Authorities table and sidebar link from the dashboard to
    prepare for a new, improved dashboard view communicating authority data
* Internal
  * Modified the integration test for `linkerd upgrade` to test upgrading from
    the latest stable release instead of the latest edge, to reflect the
    typical use case

## stable-2.3.1

This stable release adds a number of proxy stability improvements.

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Special thanks to**: @zaharidichev and @11Takanori!

**Full release notes**:

* Proxy
  * Changed the proxy's routing behavior so that, when the control plane does
    not resolve a destination, the proxy forwards the request with minimal
    additional routing logic
  * Fixed a bug in the proxy's HPACK codec that could cause requests with very
    large header values to hang indefinitely
  * Replaced the fixed reconnect backoff with an exponential one (thanks,
    @zaharidichev!)
  * Fixed an issue where requests could be held indefinitely by the load
    balancer
  * Added a dispatch timeout that limits the amount of time a request can be
    buffered in the proxy
  * Removed the limit on the number of concurrently active service discovery
    queries to the destination service
  * Fixed an epoll notification issue that could cause excessive CPU usage
  * Added the ability to disable tap by setting an env var (thanks,
    @zaharidichev!)

## edge-19.5.3

* CLI
  * **New** Added a `linkerd edges` command that shows the source and
    destination name and identity for proxied connections, to assist in
    debugging
  * Tap can now be disabled for specific pods during injection by using the
    `--disable-tap` flag, or by using the `config.linkerd.io/disable-tap`
    annotation
  * Introduced pre-install healthcheck for clock skew (thanks, @matej-g!)
* Controller
  * Added Controller Component Labels to the webhook config resources (thanks,
    @Pothulapati!)
  * Moved the tap service into its own pod
* Proxy
  * Fix an epoll notification issue that could cause excessive CPU usage
  * Added the ability to disable tap by setting an env var (thanks,
    @zaharidichev!)

## edge-19.5.2

* CLI
  * Fixed `linkerd check` and `linkerd dashboard` failing when any control
    plane pod is not ready, even when multiple replicas exist (as in HA mode)
* Controller
  * Fixed control plane components failing on startup when the Kubernetes API
    returns an `ErrGroupDiscoveryFailed`
* Proxy
  * Added a dispatch timeout that limits the amount of time a request can be
    buffered in the proxy
  * Removed the limit on the number of concurrently active service discovery
    queries to the destination service

Special thanks to @zaharidichev for adding end to end tests for proxies with
TLS!

## edge-19.5.1

* CLI
  * Added a `linkerd check config` command for verifying that `linkerd install
    config` was successful
  * Improved the help documentation of `linkerd install` to clarify flag usage
  * Added support for private Kubernetes clusters by changing the CLI to
    connect to the control plane using a port-forward (thanks, @jackprice!)
* Controller
  * Fixed pod creation failure when a `ResourceQuota` exists by adding a
    default resource spec for the proxy-init init container
* Proxy
  * Replaced the fixed reconnect backoff with an exponential one (thanks,
    @zaharidichev!)
  * Fixed an issue where load balancers can become stuck
* Internal
  * Fixed integration tests by adding known proxy-injector log warning to
    tests

## edge-19.4.5

### Significant Update

As of this edge release the proxy injector component is always installed. To
have the proxy injector inject a pod you still can manually add the
`linkerd.io/inject: enable` annotation into the pod spec, or at the namespace
level to have all your pods be injected by default. With this release the
behaviour of the `linkerd inject` command changes, where the proxy sidecar
container YAML is no longer included in its output by default, but instead it
will just add the annotations to defer the injection to the proxy injector.
For use cases that require the full injected YAML to be output, a new
`--manual` flag has been added.

Another important update is the introduction of install stages. You still have
the old `linkerd install` command, but now it can be broken into `linkerd
install config` which installs the resources that require cluster-level
privileges, and `linkerd install control-plane` that continues with the
resources that only require namespace-level privileges. This also applies to
the `linkerd upgrade` command.

* CLI
  * **Breaking Change** Removed the `--proxy-auto-inject` flag, as the proxy
    injector is now always installed
  * **Breaking Change** Replaced the `--linkerd-version` flag with the
    `--proxy-version` flag in the `linkerd install` and `linkerd upgrade`
    commands, which allows setting the version for the injected proxy sidecar
    image, without changing the image versions for the control plane
  * Introduced install stages: `linkerd install config` and `linkerd install
    control-plane`
  * Introduced upgrade stages: `linkerd upgrade config` and `linkerd upgrade
    control-plane`
  * Introduced a new `--from-manifests` flag to `linkerd upgrade` allowing
    manually feeding a previously saved output of `linkerd install` into the
    command, instead of requiring a connection to the cluster to fetch the
    config
  * Introduced a new `--manual` flag to `linkerd inject` to output the proxy
    sidecar container spec
  * Introduced a new `--enable-debug-sidecar` option to `linkerd inject`, that
    injects a debug sidecar to inspect traffic to and from the meshed pod
  * Added a new check for unschedulable pods and PSP issues (thanks,
    @liquidslr!)
  * Disabled the spinner in `linkerd check` when running without a TTY
  * Ensured the ServiceAccount for the proxy injector is created before its
    Deployment to avoid warnings when installing the proxy injector (thanks,
    @dwj300!)

* Controller
  * Added Go pprof HTTP endpoints to all control plane components' admin
    servers to better assist debugging efforts
  * Fixed bug in the proxy injector, where sporadically the pod workload owner
    wasn't properly determined, which would result in erroneous stats
  * Added support for a new `config.linkerd.io/disable-identity` annotation to
    opt out of identity for a specific pod

* Web UI
  * Added the Font Awesome stylesheet locally; this allows both Font Awesome
    and Material-UI sidebar icons to display consistently with no/limited
    internet access (thanks again, @liquidslr!)

* Internal
  * Known container errors were hidden in the integration tests; now they are
    reported in the output, still without having the tests fail

## stable-2.3.0

This stable release introduces a new TLS-based service identity system into
the default Linkerd installation, replacing `--tls=optional` and the
`linkerd-ca` controller. Now, proxies generate ephemeral private keys into a
tmpfs directory and dynamically refresh certificates, authenticated by
Kubernetes ServiceAccount tokens, and tied to ServiceAccounts as the identity
primitive

In this release, all meshed HTTP communication is private and authenticated by
default.

Among the many improvements to the web dashboard, we've added a Community page
to surface news and updates from linkerd.io.

For more details, see the announcement blog post:
<https://linkerd.io/2019/04/16/announcing-linkerd-2.3/>

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: The `linkerd-ca` controller has been removed in favor of
the `linkerd-identity` controller. If you had previously installed Linkerd
with `--tls=optional`, manually delete the `linkerd-ca` deployment after
upgrading. Also, `--single-namespace` mode is no longer supported. For full
details on upgrading to this release, please see the [upgrade
instructions](https://linkerd.io/2/tasks/upgrade/#upgrade-notice-stable-2-3-0).

**Special thanks to**: @codeman9, @harsh-98, @huynq0911, @KatherineMelnyk,
@liquidslr, @paranoidaditya, @Pothulapati, @TwinProduction, and @yb172!

**Full release notes**:

* CLI
  * Introduced an `upgrade` command! This allows an existing Linkerd control
    plane to be reinstalled or reconfigured; it is particularly useful for
    automatically reusing flags set in the previous `install` or `upgrade`
  * Introduced the `linkerd metrics` command for fetching proxy metrics
  * **Breaking Change:** The `--linkerd-cni-enabled` flag has been removed
    from the `inject` command; CNI is configured at the cluster level with the
    `install` command and no longer applies to the `inject` command
  * **Breaking Change** Removed the `--disable-external-profiles` flag from
    the `install` command; external profiles are now disabled by default and
    can be enabled with the new `--enable-external-profiles` flag
  * **Breaking change** Removed the `--api-port` flag from the `inject` and
    `install` commands, since there's no benefit to running the control
    plane's destination API on a non-default port (thanks, @paranoidaditya)
  * **Breaking change** Removed the `--tls=optional` flag from the `linkerd
    install` command, since TLS is now enabled by default
  * Changed `install` to accept or generate an issuer Secret for the Identity
    controller
  * Changed `install` to fail in the case of a conflict with an existing
    installation; this can be disabled with the `--ignore-cluster` flag
  * Added the ability to adjust the Prometheus log level via
    `--controller-log-level`
  * Implemented `--proxy-cpu-limit` and `--proxy-memory-limit` for setting the
    proxy resources limits (`--proxy-cpu` and `--proxy-memory` were deprecated
    in favor of `proxy-cpu-request` and `proxy-memory-request`) (thanks
    @TwinProduction!)
  * Added a validator for the `--proxy-log-level` flag
  * Updated the `inject` and `uninject` subcommands to issue warnings when
    resources lack a `Kind` property (thanks @Pothulapati!)
  * The `inject` command proxy options are now converted into config
    annotations; the annotations ensure that these configs are persisted in
    subsequent resource updates
  * Changed `inject` to require fetching a configuration from the control
    plane; this can be disabled with the `--ignore-cluster` and
    `--disable-identity` flags, though this will prevent the injected pods
    from participating in mesh identity
  * Included kubectl version check as part of `linkerd check` (thanks @yb172!)
  * Updated `linkerd check` to ensure hint URLs are displayed for RPC checks
  * Fixed sporadic (and harmless) race condition error in `linkerd check`
  * Introduced a check for NET_ADMIN in `linkerd check`
  * Fixed permissions check for CRDs
  * Updated the `linkerd dashboard` command to serve the dashboard on a fixed
    port, allowing it to leverage browser local storage for user settings
  * Updated the `linkerd routes` command to display rows for routes that are
    not receiving any traffic
  * Added TCP stats to the stat command, under the `-o wide` and `-o json`
    flags
  * The `stat` command now always shows the number of open TCP connections
  * Removed TLS metrics from the `stat` command; this is in preparation for
    surfacing identity metrics in a clearer way
  * Exposed the `install-cni` command and its flags, and tweaked their
    descriptions
  * Eliminated false-positive vulnerability warnings related to go.uuid
* Controller
  * Added a new public API endpoint for fetching control plane configuration
  * **Breaking change** Removed support for running the control plane in
    single-namespace mode, which was severely limited in the number of
    features it supported due to not having access to cluster-wide resources;
    the end goal being Linkerd degrading gracefully depending on its
    privileges
  * Updated automatic proxy injection and CLI injection to support overriding
    inject defaults via pod spec annotations
  * Added support for the `config.linkerd.io/proxy-version` annotation on pod
    specs; this will override the injected proxy version
  * The auto-inject admission controller webhook is updated to watch pods
    creation and update events; with this change, proxy auto-injection now
    works for all kinds of workloads, including StatefulSets, DaemonSets,
    Jobs, etc
  * Service profile validation is now performed via a webhook endpoint; this
    prevents Kubernetes from accepting invalid service profiles
  * Changed the default CPU request from `10m` to `100m` for HA deployments;
    this will help some intermittent liveness/readiness probes from failing
    due to tight resource constraints
  * Updated destination service to return TLS identities only when the
    destination pod is TLS-aware and is in the same controller namespace
  * Lessen klog level to improve security
  * Updated control plane components to query Kubernetes at startup to
    determine authorized namespaces and if ServiceProfile support is available
  * Modified the stats payload to include the following TCP stats:
    `tcp_open_connections`, `tcp_read_bytes_total`, `tcp_write_bytes_total`
  * Instrumented clients in the control plane connecting to Kubernetes, thus
    providing better visibility for diagnosing potential problems with those
    connections
  * Renamed the "linkerd-proxy-api" service to "linkerd-destination"
  * Bumped Prometheus to version 2.7.1 and Grafana to version 5.4.3
* Proxy
  * Introduced per-proxy private key generation and dynamic certificate
    renewal
  * **Fixed** a connection starvation issue where TLS discovery detection on
    slow or idle connections could block all other connections from being
    accepted on the inbound listener of the proxy
  * **Fixed** a stream leak between the proxy and the control plane that could
    cause the `linkerd-controller` pod to use an excessive amount of memory
  * Added a readiness check endpoint on `:4191/ready` so that Kubernetes
    doesn't consider pods ready until they have acquired a certificate from
    the Identity controller
  * Some `l5d-*` informational headers have been temporarily removed from
    requests and responses because they could leak information to external
    clients
  * The proxy's connect timeouts have been updated, especially to improve
    reconnect behavior between the proxy and the control plane
  * Increased the inbound/router cap on MAX_CONCURRENT_STREAMS
  * The `l5d-remote-ip` header is now set on inbound requests and outbound
    responses
  * Fixed issue with proxy falling back to filesystem polling due to
    improperly sized inotify buffer
* Web UI
  * **New** Added a Community page to surface news and updates from linkerd.io
  * Added a Debug page to the web dashboard, allowing you to introspect
    service discovery state
  * The Overview page in the Linkerd dashboard now renders appropriately when
    viewed on mobile devices
  * Added filter functionality to the metrics tables
  * Added stable sorting for table rows
  * Added TCP stats to the Linkerd Pod Grafana dashboard
  * Added TCP stat tables on the namespace landing page and resource detail
    page
  * The topology graph now shows TCP stats if no HTTP stats are available
  * Improved table display on the resource detail page for resources with
    TCP-only traffic
  * Updated the resource detail page to start displaying a table with TCP
    stats
  * Modified the Grafana variable queries to use a TCP-based metric, so that
    if there is only TCP traffic then the dropdowns don't end up empty
  * Fixed sidebar not updating when resources were added/deleted (thanks
    @liquidslr!)
  * Added validation to the "new service profile" form (thanks @liquidslr!)
  * Added a Grafana dashboard and web tables for displaying Job stats (thanks,
    @Pothulapati!)
  * Removed TLS columns from the dashboard tables; this is in preparation for
    surfacing identity metrics in a clearer way
  * Fixed the behavior of the Top query 'Start' button if a user's query
    returns no data
  * Fixed an issue with the order of tables returned from a Top Routes query
  * Added text wrap for paths in the modal for expanded Tap query data
  * Fixed a quoting issue with service profile downloads (thanks, @liquidslr!)
  * Updated sorting of route table to move default routes to the bottom
  * Removed 'Help' hierarchy and surfaced links on navigation sidebar
  * Ensured that all the tooltips in Grafana displaying the series are shared
    across all the graphs
* Internals
  * Improved the `bin/go-run` script for the build process so that on failure,
    all associated background processes are terminated
  * Added more log errors to the integration tests
  * Removed the GOPATH dependence from the CLI dev environment
  * Consolidated injection code from CLI and admission controller code paths
  * Enabled the following linters: `unparam`, `unconvert`, `goimports`,
    `goconst`, `scopelint`, `unused`, `gosimple`
  * Bumped base Docker images
  * Added the flags `-update` and `-pretty-diff` to tests to allow overwriting
    fixtures and to print the full text of the fixtures upon mismatches
  * Introduced golangci-lint tooling, using `.golangci.yml` to centralize the
    config
  * Added a `-cover` parameter to track code coverage in go tests (more info
    in TEST.md)
  * Renamed a function in a test that was shadowing a go built-in function
    (thanks @huynq0911!)

## edge-19.4.4

* Proxy
  * **Fixed** a connection starvation issue where TLS discovery detection on
    slow or idle connections could block all other connections from being
    accepted on the inbound listener of the proxy
* CLI
  * **Fixed** `inject` to allow the `--disable-identity` flag to be used
    without having to specify the `--ignore-cluster` flag
* Web UI
  * The Overview page in the Linkerd dashboard now renders appropriately when
    viewed on mobile devices

## edge-19.4.3

* CLI
  * **Fixed** `linkerd upgrade` command not upgrading proxy containers (thanks
    @jon-walton for the issue report!)
  * **Fixed** `linkerd upgrade` command not installing the identity service
    when it was not already installed
  * Eliminate false-positive vulnerability warnings related to go.uuid

Special thanks to @KatherineMelnyk for updating the web component to read the
UUID from the `linkerd-config` ConfigMap!

## edge-19.4.2

* CLI
  * Removed TLS metrics from the `stat` command; this is in preparation for
    surfacing identity metrics in a clearer way
  * The `upgrade` command now outputs a URL that explains next steps for
    upgrading
  * **Breaking Change:** The `--linkerd-cni-enabled` flag has been removed
    from the `inject` command; CNI is configured at the cluster level with the
    `install` command and no longer applies to the `inject` command
* Controller
  * Service profile validation is now performed via a webhook endpoint; this
    prevents Kubernetes from accepting invalid service profiles
  * Added support for the `config.linkerd.io/proxy-version` annotation on pod
    specs; this will override the injected proxy version
  * Changed the default CPU request from `10m` to `100m` for HA deployments;
    this will help some intermittent liveness/readiness probes from failing
    due to tight resource constraints
* Proxy
  * The `CommonName` field on CSRs is now set to the proxy's identity name
* Web UI
  * Removed TLS columns from the dashboard tables; this is in preparation for
    surfacing identity metrics in a clearer way

## edge-19.4.1

* CLI
  * Introduced an `upgrade` command! This allows an existing Linkerd control
    plane to be reinstalled or reconfigured; it is particularly useful for
    automatically reusing flags set in the previous `install` or `upgrade`
  * The `inject` command proxy options are now converted into config
    annotations; the annotations ensure that these configs are persisted in
    subsequent resource updates
  * The `stat` command now always shows the number of open TCP connections
  * **Breaking Change** Removed the `--disable-external-profiles` flag from
    the `install` command; external profiles are now disabled by default and
    can be enabled with the new `--enable-external-profiles` flag
* Controller
  * The auto-inject admission controller webhook is updated to watch pods
    creation and update events; with this change, proxy auto-injection now
    works for all kinds of workloads, including StatefulSets, DaemonSets,
    Jobs, etc
* Proxy
  * Some `l5d-*` informational headers have been temporarily removed from
    requests and responses because they could leak information to external
    clients
* Web UI
  * The topology graph now shows TCP stats if no HTTP stats are available
  * Improved table display on the resource detail page for resources with
    TCP-only traffic
  * Added validation to the "new service profile" form (thanks @liquidslr!)

## edge-19.3.3

### Significant Update

This edge release introduces a new TLS Identity system into the default
Linkerd installation, replacing `--tls=optional` and the `linkerd-ca`
controller. Now, proxies generate ephemeral private keys into a tmpfs
directory and dynamically refresh certificates, authenticated by Kubernetes
ServiceAccount tokens, via the newly-introduced Identity controller.

Now, all meshed HTTP communication is private and authenticated by default.

* CLI
  * Changed `install` to accept or generate an issuer Secret for the Identity
    controller
  * Changed `install` to fail in the case of a conflict with an existing
    installation; this can be disabled with the `--ignore-cluster` flag
  * Changed `inject` to require fetching a configuration from the control
    plane; this can be disabled with the `--ignore-cluster` and
    `--disable-identity` flags, though this will prevent the injected pods
    from participating in mesh identity
  * **Breaking change** Removed the `--tls=optional` flag from the `linkerd
    install` command, since TLS is now enabled by default
  * Added the ability to adjust the Prometheus log level
* Proxy
  * **Fixed** a stream leak between the proxy and the control plane that could
    cause the `linkerd-controller` pod to use an excessive amount of memory
  * Introduced per-proxy private key generation and dynamic certificate
    renewal
  * Added a readiness check endpoint on `:4191/ready` so that Kubernetes
    doesn't consider pods ready until they have acquired a certificate from
    the Identity controller
  * The proxy's connect timeouts have been updated, especially to improve
    reconnect behavior between the proxy and the control plane
* Web UI
  * Added TCP stats to the Linkerd Pod Grafana dashboard
  * Fixed the behavior of the Top query 'Start' button if a user's query
    returns no data
  * Added stable sorting for table rows
  * Fixed an issue with the order of tables returned from a Top Routes query
  * Added text wrap for paths in the modal for expanded Tap query data
* Internal
  * Improved the `bin/go-run` script for the build process so that on failure,
    all associated background processes are terminated

Special thanks to @liquidslr for many useful UI and log changes, and to
@mmalone and @sourishkrout at @smallstep for collaboration and advice on the
Identity system!

## edge-19.3.2

* Controller
  * **Breaking change** Removed support for running the control plane in
    single-namespace mode, which was severely limited in the number of
    features it supported due to not having access to cluster-wide resources
  * Updated automatic proxy injection and CLI injection to support overriding
    inject defaults via pod spec annotations
  * Added a new public API endpoint for fetching control plane configuration
* CLI
  * **Breaking change** Removed the `--api-port` flag from the `inject` and
    `install` commands, since there's no benefit to running the control
    plane's destination API on a non-default port (thanks, @paranoidaditya)
  * Introduced the `linkerd metrics` command for fetching proxy metrics
  * Updated the `linkerd routes` command to display rows for routes that are
    not receiving any traffic
  * Updated the `linkerd dashboard` command to serve the dashboard on a fixed
    port, allowing it to leverage browser local storage for user settings
* Web UI
  * **New** Added a Community page to surface news and updates from linkerd.io
  * Fixed a quoting issue with service profile downloads (thanks, @liquidslr!)
  * Added a Grafana dashboard and web tables for displaying Job stats (thanks,
    @Pothulapati!)
  * Updated sorting of route table to move default routes to the bottom
  * Added TCP stat tables on the namespace landing page and resource detail
    page

## edge-19.3.1

* CLI
  * Introduced a check for NET_ADMIN in `linkerd check`
  * Fixed permissions check for CRDs
  * Included kubectl version check as part of `linkerd check` (thanks @yb172!)
  * Added TCP stats to the stat command, under the `-o wide` and `-o json`
    flags
* Controller
  * Updated the `mutatingwebhookconfiguration` so that it is recreated when
    the proxy injector is restarted, so that the MWC always picks up the
    latest config template during version upgrade
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
  * Updated control plane components to query Kubernetes at startup to
    determine authorized namespaces and if ServiceProfile support is available
  * Modified the stats payload to include the following TCP stats:
    `tcp_open_connections`, `tcp_read_bytes_total`, `tcp_write_bytes_total`
* Proxy
  * Fixed issue with proxy falling back to filesystem polling due to
    improperly sized inotify buffer
* Web UI
  * Removed 'Help' hierarchy and surfaced links on navigation sidebar
  * Added a Debug page to the web dashboard, allowing you to introspect
    service discovery state
  * Updated the resource detail page to start displaying a table with TCP
    stats
* Internal
  * Enabled the following linters: `unparam`, `unconvert`, `goimports`,
    `goconst`, `scopelint`, `unused`, `gosimple`
  * Bumped base Docker images

## stable-2.2.1

This stable release polishes some of the CLI help text and fixes two issues
that came up since the stable-2.2.0 release.

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
    proxy resources limits (`--proxy-cpu` and `--proxy-memory` were deprecated
    in favor of `proxy-cpu-request` and `proxy-memory-request`) (thanks
    @TwinProduction!)
  * Updated the `inject` and `uninject` subcommands to issue warnings when
    resources lack a `Kind` property (thanks @Pothulapati!)
  * Exposed the `install-cni` command and its flags, and tweaked their
    descriptions
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
  * Introduced golangci-lint tooling, using `.golangci.yml` to centralize the
    config
  * Added a `-cover` parameter to track code coverage in go tests (more info
    in TEST.md)
  * Added integration tests for `--single-namespace`
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
<https://blog.linkerd.io/2019/02/12/announcing-linkerd-2-2/>

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: The default behavior for proxy auto injection and service
profile ownership has changed as part of this release. Please see the [upgrade
instructions](https://linkerd.io/2/tasks/upgrade/#upgrade-notice-stable-2-2-0)
for more details.

**Special thanks to**: @alenkacz, @codeman9, @jonrichards, @radu-matei,
@yeya24, and @zknill

**Full release notes**:

* CLI
  * Improved service profile validation when running `linkerd check` in order
    to validate service profiles in all namespaces
  * Added the `linkerd endpoints` command to introspect Linkerd's service
    discovery state
  * Added the `--tap` flag to `linkerd profile` to generate service profiles
    using the route results seen during the tap
  * Added support for the `linkerd.io/inject: disabled` annotation on pod
    specs to disable injection for specific pods when running `linkerd inject`
  * Added support for `basePath` in OpenAPI 2.0 files when running `linkerd
    profile --open-api`
  * Increased `linkerd check` client timeout from 5 seconds to 30 seconds to
    fix issues for clusters with slow API servers
  * Updated `linkerd routes` to no longer return rows for `ExternalName`
    services in the namespace
  * Broadened the set of valid URLs when connecting to the Kubernetes API
  * Added the `--proto` flag to `linkerd profile` to output a service profile
    based on a Protobuf spec file
  * Fixed CLI connection failures to clusters that use self-signed
    certificates
  * Simplified `linkerd install` so that setting up proxy auto-injection (flag
    `--proxy-auto-inject`) no longer requires enabling TLS (flag `--tls`)
  * Added links for each `linkerd check` failure, pointing to a relevant
    section in our new FAQ page with resolution steps for each case
  * Added optional `linkerd install-sp` command to generate service profiles
    for the control plane, providing per-route metrics for control plane
    components
  * Removed `--proxy-bind-timeout` flag from `linkerd install` and `linkerd
    inject`, as the proxy no longer accepts this environment variable
  * Improved CLI appearance on Windows systems
  * Improved `linkerd check` output, fixed bug with `--single-namespace`
  * Fixed panic when `linkerd routes` is called in single-namespace mode
  * Added `linkerd logs` command to surface logs from any container in the
    Linkerd control plane
  * Added `linkerd uninject` command to remove the Linkerd proxy from a
    Kubernetes config
  * Improved `linkerd inject` to re-inject a resource that already has a
    Linkerd proxy
  * Improved `linkerd routes` to list all routes, including those without
    traffic
  * Improved readability in `linkerd check` and `linkerd inject` outputs
  * Adjusted the set of checks that are run before executing CLI commands,
    which allows the CLI to be invoked even when the control plane is not
    fully ready
  * Fixed reporting of injected resources when the `linkerd inject` command is
    run on `List` type resources with multiple items
  * Updated the `linkerd dashboard` command to use port-forwarding instead of
    proxying when connecting to the web UI and Grafana
  * Added validation for the `ServiceProfile` CRD
  * Updated the `linkerd check` command to disallow setting both the `--pre`
    and `--proxy` flags simultaneously
  * Added `--routes` flag to the `linkerd top` command, for grouping table
    rows by route instead of by path
  * Updated Prometheus configuration to automatically load `*_rules.yml` files
  * Removed TLS column from the `linkerd routes` command output
  * Updated `linkerd install` output to use non-default service accounts,
    `emptyDir` volume mounts, and non-root users
  * Removed cluster-wide resources from single-namespace installs
  * Fixed resource requests for proxy-injector container in `--ha` installs
* Controller
  * Fixed issue with auto-injector not setting the proxy ID, which is required
    to successfully locate client service profiles
  * Added full stat and tap support for DaemonSets and StatefulSets in the
    CLI, Grafana, and web UI
  * Updated auto-injector to use the proxy log level configured at install
    time
  * Fixed issue with auto-injector including TLS settings in injected pods
    even when TLS was not enabled
  * Changed automatic proxy injection to be opt-in via the `linkerd.io/inject`
    annotation on the pod or namespace
  * Move service profile definitions to client and server namespaces, rather
    than the control plane namespace
  * Added `linkerd.io/created-by` annotation to the linkerd-cni DaemonSet
  * Added a 10 second keepalive default to resolve dropped connections in
    Azure environments
  * Improved node selection for installing the linkerd-cni DaemonSet
  * Corrected the expected controller identity when configuring pods with TLS
  * Modified klog to be verbose when controller log-level is set to `debug`
  * Added support for retries and timeouts, configured directly in the service
    profile for each route
  * Added an experimental CNI plugin to avoid requiring the NET_ADMIN
    capability when injecting proxies
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
  * Fixed `linkerd dashboard` to maintain proxy connection when browser open
    fails
  * Fixed JavaScript bundling to avoid serving old versions after upgrade
  * Reduced the size of the webpack JavaScript bundle by nearly 50%
  * Fixed an indexing error on the top results page
  * Restored unmeshed resources in the network graph on the resource detail
    page
  * Adjusted label for unknown routes in route tables, added tooltip
  * Updated Top Routes page to persist form settings in URL
  * Added button to create new service profiles on Top Routes page
  * Fixed CLI commands displayed when linkerd is running in non-default
    namespace
* Proxy
  * Modified the way in which canonicalization warnings are logged to reduce
    the overall volume of error logs and make it clearer when failures occur
  * Added TCP keepalive configuration to fix environments where peers may
    silently drop connections
  * Updated the `Get` and `GetProfiles` APIs to accept a `proxy_id` parameter
    in order to return more tailored results
  * Removed TLS fallback-to-plaintext if handshake fails
  * Added the ability to override a proxy's normal outbound routing by adding
    an `l5d-override-dst` header
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
  * Improved service profile validation when running `linkerd check` in order
    to validate service profiles in all namespaces
* Controller
  * Added stat and tap support for StatefulSets in the CLI, Grafana, and web
    UI
  * Updated auto-injector to use the proxy log level configured at install
    time
  * Fixed issue with auto-injector including TLS settings in injected pods
    even when TLS was not enabled
* Proxy
  * Modified the way in which canonicalization warnings are logged to reduce
    the overall volume of error logs and make it clearer when failures occur

## edge-19.2.1

* Controller
  * **Breaking change** Changed automatic proxy injection to be opt-in via the
    `linkerd.io/inject` annotation on the pod or namespace. More info:
    <https://linkerd.io/2/proxy-injection/>
  * **Breaking change** `ServiceProfile`s are now defined in client and server
    namespaces, rather than the control plane namespace. `ServiceProfile`s
    defined in the client namespace take priority over ones defined in the
    server namespace
  * Added `linkerd.io/created-by` annotation to the linkerd-cni DaemonSet
    (thanks @codeman9!)
  * Added a 10 second keepalive default to resolve dropped connections in
    Azure environments
  * Improved node selection for installing the linkerd-cni DaemonSet (thanks
    @codeman9!)
  * Corrected the expected controller identity when configuring pods with TLS
  * Modified klog to be verbose when controller log-level is set to `Debug`
* CLI
  * Added the `linkerd endpoints` command to introspect Linkerd's service
    discovery state
  * Added the `--tap` flag to `linkerd profile` to generate a `ServiceProfile`
    by using the route results seen during the tap
  * Added support for the `linkerd.io/inject: disabled` annotation on pod
    specs to disable injection for specific pods when running `linkerd inject`
  * Added support for `basePath` in OpenAPI 2.0 files when running `linkerd
    profile --open-api`
  * Increased `linkerd check` client timeout from 5 seconds to 30 seconds to
    fix issues for clusters with a slower API server
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
  * Added support for timeouts! Configurable in the service profiles for each
    route
  * Added an experimental CNI plugin to avoid requiring the NET_ADMIN
    capability when injecting proxies (more details at
    <https://linkerd.io/2/cni)> (thanks @codeman9!)
  * Added more improvements to the API for `ListPods` (thanks @alenkacz!)
* Web UI
  * Grayed-out the tap icon for requests from sources that are not meshed
* CLI
  * Added the `--proto` flag to `linkerd profile` to output a service profile
    based on a Protobuf spec file
  * Fixed CLI connection failure to clusters that use self-signed certificates
  * Simplified `linkerd install` so that setting up proxy auto-injection (flag
    `--proxy-auto-inject`) no longer requires enabling TLS (flag `--tls`)
  * Added links for each `linkerd check` failure, pointing to a relevant
    section in our new FAQ page with resolution steps for each case

## edge-19.1.3

* Controller
  * Improved API for `ListPods` (thanks @alenkacz!)
  * Fixed `GetProfiles` API call not returning immediately when no profile
    exists (resulting in proxies logging warnings)
* Web UI
  * Improved resource detail pages now show all resource types
  * Fixed stats not appearing for routes that have service profiles installed
* CLI
  * Added optional `linkerd install-sp` command to generate service profiles
    for the control plane, providing per-route metrics for control plane
    components
  * Removed `--proxy-bind-timeout` flag from `linkerd install` and `linkerd
    inject` commands, as the proxy no longer accepts this environment variable
  * Improved CLI appearance on Windows systems
  * Improved `linkerd check` output, fixed check bug when using
    `--single-namespace` (thanks to @djeeg for the bug report!)
  * Improved `linkerd stat` now supports DaemonSets (thanks @zknill!)
  * Fixed panic when `linkerd routes` is called in single-namespace mode
* Proxy
  * Added the ability to override a proxy's normal outbound routing by adding
    an `l5d-override-dst` header
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
  * Fix `linkerd dashboard` to maintain proxy connection when browser open
    fails
  * Fix JavaScript bundling to avoid serving old versions after upgrade
* CLI
  * Add `linkerd logs` command to surface logs from any container in the
    Linkerd control plane (shout out to
    [Stern](https://github.com/wercker/stern)!)
  * Add `linkerd uninject` command to remove the Linkerd proxy from a
    Kubernetes config
  * Improve `linkerd inject` to re-inject a resource that already has a
    Linkerd proxy
  * Improve `linkerd routes` to list all routes, including those without
    traffic
  * Improve readability in `linkerd check` and `linkerd inject` outputs
* Proxy
  * Fix a deadlock in HTTP/2 stream reference counts

## edge-19.1.1

* CLI
  * Adjust the set of checks that are run before executing CLI commands, which
    allows the CLI to be invoked even when the control plane is not fully
    ready
  * Fix reporting of injected resources when the `linkerd inject` command is
    run on `List` type resources with multiple items
  * Update the `linkerd dashboard` command to use port-forwarding instead of
    proxying when connecting to the web UI and Grafana
  * Add validation for the `ServiceProfile` CRD (thanks, @alenkacz!)
  * Update the `linkerd check` command to disallow setting both the `--pre`
    and `--proxy` flags simultaneously (thanks again, @alenkacz!)
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
edge-18.12.1 release to reduce possible naming collisions. To upgrade an older
installation, see the [Upgrade Guide](https://linkerd.io/2/upgrade/).

* CLI
  * Add `--routes` flag to the `linkerd top` command, for grouping table rows
    by route instead of by path
  * Update Prometheus configuration to automatically load `*_rules.yml` files
  * Remove TLS column from the `linkerd routes` command output
* Web UI
  * Restore unmeshed resources in the network graph on the resource detail
    page
  * Reduce the overall size of the asset bundle for the web frontend
* Proxy
  * Improve configuration of the PeakEwma load balancer

Special thanks to @radu-matei for cleaning up a whole slew of Go lint
warnings, and to @jonrichards for improving the Rust build setup!

## edge-18.12.3

Upgrade notes: The control plane components have been renamed as of the
edge-18.12.1 release to reduce possible naming collisions. To upgrade an older
installation, see the [Upgrade Guide](https://linkerd.io/2/upgrade/).

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
  * Fix CLI commands displayed when linkerd is running in non-default
    namespace
* Proxy
  * Proxies with TLS enabled now honor ports configured to skip protocol
    detection

## stable-2.1.0

This stable release introduces several major improvements, including per-route
metrics, service profiles, and a vastly improved dashboard UI. It also adds
several significant experimental features, including proxy auto-injection,
single namespace installs, and a high-availability mode for the control plane.

For more details, see the announcement blog post:
<https://blog.linkerd.io/2018/12/06/announcing-linkerd-2-1/>

To install this release, run: `curl https://run.linkerd.io/install | sh`

**Upgrade notes**: The control plane components have been renamed in this
release to reduce possible naming collisions. Please make sure to read the
[upgrade
instructions](https://linkerd.io/2/upgrade/#upgrade-notice-stable-2-1-0) if
you are upgrading from the `stable-2.0.0` release.

**Special thanks to**: @alenkacz, @alpeb, @benjdlambert, @fahrradflucht,
@ffd2subroutine, @hypnoglow, @ihcsim, @lucab, and @rochacon

**Full release notes**:

* CLI
  * `linkerd routes` command displays per-route stats for *any resource*
  * Service profiles are now supported for external authorities
  * `linkerd routes --open-api` flag generates a service profile based on an
    OpenAPI specification (swagger) file
  * `linkerd routes` command displays per-route stats for services with
    service profiles
  * Add `--ha` flag to `linkerd install` command, for HA deployment of the
    control plane
  * Update stat command to accept multiple stat targets
  * Fix authority stat filtering when the `--from` flag is present
  * Various improvements to check command, including:
    * Emit warnings instead of errors when not running the latest version
    * Add retries if control plane health check fails initially
    * Run all pre-install RBAC checks, instead of stopping at first failure
  * Fixed an issue with the `--registry` install flag not accepting hosts with
    ports
  * Added an `--output` stat flag, for printing stats as JSON
  * Updated the `top` table to set column widths dynamically
  * Added a `--single-namespace` install flag for installing the control plane
    with Role permissions instead of ClusterRole permissions
  * Added a `--proxy-auto-inject` flag to the `install` command, allowing for
    auto-injection of sidecar containers
  * Added `--proxy-cpu` and `--proxy-memory` flags to the `install` and
    `inject` commands, giving the ability to configure CPU + Memory requests
  * Added a `--context` flag to specify the context to use to talk to the
    Kubernetes apiserver
  * The namespace in which Linkerd is installed is configurable via the
    `LINKERD_NAMESPACE` env var, in addition to the `--linkerd-namespace` flag
  * The wait time for the `check` and `dashboard` commands is configurable via
    the `--wait` flag
  * The `top` command now aggregates by HTTP method as well
* Controller
  * Rename snake case fields to camel case in service profile spec
  * Controller components are now prefixed with `linkerd-` to prevent name
    collisions with existing resources
  * `linkerd install --disable-h2-upgrade` flag has been added to control
    automatic HTTP/2 upgrading
  * Fix auto injection issue on Kubernetes `v1.9.11` that would merge, rather
    than append, the proxy container into the application
  * Fixed a few issues with auto injection via the proxy-injector webhook:
    * Injected pods now execute the linkerd-init container last, to avoid
      rerouting requests during pod init
    * Original pod labels and annotations are preserved when auto-injecting
  * CLI health check now uses unified endpoint for data plane checks
  * Include Licence files in all Docker images
* Proxy
  * The proxy's `tap` subsystem has been reimplemented to be more efficient
    and and reliable
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
  * Route metrics are now available in the resource detail pages for services
    with configured profiles
  * Service profiles can be created and downloaded from the Web UI
  * Top Routes page, served at `/routes`
  * Fixed a smattering of small UI issues
  * Added a new Grafana dashboard for authorities
  * Revamped look and feel of the Linkerd dashboard by switching component
    libraries from antd to material-ui
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
edge-18.12.1 release to reduce possible naming collisions. To upgrade an older
installation, see the [Upgrade Guide](https://linkerd.io/2/upgrade/).

* Controller
  * Rename snake case fields to camel case in service profile spec

## edge-18.12.1

Upgrade notes: The control plane components have been renamed in this release
to reduce possible naming collisions. To upgrade an existing installation:

* Install new CLI: `curl https://run.linkerd.io/install-edge | sh`
* Install new control plane: `linkerd install | kubectl apply -f -`
* Remove old deploys/cms: `kubectl -n linkerd get deploy,cm -oname | grep -v
  linkerd | xargs kubectl -n linkerd delete`
* Re-inject your applications: `linkerd inject my-app.yml | kubectl apply -f
  -`
* Remove old services: `kubectl -n linkerd get svc -oname | grep -v linkerd |
  xargs kubectl -n linkerd delete`

For more information, see the [Upgrade Guide](https://linkerd.io/2/upgrade/).

* CLI
  * **Improved** `linkerd routes` command displays per-route stats for *any
    resource*!
  * **New** Service profiles are now supported for external authorities!
  * **New** `linkerd routes --open-api` flag generates a service profile based
    on an OpenAPI specification (swagger) file
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
  * **New** `linkerd routes` command displays per-route stats for services
    with service profiles
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

This release brings major improvements to the CLI as described below,
including support for auto-injecting deployments via a Kubernetes Admission
Controller. Proxy auto-injection is **experimental**, and the implementation
may change going forward.

* CLI
  * **New** Added a `--proxy-auto-inject` flag to the `install` command,
    allowing for auto-injection of sidecar containers (Thanks @ihcsim!)
  * **Improved** Added `--proxy-cpu` and `--proxy-memory` flags to the
    `install` and `inject` commands, giving the ability to configure CPU +
    Memory requests (Thanks @benjdlambert!)
  * **Improved** Added a `--context` flag to specify the context to use to
    talk to the Kubernetes apiserver (Thanks @ffd2subroutine!)

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
  * **New** The namespace in which Linkerd is installed is configurable via
    the `LINKERD_NAMESPACE` env var, in addition to the `--linkerd-namespace`
    flag
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
    * Sidebar scrolls independently from main panel, scrollbars hidden when
      not needed
    * Removed social links from sidebar
* CLI
  * **New** `linkerd check` now validates Linkerd proxy versions and readiness
  * **New** `linkerd inject` now provides an injection status report, and
    warns when resources are not injectable
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

Special thanks to @sourishkrout for contributing a web readability fix!

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
  * All resource tables have been updated to display meshed pod counts, as
    well as an icon linking to the resource's Grafana dashboard if it is
    meshed
  * The UI now shows more useful information when server errors are
    encountered
* Proxy
  * The `h2` crate fixed a HTTP/2 window management bug
  * The `rustls` crate fixed a bug that could improperly fail TLS streams
* Control Plane
  * The tap server now hydrates metadata for both sources and destinations

## v18.8.1

* Web UI
  * **New** Tap UI makes it possible to query & inspect requests from the
    browser!
* Proxy
  * **New** Automatic, transparent HTTP/2 multiplexing of HTTP/1 traffic
    reduces the cost of short-lived HTTP/1 connections
* Control Plane
  * **Improved** `linkerd inject` now supports injecting all resources in a
    folder
  * **Fixed** `linkerd tap` no longer crashes when there are many pods
  * **New** Prometheus now only scrapes proxies belonging to its own linkerd
    install
  * **Fixed** Prometheus metrics collection for clusters with >100 pods

Special thanks to @ihcsim for contributing the `inject` improvement!

## v18.7.3

Linkerd2 v18.7.3 completes the rebranding from Conduit to Linkerd2, and
improves overall performance and stability.

* Proxy
  * **Improved** CPU utilization by ~20%
* Web UI
  * **Experimental** `/tap` page now supports additional filters
* Control Plane
  * Updated all k8s.io dependencies to 1.11.1

## v18.7.2

Linkerd2 v18.7.2 introduces new stability features as we work toward
production readiness.

* Control Plane
  * **Breaking change** Injected pod labels have been renamed to be more
    consistent with Kubernetes; previously injected pods must be re-injected
    with new version of linkerd CLI in order to work with updated control
    plane
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
  * Fix issue with destination service sending back incomplete pod metadata
  * Fix high CPU usage during proxy shutdown
  * ClusterRoles are now unique per Linkerd install, allowing multiple
    instances to be installed in the same Kubernetes cluster

## v0.5.0

Conduit v0.5.0 introduces a new, experimental feature that automatically
enables Transport Layer Security between Conduit proxies to secure application
traffic. It also adds support for HTTP protocol upgrades, so applications that
use WebSockets can now benefit from Conduit.

* Security
  * **New** `conduit install --tls=optional` enables automatic, opportunistic
    TLS. See [the docs][auto-tls] for more info.
* Production Readiness
  * The proxy now transparently supports HTTP protocol upgrades to support,
    for instance, WebSockets.
  * The proxy now seamlessly forwards HTTP `CONNECT` streams.
  * Controller services are now configured with liveness and readiness probes.
* User Interface
  * `conduit stat` now supports a virtual `authority` resource that aggregates
    traffic by the `:authority` (or `Host`) header of an HTTP request.
  * `dashboard`, `stat`, and `tap` have been updated to describe TLS state for
    traffic.
  * `conduit tap` now has more detailed information, including the direction
    of each message (outbound or inbound).
  * `conduit stat` now more-accurately records histograms for low-latency
    services.
  * `conduit dashboard` now includes error messages when a Conduit-enabled pod
    fails.
* Internals
  * Prometheus has been upgraded to v2.3.1.
  * A potential live-lock has been fixed in HTTP/2 servers.
  * `conduit tap` could crash due to a null-pointer access. This has been
    fixed.

[auto-tls]: docs/automatic-tls.md

## v0.4.4

Conduit v0.4.4 continues to improve production suitability and sets up
internals for the upcoming v0.5.0 release.

* Production Readiness
  * The destination service has been mostly-rewritten to improve safety and
    correctness, especially during controller initialization.
  * Readiness and Liveness checks have been added for some controller
    components.
  * RBAC settings have been expanded so that Prometheus can access node-level
    metrics.
* User Interface
  * Ad blockers like uBlock prevented the Conduit dashboard from fetching API
    data. This has been fixed.
  * The UI now highlights pods that have failed to start a proxy.
* Internals
  * Various dependency upgrades, including Rust 1.26.2.
  * TLS testing continues to bear fruit, precipitating stability improvements
    to dependencies like Rustls.

Special thanks to @alenkacz for improving docker build times!

## v0.4.3

Conduit v0.4.3 continues progress towards production readiness. It features a
new latency-aware load balancer.

* Production Readiness
  * The proxy now uses a latency-aware load balancer for outbound requests.
    This implementation is based on Finagle's Peak-EWMA balancer, which has
    been proven to significantly reduce tail latencies. This is the same load
    balancing strategy used by Linkerd.
* User Interface
  * `conduit stat` is now slightly more predictable in the way it outputs
    things, especially for commands like `watch conduit stat all
    --all-namespaces`.
  * Failed and completed pods are no longer shown in stat summary results.
* Internals
  * The proxy now supports some TLS configuration, though these features
    remain disabled and undocumented pending further testing and
    instrumentation.

Special thanks to @ihcsim for contributing his first PR to the project and to
@roanta for discussing the Peak-EWMA load balancing algorithm with us.

## v0.4.2

Conduit v0.4.2 is a major step towards production readiness. It features a
wide array of fixes and improvements for long-running proxies, and several new
telemetry features. It also lays the groundwork for upcoming releases that
introduce mutual TLS everywhere.

* Production Readiness
  * The proxy now drops metrics that do not update for 10 minutes, preventing
    unbounded memory growth for long-running processes.
  * The proxy now constrains the number of services that a node can route to
    simultaneously (default: 100). This protects long-running proxies from
    consuming unbounded resources by tearing down the longest-idle clients
    when the capacity is reached.
  * The proxy now properly honors HTTP/2 request cancellation.
  * The proxy could incorrectly handle requests in the face of some connection
    errors. This has been fixed.
  * The proxy now honors DNS TTLs.
  * `conduit inject` now works with `statefulset` resources.
* Telemetry
  * **New** `conduit stat` now supports the `all` Kubernetes resource, which
    shows traffic stats for all Kubernetes resources in a namespace.
  * **New** the Conduit web UI has been reorganized to provide namespace
    overviews.
  * **Fix** a bug in Tap that prevented the proxy from simultaneously
    satisfying more than one Tap request.
  * **Fix** a bug that could prevent stats from being reported for some TCP
    streams in failure conditions.
  * The proxy now measures response latency as time-to-first-byte.
* Internals
  * The proxy now supports user-friendly time values (e.g. `10s`) from
    environment configuration.
  * The control plane now uses client for Kubernetes 1.10.2.
  * Much richer proxy debug logging, including socket and stream metadata.
  * The proxy internals have been changed substantially in preparation for TLS
    support.

Special thanks to @carllhw, @kichristensen, & @sfroment for contributing to
this release!

### Upgrading from v0.4.1

When upgrading from v0.4.1, we suggest that the control plane be upgraded to
v0.4.2 before injecting application pods to use v0.4.2 proxies.

## v0.4.1

Conduit 0.4.1 builds on the telemetry work from 0.4.0, providing rich,
Kubernetes-aware observability and debugging.

* Web UI
  * **New** Automatically-configured Grafana dashboards for Services, Pods,
    ReplicationControllers, and Conduit mesh health.
  * **New** `conduit dashboard` Pod and ReplicationController views.
* Command-line interface
  * **Breaking change** `conduit tap` now operates on most Kubernetes
    resources.
  * `conduit stat` and `conduit tap` now both support kubectl-style resource
    strings (`deploy`, `deploy/web`, and `deploy web`), specifically:
    * `namespaces`
    * `deployments`
    * `replicationcontrollers`
    * `services`
    * `pods`
* Telemetry
  * **New** Tap support for filtering by and exporting destination metadata.
    Now you can sample requests from A to B, where A and B are any resource or
    group of resources.
  * **New** TCP-level stats, including connection counts and durations, and
    throughput, wired through to Grafana dashboards.
* Service Discovery
  * The proxy now uses the [trust-dns] DNS resolver. This fixes a number of
    DNS correctness issues.
  * The destination service could sometimes return incorrect, stale, labels
    for an endpoint. This has been fixed!

[trust-dns]: https://github.com/bluejekyll/trust-dns

## v0.4.0

Conduit 0.4.0 overhauls Conduit's telemetry system and improves service
discovery reliability.

* Web UI
  * **New** automatically-configured Grafana dashboards for all Deployments.
* Command-line interface
  * `conduit stat` has been completely rewritten to accept arguments like
    `kubectl get`. The `--to` and `--from` filters can be used to filter
    traffic by destination and source, respectively.  `conduit stat` currently
    can operate on `Namespace` and `Deployment` Kubernetes resources. More
    resource types will be added in the next release!
* Proxy (data plane)
  * **New** Prometheus-formatted metrics are now exposed on `:4191/metrics`,
    including rich destination labeling for outbound HTTP requests. The proxy
    no longer pushes metrics to the control plane.
  * The proxy now handles `SIGINT` or `SIGTERM`, gracefully draining requests
    until all are complete or `SIGQUIT` is received.
  * SMTP and MySQL (ports 25 and 3306) are now treated as opaque TCP by
    default. You should no longer have to specify `--skip-outbound-ports` to
    communicate with such services.
  * When the proxy reconnected to the controller, it could continue to send
    requests to old endpoints. Now, when the proxy reconnects to the
    controller, it properly removes invalid endpoints.
  * A bug impacting some HTTP/2 reset scenarios has been fixed.
* Service Discovery
  * Previously, the proxy failed to resolve some domain names that could be
    misinterpreted as a Kubernetes Service name. This has been fixed by
    extending the _Destination_ API with a negative acknowledgement response.
* Control Plane
  * The _Telemetry_ service and associated APIs have been removed.
* Documentation
  * Updated [Roadmap](doc/roadmap.md)

Special thanks to @ahume, @alenkacz, & @xiaods for contributing to this
release!

### Upgrading from v0.3.1

When upgrading from v0.3.1, it's important to upgrade proxies before upgrading
the controller. As you upgrade proxies, the controller will lose visibility
into some data plane stats. Once all proxies are updated, `conduit install
|kubectl apply -f -` can be run to upgrade the controller without causing any
data plane disruptions. Once the controller has been restarted, traffic stats
should become available.

## v0.3.1

Conduit 0.3.1 improves Conduit's resilience and transparency.

* Proxy (data plane)
  * The proxy now makes fewer changes to requests and responses being proxied.
    In particular, requests and responses without bodies or with empty bodies
    are better supported.
  * HTTP/1 requests with different `Host` header fields are no longer sent on
    the same HTTP/1 connection even when those hostnames resolve to the same
    IP address.
  * A connection leak during proxying of non-HTTP TCP connections was fixed.
  * The proxy now handles unavailable services more gracefully by timing out
    while waiting for an endpoint to become available for the service.
* Command-line interface
  * `$KUBECONFIG` with multiple paths is now supported. (PR #482 by
    @hypnoglow).
  * `conduit check` now checks for the availability of a Conduit update. (PR
    #460 by @ahume).
* Service Discovery
  * Kubernetes services with type `ExternalName` are now supported.
* Control Plane
  * The proxy is injected into the control plane during installation to
    improve the control plane's resilience and to "dogfood" the proxy.
  * The control plane is now more resilient regarding networking failures.
* Documentation
  * The markdown source for the documentation published at
    <https://conduit.io/docs/> is now open source at
    <https://github.com/runconduit/conduit/tree/master/doc.>

## v0.3.0

Conduit 0.3 focused heavily on production hardening of Conduit's telemetry
system. Conduit 0.3 should "just work" for most apps on Kubernetes 1.8 or 1.9
without configuration, and should support Kubernetes clusters with hundreds of
services, thousands of instances, and hundreds of RPS per instance.

With this release, Conduit also moves from _experimental_ to _alpha_---meaning
that we're ready for some serious testing and vetting from you. As part of
this, we've published the [Conduit roadmap](https://conduit.io/roadmap/), and
we've also launched some new mailing lists:
[conduit-users](https://groups.google.com/forum/#!forum/conduit-users),
[conduit-dev](https://groups.google.com/forum/#!forum/conduit-dev), and
[conduit-announce](https://groups.google.com/forum/#!forum/conduit-announce).

* CLI
  * CLI commands no longer depend on `kubectl`
  * `conduit dashboard` now runs on an ephemeral port, removing port 8001
    conflicts
  * `conduit inject` now skips pods with `hostNetwork=true`
  * CLI commands now have friendlier error messages, and support a `--verbose`
    flag for debugging
* Web UI
  * All displayed metrics are now instantaneous snapshots rather than
    aggregated over 10 minutes
  * The sidebar can now be collapsed
  * UX refinements and bug fixes
* Conduit proxy (data plane)
  * Proxy does load-aware (P2C + least-loaded) L7 balancing for HTTP
  * Proxy can now route to external DNS names
  * Proxy now properly sheds load in some pathological cases when it cannot
    route
* Telemetry system
  * Many optimizations and refinements to support scale goals
  * Per-path and per-pod metrics have been removed temporarily to improve
    scalability and stability; they will be reintroduced in Conduit 0.4 (#405)
* Build improvements
  * The Conduit docker images are now much smaller.
  * Dockerfiles have been changed to leverage caching, improving build times
    substantially

Known Issues:

* Some DNS lookups to external domains fail (#62, #155, #392)
* Applications that use WebSockets, HTTP tunneling/proxying, or protocols such
  as MySQL and SMTP, require additional configuration (#339)

## v0.2.0

This is a big milestone! With this release, Conduit adds support for HTTP/1.x
and raw TCP traffic, meaning it should "just work" for most applications that
are running on Kubernetes without additional configuration.

* Data plane
  * Conduit now transparently proxies all TCP traffic, including HTTP/1.x and
    HTTP/2. (See caveats below.)
* Command-line interface
  * Improved error handling for the `tap` command
  * `tap` also now works with HTTP/1.x traffic
* Dashboard
  * Minor UI appearance tweaks
  * Deployments now searchable from the dashboard sidebar

Caveats:

* Conduit will automatically work for most protocols. However, applications
  that use WebSockets, HTTP tunneling/proxying, or protocols such as MySQL and
  SMTP, will require some additional configuration. See the
  [documentation](https://conduit.io/adding-your-service/#protocol-support)
  for details.
* Conduit doesn't yet support external DNS lookups. These will be addressed in
  an upcoming release.
* There are known issues with Conduit's telemetry pipeline that prevent it
  from scaling beyond a few nodes. These will be addressed in an upcoming
  release.
* Conduit is still in alpha! Please help us by [filing issues and contributing
  pull requests](https://github.com/runconduit/conduit/issues/new).

## v0.1.3

* This is a minor bugfix for some web dashboard UI elements that were not
  rendering correctly.

## v0.1.2

Conduit 0.1.2 continues down the path of increasing usability and improving
debugging and introspection of the service mesh itself.

* Conduit CLI
  * New `conduit check` command reports on the health of your Conduit
    installation.
  * New `conduit completion` command provides shell completion.
* Dashboard
  * Added per-path metrics to the deployment detail pages.
  * Added animations to line graphs indicating server activity.
  * More descriptive CSS variable names. (Thanks @natemurthy!)
  * A variety of other minor UI bugfixes and improvements
* Fixes
  * Fixed Prometheus config when using RBAC. (Thanks @FaKod!)
  * Fixed `tap` failure when pods do not belong to a deployment. (Thanks
    @FaKod!)

## v0.1.1

Conduit 0.1.1 is focused on making it easier to get started with Conduit.

* Conduit can now be installed on Kubernetes clusters that use RBAC.
* The `conduit inject` command now supports a `--skip-outbound-ports` flag
  that directs Conduit to bypass proxying for specific outbound ports, making
  Conduit easier to use with non-gRPC or HTTP/2 protocols.
* The `conduit tap` command output has been reformatted to be line-oriented,
  making it easier to parse with common UNIX command line utilities.
* Conduit now supports routing of non-fully qualified domain names.
* The web UI has improved support for large deployments and deployments that
  don't have any inbound/outbound traffic.

## v0.1.0

Conduit 0.1.0 is the first public release of Conduit.

* This release supports services that communicate via gRPC only. non-gRPC
  HTTP/2 services should work. More complete HTTP support, including HTTP/1.0
  and HTTP/1.1 and non-gRPC HTTP/2, will be added in an upcoming release.
* Kubernetes 1.8.0 or later is required.
* kubectl 1.8.0 or later is required. `conduit dashboard` will not work with
  earlier versions of kubectl.
* When deploying to Minikube, Minikube 0.23 or 0.24.1 or later are required.
  Earlier versions will not work.
* This release has been tested using Google Kubernetes Engine and Minikube.
  Upcoming releases will be tested on additional providers too.
* Configuration settings and protocols are not stable yet.
* Services written in Go must use grpc-go 1.3 or later to avoid [grpc-go bug
  #1120](https://github.com/grpc/grpc-go/issues/1120).
