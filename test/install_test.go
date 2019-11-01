package test

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/testutil"
	corev1 "k8s.io/api/core/v1"
)

type deploySpec struct {
	replicas   int
	containers []string
}

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

var (
	linkerdSvcs = []string{
		"linkerd-controller-api",
		"linkerd-destination",
		"linkerd-grafana",
		"linkerd-identity",
		"linkerd-prometheus",
		"linkerd-web",
		"linkerd-tap",
	}

	linkerdDeployReplicas = map[string]deploySpec{
		"linkerd-controller":     {1, []string{"public-api"}},
		"linkerd-destination":    {1, []string{"destination"}},
		"linkerd-tap":            {1, []string{"tap"}},
		"linkerd-grafana":        {1, []string{}},
		"linkerd-identity":       {1, []string{"identity"}},
		"linkerd-prometheus":     {1, []string{}},
		"linkerd-sp-validator":   {1, []string{"sp-validator"}},
		"linkerd-web":            {1, []string{"web"}},
		"linkerd-proxy-injector": {1, []string{"proxy-injector"}},
	}

	// Linkerd commonly logs these errors during testing, remove these once
	// they're addressed: https://github.com/linkerd/linkerd2/issues/2453
	knownControllerErrorsRegex = regexp.MustCompile(strings.Join([]string{
		`.* linkerd-controller-.*-.* tap time=".*" level=error msg="\[.*\] encountered an error: rpc error: code = Canceled desc = context canceled"`,
		`.* linkerd-web-.*-.* web time=".*" level=error msg="Post http://linkerd-controller-api\..*\.svc\.cluster\.local:8085/api/v1/Version: context canceled"`,
		`.* linkerd-proxy-injector-.*-.* proxy-injector time=".*" level=warning msg="failed to retrieve replicaset from indexer, retrying with get request .*-smoke-test.*/smoke-test-.*-.*: replicaset\.apps \\"smoke-test-.*-.*\\" not found"`,
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
		`MountVolume.SetUp failed for volume .* : couldn't propagate object cache: timed out waiting for the condition`,
		`(Liveness|Readiness) probe failed: HTTP probe failed with statuscode: 50(2|3)`,
		`(Liveness|Readiness) probe failed: Get http://.*: dial tcp .*: connect: connection refused`,
		`(Liveness|Readiness) probe failed: Get http://.*: read tcp .*: read: connection reset by peer`,
		`(Liveness|Readiness) probe failed: Get http://.*: net/http: request canceled .*\(Client\.Timeout exceeded while awaiting headers\)`,
		`Failed to update endpoint .*-upgrade/linkerd-.*: Operation cannot be fulfilled on endpoints "linkerd-.*": the object has been modified; please apply your changes to the latest version and try again`,
		`error killing pod: failed to "KillPodSandbox" for ".*" with KillPodSandboxError: "rpc error: code = Unknown desc = failed to destroy network for sandbox \\".*\\": could not teardown ipv4 dnat: running \[/usr/sbin/iptables -t nat -X CNI-DN-.* --wait\]: exit status 1: iptables: No chain/target/match by that name\.\\n"`,
	}, "|"))

	injectionCases = []struct {
		ns          string
		annotations map[string]string
		injectArgs  []string
	}{
		{
			ns: "smoke-test",
			annotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			injectArgs: nil,
		},
		{
			ns:         "smoke-test-manual",
			injectArgs: []string{"--manual"},
		},
		{
			ns:         "smoke-test-ann",
			injectArgs: []string{},
		},
	}
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// Tests are executed in serial in the order defined
// Later tests depend on the success of earlier tests

func TestVersionPreInstall(t *testing.T) {
	version := "unavailable"
	if TestHelper.UpgradeFromVersion() != "" {
		version = TestHelper.UpgradeFromVersion()
	}

	err := TestHelper.CheckVersion(version)
	if err != nil {
		t.Fatalf("Version command failed\n%s", err.Error())
	}
}

func TestCheckPreInstall(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		t.Skip("Skipping pre-install check for upgrade test")
	}

	cmd := []string{"check", "--pre", "--expected-version", TestHelper.GetVersion()}
	golden := "check.pre.golden"
	out, _, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("Check command failed\n%s", out)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}
}

func TestInstallOrUpgradeCli(t *testing.T) {
	if TestHelper.GetHelmReleaseName() != "" {
		return
	}

	var (
		cmd  = "install"
		args = []string{
			"--controller-log-level", "debug",
			"--proxy-log-level", "warn,linkerd2_proxy=debug",
			"--proxy-version", TestHelper.GetVersion(),
		}
	)

	if TestHelper.GetClusterDomain() != "" {
		args = append(args, "--cluster-domain", TestHelper.GetClusterDomain())
	}

	if TestHelper.UpgradeFromVersion() != "" {
		cmd = "upgrade"

		// test 2-stage install during upgrade
		out, _, err := TestHelper.LinkerdRun(cmd, "config")
		if err != nil {
			t.Fatalf("linkerd upgrade config command failed\n%s", out)
		}

		// apply stage 1
		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			t.Fatalf("kubectl apply command failed\n%s", out)
		}

		// prepare for stage 2
		args = append([]string{"control-plane"}, args...)
	}

	exec := append([]string{cmd}, args...)
	out, _, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		t.Fatalf("linkerd install command failed\n%s", out)
	}

	// test `linkerd upgrade --from-manifests`
	if TestHelper.UpgradeFromVersion() != "" {
		manifests, err := TestHelper.Kubectl("",
			"--namespace", TestHelper.GetLinkerdNamespace(),
			"get", "configmaps/"+k8s.ConfigConfigMapName, "secrets/"+k8s.IdentityIssuerSecretName,
			"-oyaml",
		)
		if err != nil {
			t.Fatalf("kubectl get command failed with %s\n%s", err, out)
		}
		exec = append(exec, "--from-manifests", "-")
		upgradeFromManifests, stderr, err := TestHelper.PipeToLinkerdRun(manifests, exec...)
		if err != nil {
			t.Fatalf("linkerd upgrade --from-manifests command failed with %s\n%s\n%s", err, stderr, upgradeFromManifests)
		}

		if out != upgradeFromManifests {
			// retry in case it's just a discrepancy in the heartbeat cron schedule
			exec := append([]string{cmd}, args...)
			out, _, err := TestHelper.LinkerdRun(exec...)
			if err != nil {
				t.Fatalf("command failed: %v\n%s", exec, out)
			}

			if out != upgradeFromManifests {
				t.Fatalf("manifest upgrade differs from k8s upgrade.\nk8s upgrade:\n%s\nmanifest upgrade:\n%s", out, upgradeFromManifests)
			}
		}
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}
}

func TestInstallHelm(t *testing.T) {
	if TestHelper.GetHelmReleaseName() == "" {
		return
	}

	cn := fmt.Sprintf("identity.%s.cluster.local", TestHelper.GetLinkerdNamespace())
	root, err := tls.GenerateRootCAWithDefaults(cn)
	if err != nil {
		t.Fatalf("failed to generate root certificate for identity: %s", err)
	}

	args := []string{
		"--set", "ControllerLogLevel=debug",
		"--set", "LinkerdVersion=" + TestHelper.GetVersion(),
		"--set", "Proxy.Image.Version=" + TestHelper.GetVersion(),
		"--set", "Identity.TrustDomain=cluster.local",
		"--set", "Identity.TrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "Identity.Issuer.TLS.CrtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "Identity.Issuer.TLS.KeyPEM=" + root.Cred.EncodePrivateKeyPEM(),
		"--set", "Identity.Issuer.CrtExpiry=" + root.Cred.Crt.Certificate.NotAfter.Format(time.RFC3339),
	}
	if stdout, stderr, err := TestHelper.HelmRun("install", args...); err != nil {
		t.Fatalf("helm install command failed\n%s\n%s", stdout, stderr)
	}
}

func TestResourcesPostInstall(t *testing.T) {
	// Tests Namespace
	err := TestHelper.CheckIfNamespaceExists(TestHelper.GetLinkerdNamespace())
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}

	// Tests Services
	for _, svc := range linkerdSvcs {
		if err := TestHelper.CheckService(TestHelper.GetLinkerdNamespace(), svc); err != nil {
			t.Error(fmt.Errorf("Error validating service [%s]:\n%s", svc, err))
		}
	}

	// Tests Pods and Deployments
	for deploy, spec := range linkerdDeployReplicas {
		if err := TestHelper.CheckPods(TestHelper.GetLinkerdNamespace(), deploy, spec.replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating pods for deploy [%s]:\n%s", deploy, err))
		}
		if err := TestHelper.CheckDeployment(TestHelper.GetLinkerdNamespace(), deploy, spec.replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating deploy [%s]:\n%s", deploy, err))
		}
	}
}

func TestVersionPostInstall(t *testing.T) {
	err := TestHelper.CheckVersion(TestHelper.GetVersion())
	if err != nil {
		t.Fatalf("Version command failed\n%s", err.Error())
	}
}

// TODO: run this after a `linkerd install config`
func TestCheckConfigPostInstall(t *testing.T) {
	cmd := []string{"check", "config", "--expected-version", TestHelper.GetVersion(), "--wait=0"}
	golden := "check.config.golden"

	err := TestHelper.RetryFor(time.Minute, func() error {
		out, stderr, err := TestHelper.LinkerdRun(cmd...)

		if err != nil {
			return fmt.Errorf("Check command failed\n%s\n%s", stderr, out)
		}

		err = TestHelper.ValidateOutput(out, golden)
		if err != nil {
			return fmt.Errorf("Received unexpected output\n%s", err.Error())
		}

		return nil
	})
	if err != nil {
		t.Fatal(err.Error())
	}
}

func TestCheckPostInstall(t *testing.T) {
	cmd := []string{"check", "--expected-version", TestHelper.GetVersion(), "--wait=0"}
	golden := "check.golden"

	err := TestHelper.RetryFor(time.Minute, func() error {
		out, stderr, err := TestHelper.LinkerdRun(cmd...)

		if err != nil {
			return fmt.Errorf("Check command failed\n%s\n%s", stderr, out)
		}

		err = TestHelper.ValidateOutput(out, golden)
		if err != nil {
			return fmt.Errorf("Received unexpected output\n%s", err.Error())
		}

		return nil
	})
	if err != nil {
		t.Fatal(err.Error())
	}
}

func TestInstallSP(t *testing.T) {
	cmd := []string{"install-sp"}

	out, _, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("linkerd install-sp command failed\n%s", out)
	}

	out, err = TestHelper.KubectlApply(out, TestHelper.GetLinkerdNamespace())
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}
}

func TestDashboard(t *testing.T) {
	dashboardPort := 52237
	dashboardURL := fmt.Sprintf("http://localhost:%d", dashboardPort)

	outputStream, err := TestHelper.LinkerdRunStream("dashboard", "-p",
		strconv.Itoa(dashboardPort), "--show", "url")
	if err != nil {
		t.Fatalf("Error running command:\n%s", err)
	}
	defer outputStream.Stop()

	outputLines, err := outputStream.ReadUntil(4, 1*time.Minute)
	if err != nil {
		t.Fatalf("Error running command:\n%s", err)
	}

	output := strings.Join(outputLines, "")
	if !strings.Contains(output, dashboardURL) {
		t.Fatalf("Dashboard command failed. Expected url [%s] not present", dashboardURL)
	}

	resp, err := TestHelper.HTTPGetURL(dashboardURL + "/api/version")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !strings.Contains(resp, TestHelper.GetVersion()) {
		t.Fatalf("Dashboard command failed. Expected response [%s] to contain version [%s]",
			resp, TestHelper.GetVersion())
	}
}

func TestInject(t *testing.T) {
	resources, err := testutil.ReadFile("testdata/smoke_test.yaml")
	if err != nil {
		t.Fatalf("failed to read smoke test file: %s", err)
	}

	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			var out string

			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			err := TestHelper.CreateNamespaceIfNotExists(prefixedNs, tc.annotations)
			if err != nil {
				t.Fatalf("failed to create %s namespace: %s", prefixedNs, err)
			}

			if tc.injectArgs != nil {
				cmd := []string{"inject"}
				cmd = append(cmd, tc.injectArgs...)
				cmd = append(cmd, "testdata/smoke_test.yaml")

				var injectReport string
				out, injectReport, err = TestHelper.LinkerdRun(cmd...)
				if err != nil {
					t.Fatalf("linkerd inject command failed: %s\n%s", err, out)
				}

				err = TestHelper.ValidateOutput(injectReport, "inject.report.golden")
				if err != nil {
					t.Fatalf("Received unexpected output\n%s", err.Error())
				}
			} else {
				out = resources
			}

			out, err = TestHelper.KubectlApply(out, prefixedNs)
			if err != nil {
				t.Fatalf("kubectl apply command failed\n%s", out)
			}

			for _, deploy := range []string{"smoke-test-terminus", "smoke-test-gateway"} {
				err = TestHelper.CheckPods(prefixedNs, deploy, 1)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}

			url, err := TestHelper.URLFor(prefixedNs, "smoke-test-gateway", 8080)
			if err != nil {
				t.Fatalf("Failed to get URL: %s", err)
			}

			output, err := TestHelper.HTTPGetURL(url)
			if err != nil {
				t.Fatalf("Unexpected error: %v %s", err, output)
			}

			expectedStringInPayload := "\"payload\":\"BANANA\""
			if !strings.Contains(output, expectedStringInPayload) {
				t.Fatalf("Expected application response to contain string [%s], but it was [%s]",
					expectedStringInPayload, output)
			}
		})
	}
}

func TestServiceProfileDeploy(t *testing.T) {
	bbProto, err := TestHelper.HTTPGetURL("https://raw.githubusercontent.com/BuoyantIO/bb/master/api.proto")
	if err != nil {
		t.Fatalf("Unexpected error: %v %s", err, bbProto)
	}

	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			cmd := []string{"profile", "-n", prefixedNs, "--proto", "-", "smoke-test-terminus-svc"}
			bbSP, stderr, err := TestHelper.PipeToLinkerdRun(bbProto, cmd...)
			if err != nil {
				t.Fatalf("Unexpected error: %v %s", err, stderr)
			}

			out, err := TestHelper.KubectlApply(bbSP, prefixedNs)
			if err != nil {
				t.Fatalf("kubectl apply command failed: %s\n%s", err, out)
			}
		})
	}
}

func TestCheckProxy(t *testing.T) {
	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)
			cmd := []string{"check", "--proxy", "--expected-version", TestHelper.GetVersion(), "--namespace", prefixedNs, "--wait=0"}
			golden := "check.proxy.golden"

			err := TestHelper.RetryFor(time.Minute, func() error {
				out, _, err := TestHelper.LinkerdRun(cmd...)
				if err != nil {
					return fmt.Errorf("Check command failed\n%s", out)
				}

				err = TestHelper.ValidateOutput(out, golden)
				if err != nil {
					return fmt.Errorf("Received unexpected output\n%s", err.Error())
				}

				return nil
			})
			if err != nil {
				t.Fatal(err.Error())
			}
		})
	}
}

func TestLogs(t *testing.T) {
	controllerRegex := regexp.MustCompile("level=(panic|fatal|error|warn)")
	proxyRegex := regexp.MustCompile(fmt.Sprintf("%s (ERR|WARN)", k8s.ProxyContainerName))
	clientGoRegex := regexp.MustCompile("client-go@")
	hasClientGoLogs := false

	for deploy, spec := range linkerdDeployReplicas {
		deploy := strings.TrimPrefix(deploy, "linkerd-")
		containers := append(spec.containers, k8s.ProxyContainerName)

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

			t.Run(name, func(t *testing.T) {
				outputStream, err := TestHelper.LinkerdRunStream(
					"logs", "--no-color",
					"--control-plane-component", deploy,
					"--container", container,
				)
				if err != nil {
					t.Errorf("Error running command:\n%s", err)
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
						t.Errorf("No logs found for %s", name)
					}
				}

				for _, line := range outputLines {
					if errRegex.MatchString(line) {
						if knownErrorsRegex.MatchString(line) {
							// report all known logging errors in the output
							t.Logf("Found known error in %s log: %s", name, line)
						} else {
							if proxy {
								t.Logf("Found unexpected proxy error in %s log: %s", name, line)
							} else {
								t.Errorf("Found unexpected controller error in %s log: %s", name, line)
							}
						}
					}
					if clientGoRegex.MatchString((line)) {
						hasClientGoLogs = true
					}
				}
			})
		}
	}
	if !hasClientGoLogs {
		t.Errorf("Didn't find any client-go entries")
	}
}

func TestEvents(t *testing.T) {
	out, err := TestHelper.Kubectl("",
		"--namespace", TestHelper.GetLinkerdNamespace(),
		"get", "events", "-ojson",
	)
	if err != nil {
		t.Errorf("kubectl get events command failed with %s\n%s", err, out)
	}
	var list corev1.List
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Errorf("Error unmarshaling list from `kubectl get events`: %s", err)
	}

	if len(list.Items) == 0 {
		t.Error("No events found")
	}

	var unknownEvents []string
	for _, i := range list.Items {
		var e corev1.Event
		if err := json.Unmarshal(i.Raw, &e); err != nil {
			t.Errorf("Error unmarshaling list event from `kubectl get events`: %s", err)
		}

		if e.Type == corev1.EventTypeNormal {
			continue
		}

		evtStr := fmt.Sprintf("Reason: [%s] Object: [%s] Message: [%s]", e.Reason, e.InvolvedObject.Name, e.Message)
		if knownEventWarningsRegex.MatchString(e.Message) {
			t.Logf("Found known warning event: %s", evtStr)
		} else {
			unknownEvents = append(unknownEvents, evtStr)
		}
	}

	if len(unknownEvents) > 0 {
		t.Errorf("Found unexpected warning events:\n%s", strings.Join(unknownEvents, "\n"))
	}
}

func TestRestarts(t *testing.T) {
	for deploy, spec := range linkerdDeployReplicas {
		if err := TestHelper.CheckPods(TestHelper.GetLinkerdNamespace(), deploy, spec.replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating pods [%s]:\n%s", deploy, err))
		}
	}
}
