package test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

type deploySpec struct {
	replicas   int
	containers []string
}

const (
	proxyContainer = "linkerd-proxy"
	initContainer  = "linkerd-init"
)

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
		"linkerd-grafana",
		"linkerd-prometheus",
		"linkerd-destination",
		"linkerd-web",
	}

	linkerdDeployReplicas = map[string]deploySpec{
		"linkerd-controller": {1, []string{"destination", "public-api", "tap"}},
		"linkerd-grafana":    {1, []string{}},
		"linkerd-prometheus": {1, []string{}},
		"linkerd-web":        {1, []string{"web"}},
	}

	// linkerd-proxy logs some errors when TLS is enabled, remove these once
	// they're addressed.
	knownProxyErrors = []string{
		`linkerd2_proxy::control::serve_http error serving metrics: Error { kind: Shutdown, cause: Os { code: 107, kind: NotConnected, message: "Transport endpoint is not connected" } }`,
		`linkerd-proxy ERR! admin={bg=tls-config} linkerd2_proxy::transport::tls::config error loading /var/linkerd-io/identity/certificate.crt: No such file or directory (os error 2)`,
		`linkerd-proxy ERR! admin={bg=tls-config} linkerd2_proxy::transport::tls::config error loading /var/linkerd-io/trust-anchors/trust-anchors.pem: No such file or directory (os error 2)`,
		`linkerd-proxy WARN admin={bg=tls-config} linkerd2_proxy::transport::tls::config error reloading TLS config: Io("/var/linkerd-io/identity/certificate.crt", Some(2)), falling back`,
		`linkerd-proxy WARN admin={bg=tls-config} linkerd2_proxy::transport::tls::config error reloading TLS config: Io("/var/linkerd-io/trust-anchors/trust-anchors.pem", Some(2)), falling back`,
	}
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// Tests are executed in serial in the order defined
// Later tests depend on the success of earlier tests

func TestVersionPreInstall(t *testing.T) {
	err := TestHelper.CheckVersion("unavailable")
	if err != nil {
		t.Fatalf("Version command failed\n%s", err.Error())
	}
}

func TestCheckPreInstall(t *testing.T) {
	cmd := []string{"check", "--pre", "--expected-version", TestHelper.GetVersion()}
	golden := "check.pre.golden"
	if TestHelper.SingleNamespace() {
		cmd = append(cmd, "--single-namespace")
		golden = "check.pre.single_namespace.golden"
		err := TestHelper.CreateNamespaceIfNotExists(TestHelper.GetLinkerdNamespace())
		if err != nil {
			t.Fatalf("Namespace creation failed\n%s", err.Error())
		}
	}
	out, _, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("Check command failed\n%s", out)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}
}

func TestInstall(t *testing.T) {
	cmd := []string{"install",
		"--controller-log-level", "debug",
		"--proxy-log-level", "warn,linkerd2_proxy=debug",
		"--linkerd-version", TestHelper.GetVersion(),
	}
	if TestHelper.TLS() {
		cmd = append(cmd, []string{"--tls", "optional"}...)
		linkerdDeployReplicas["linkerd-ca"] = deploySpec{1, []string{"ca"}}
	}
	if TestHelper.SingleNamespace() {
		cmd = append(cmd, "--single-namespace")
	}

	out, _, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("linkerd install command failed\n%s", out)
	}

	out, err = TestHelper.KubectlApply(out, TestHelper.GetLinkerdNamespace())
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}

	// Tests Namespace
	err = TestHelper.CheckIfNamespaceExists(TestHelper.GetLinkerdNamespace())
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

func TestCheckPostInstall(t *testing.T) {
	cmd := []string{"check", "--expected-version", TestHelper.GetVersion(), "--wait=0"}
	golden := "check.golden"
	if TestHelper.SingleNamespace() {
		cmd = append(cmd, "--single-namespace")
		golden = "check.single_namespace.golden"
	}

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
}

func TestDashboard(t *testing.T) {
	dashboardPort := 52237
	dashboardURL := fmt.Sprintf("http://127.0.0.1:%d", dashboardPort)

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
	cmd := []string{"inject", "testdata/smoke_test.yaml"}
	if TestHelper.TLS() {
		cmd = append(cmd, []string{"--tls", "optional"}...)
	}

	out, injectReport, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("linkerd inject command failed\n%s", out)
	}

	err = TestHelper.ValidateOutput(injectReport, "inject.report.golden")
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}

	prefixedNs := TestHelper.GetTestNamespace("smoke-test")
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
}

func TestCheckProxy(t *testing.T) {
	prefixedNs := TestHelper.GetTestNamespace("smoke-test")
	cmd := []string{"check", "--proxy", "--expected-version", TestHelper.GetVersion(), "--namespace", prefixedNs, "--wait=0"}
	golden := "check.proxy.golden"
	if TestHelper.SingleNamespace() {
		cmd = append(cmd, "--single-namespace")
		golden = "check.proxy.single_namespace.golden"
	}

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
}

func TestLogs(t *testing.T) {
	controllerRegex := regexp.MustCompile("level=(panic|fatal|error|warn)")
	proxyRegex := regexp.MustCompile(fmt.Sprintf("%s (ERR|WARN)", proxyContainer))

	for deploy, spec := range linkerdDeployReplicas {
		deploy := strings.TrimPrefix(deploy, "linkerd-")
		containers := append(spec.containers, proxyContainer)

		for _, container := range containers {
			regex := controllerRegex
			if container == proxyContainer {
				regex = proxyRegex
			}

			outputStream, err := TestHelper.LinkerdRunStream(
				"logs", "--no-color",
				"--control-plane-component", deploy,
				"--container", container,
			)
			if err != nil {
				t.Fatalf("Error running command:\n%s", err)
			}
			defer outputStream.Stop()
			outputLines, _ := outputStream.ReadUntil(100, 10*time.Second)
			if len(outputLines) == 0 {
				t.Fatalf("No logs found for %s/%s", deploy, container)
			}

			for _, line := range outputLines {
				if regex.MatchString(line) {

					// check for known errors
					known := false
					if TestHelper.TLS() && container == proxyContainer {
						for _, er := range knownProxyErrors {
							if strings.HasSuffix(line, er) {
								known = true
								break
							}
						}
					}

					if !known {
						t.Fatalf("Found error in %s/%s log: %s", deploy, container, line)
					}
				}
			}
		}
	}
}

func TestRestarts(t *testing.T) {
	for deploy, spec := range linkerdDeployReplicas {
		deploy := strings.TrimPrefix(deploy, "linkerd-")
		containers := append(spec.containers, proxyContainer, initContainer)

		for _, container := range containers {
			selector := fmt.Sprintf("linkerd.io/control-plane-component=%s", deploy)
			containerStatus := "containerStatuses"
			if container == initContainer {
				containerStatus = "initContainerStatuses"
			}
			output := fmt.Sprintf("jsonpath='{.items[*].status.%s[?(@.name==\"%s\")].restartCount}'", containerStatus, container)

			out, err := TestHelper.Kubectl(
				"-n", TestHelper.GetLinkerdNamespace(),
				"get", "pods",
				"--selector", selector,
				"-o", output,
			)
			if err != nil {
				t.Fatalf("kubectl command failed\n%s", out)
			}
			if out == "''" {
				t.Fatalf("Could not find restartCount for %s/%s", deploy, container)
			} else if out != "'0'" {
				t.Fatalf("Found %s restarts of %s/%s", out, deploy, container)
			}
		}
	}
}
