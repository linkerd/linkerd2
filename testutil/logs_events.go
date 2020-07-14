package testutil

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

var (
	// Linkerd commonly logs these errors during testing, remove these once
	// they're addressed: https://github.com/linkerd/linkerd2/issues/2453
	knownControllerErrorsRegex = regexp.MustCompile(strings.Join([]string{
		`.*linkerd-controller-.*-.* tap time=".*" level=error msg="\[.*\] encountered an error: rpc error: code = Canceled desc = context canceled"`,
		`.*linkerd-web-.*-.* web time=".*" level=error msg="Post http://linkerd-controller-api\..*\.svc\.cluster\.local:8085/api/v1/Version: context canceled"`,
		`.*linkerd-proxy-injector-.*-.* proxy-injector time=".*" level=warning msg="failed to retrieve replicaset from indexer .*-smoke-test.*/smoke-test-.*-.*: replicaset\.apps \\"smoke-test-.*-.*\\" not found"`,
		`.*linkerd-destination-.* destination time=".*" level=warning msg="failed to retrieve replicaset from indexer .* not found"`,
		`.*linkerd-destination-.* destination time=".*" level=warning msg="context token ns:.* using old token format" addr=":8086" component=server`,
	}, "|"))

	knownProxyErrorsRegex = regexp.MustCompile(strings.Join([]string{
		// k8s hitting readiness endpoints before components are ready
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] proxy={server=in listen=0\.0\.0\.0:4143 remote=.*} linkerd2_proxy::app::errors unexpected error: an IO error occurred: Connection reset by peer \(os error 104\)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] proxy={server=in listen=0\.0\.0\.0:4143 remote=.*} linkerd2_proxy::(proxy::http::router service|app::errors unexpected) error: an error occurred trying to connect: Connection refused \(os error 111\) \(address: 127\.0\.0\.1:.*\)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] proxy={server=out listen=127\.0\.0\.1:4140 remote=.*} linkerd2_proxy::(proxy::http::router service|app::errors unexpected) error: an error occurred trying to connect: Connection refused \(os error 111\) \(address: .*\)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] proxy={server=out listen=127\.0\.0\.1:4140 remote=.*} linkerd2_proxy::(proxy::http::router service|app::errors unexpected) error: an error occurred trying to connect: operation timed out after 1s`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy WARN \[ *\d+.\d+s\] .* linkerd2_proxy::proxy::reconnect connect error to ControlAddr .*`,

		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] admin={server=metrics listen=0\.0\.0\.0:4191 remote=.*} linkerd2_proxy::control::serve_http error serving metrics: Error { kind: Shutdown, .* }`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web|tap)-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] admin={server=admin listen=127\.0\.0\.1:4191 remote=.*} linkerd2_proxy::control::serve_http error serving admin: Error { kind: Shutdown, cause: Os { code: 107, kind: NotConnected, message: "Transport endpoint is not connected" } }`,

		`.* linkerd-web-.*-.* linkerd-proxy WARN trust_dns_proto::xfer::dns_exchange failed to associate send_message response to the sender`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|web|tap)-.*-.* linkerd-proxy WARN \[.*\] linkerd2_proxy::proxy::canonicalize failed to refine linkerd-.*\..*\.svc\.cluster\.local: deadline has elapsed; using original name`,

		// prometheus scrape failures of control-plane
		`.* linkerd-prometheus-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] proxy={server=out listen=127\.0\.0\.1:4140 remote=.*} linkerd2_proxy::proxy::http::router service error: an error occurred trying to connect: .*`,
	}, "|"))

	knownEventWarningsRegex = regexp.MustCompile(strings.Join([]string{
		`MountVolume.SetUp failed for volume .* : couldn't propagate object cache: timed out waiting for the condition`, // pre k8s 1.16
		`MountVolume.SetUp failed for volume .* : failed to sync .* cache: timed out waiting for the condition`,         // post k8s 1.16
		`(Liveness|Readiness) probe failed: HTTP probe failed with statuscode: 50(2|3)`,
		`(Liveness|Readiness) probe failed: Get http://.*: dial tcp .*: connect: connection refused`,
		`(Liveness|Readiness) probe failed: Get http://.*: read tcp .*: read: connection reset by peer`,
		`(Liveness|Readiness) probe failed: Get http://.*: net/http: request canceled .*\(Client\.Timeout exceeded while awaiting headers\)`,
		`Failed to update endpoint .*/linkerd-.*: Operation cannot be fulfilled on endpoints "linkerd-.*": the object has been modified; please apply your changes to the latest version and try again`,
		`Error updating Endpoint Slices for Service .*/linkerd-.*: Error deleting linkerd-.* EndpointSlice for Service .*/linkerd-.*: endpointslices.discovery.k8s.io "linkerd-.*" not found`,
		`Error updating Endpoint Slices for Service .*/linkerd-.*: Error updating linkerd-.* EndpointSlice for Service .*/linkerd-.*: Operation cannot be fulfilled on endpointslices.discovery.k8s.io "linkerd-.*": the object has been modified; please apply your changes to the latest version and try again`,
		`error killing pod: failed to "KillPodSandbox" for ".*" with KillPodSandboxError: "rpc error: code = Unknown desc`,
		`failed to create containerd task: failed to start io pipe copy: unable to copy pipes: containerd-shim: opening w/o fifo "/run/containerd/io.containerd.grpc.v1.cri/containers/linkerd-proxy/io/\d+/linkerd-proxy-stdout"`,
	}, "|"))
)

// FetchAndCheckLogs retrieves the logs from the control plane containers and matches them
// to the list in knownControllerErrorsRegex and knownProxyErrorsRegex.
// It returns the list of matched entries and the list of unmatched entries (as errors).
func FetchAndCheckLogs(helper *TestHelper) ([]string, []error) {
	okMessages := []string{}
	errs := []error{}

	controllerRegex := regexp.MustCompile("level=(panic|fatal|error|warn)")
	proxyRegex := regexp.MustCompile(fmt.Sprintf("%s (ERR|WARN)", k8s.ProxyContainerName))
	clientGoRegex := regexp.MustCompile("client-go@")
	hasClientGoLogs := false

	for deploy, spec := range LinkerdDeployReplicas {
		deploy := strings.TrimPrefix(deploy, "linkerd-")
		containers := append(spec.Containers, k8s.ProxyContainerName)

		for _, container := range containers {
			container := container // pin
			name := fmt.Sprintf("%s/%s", deploy, container)

			proxy := false
			errRegex := controllerRegex
			knownErrorsRegex := knownControllerErrorsRegex
			if container == k8s.ProxyContainerName {
				proxy = true
				errRegex = proxyRegex
				knownErrorsRegex = knownProxyErrorsRegex
			}

			outputStream, err := helper.LinkerdRunStream(
				"logs", "--no-color",
				"--control-plane-component", deploy,
				"--container", container,
			)
			if err != nil {
				errs = append(errs, fmt.Errorf("error running command:\n%s", err))
				continue
			}
			defer outputStream.Stop()
			// Ignore the error returned, since ReadUntil will return an error if it
			// does not return 10,000 after 2 seconds. We don't need 10,000 log lines.
			outputLines, _ := outputStream.ReadUntil(10000, 2*time.Second)
			if len(outputLines) == 0 {
				// Retry one time for 30 more seconds, in case the cluster is slow to
				// produce log lines.
				outputLines, _ = outputStream.ReadUntil(10000, 30*time.Second)
				if len(outputLines) == 0 {
					errs = append(errs, fmt.Errorf("no logs found for %s", name))
				}
			}

			for _, line := range outputLines {
				if errRegex.MatchString(line) {
					if knownErrorsRegex.MatchString(line) {
						// report all known logging errors in the output
						okMessages = append(okMessages, fmt.Sprintf("found known error in %s log: %s", name, line))
					} else {
						if proxy {
							okMessages = append(okMessages, fmt.Sprintf("found unexpected proxy error in %s log: %s", name, line))
						} else {
							errs = append(errs, fmt.Errorf("found unexpected controller error in %s log: %s", name, line))
						}
					}
				}
				if clientGoRegex.MatchString((line)) {
					hasClientGoLogs = true
				}
			}
		}
	}
	if !hasClientGoLogs {
		errs = append(errs, errors.New("didn't find any client-go entries"))
	}

	return okMessages, errs
}

// FetchAndCheckEvents retrieves the events from k8s for the current namespace, matches
// them against knownEventWarningsRegex and returns the list of unmatched entries
// (as errors).
func FetchAndCheckEvents(helper *TestHelper) []error {
	out, err := helper.Kubectl("",
		"--namespace", helper.GetLinkerdNamespace(),
		"get", "events", "-ojson",
	)
	if err != nil {
		return []error{fmt.Errorf("'kubectl get events' command failed with %s\n%s", err, out)}
	}

	events, err := ParseEvents(out)
	if err != nil {
		return []error{err}
	}

	errs := []error{}
	for _, e := range events {
		if e.Type == corev1.EventTypeNormal {
			continue
		}

		evtStr := fmt.Errorf("found unexpected warning event: reason: [%s] Object: [%s] Message: [%s]", e.Reason, e.InvolvedObject.Name, e.Message)
		if !knownEventWarningsRegex.MatchString(e.Message) {
			errs = append(errs, evtStr)
		}
	}

	return errs
}
