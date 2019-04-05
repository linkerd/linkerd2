package test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
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
	}

	linkerdDeployReplicas = map[string]deploySpec{
		"linkerd-controller":   {1, []string{"destination", "public-api", "tap"}},
		"linkerd-grafana":      {1, []string{}},
		"linkerd-identity":     {1, []string{"identity"}},
		"linkerd-prometheus":   {1, []string{}},
		"linkerd-sp-validator": {1, []string{"sp-validator"}},
		"linkerd-web":          {1, []string{"web"}},
	}

	// Linkerd commonly logs these errors during testing, remove these once
	// they're addressed.
	// TODO: eliminate these errors: https://github.com/linkerd/linkerd2/issues/2453
	knownErrorsRegex = regexp.MustCompile(strings.Join([]string{

		// k8s hitting readiness endpoints before components are ready
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web)-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] proxy={server=in listen=0\.0\.0\.0:4143 remote=.*} linkerd2_proxy::app::errors unexpected error: an IO error occurred: Connection reset by peer (os error 104)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] proxy={server=in listen=0\.0\.0\.0:4143 remote=.*} linkerd2_proxy::(proxy::http::router service|app::errors unexpected) error: an error occurred trying to connect: Connection refused \(os error 111\) \(address: 127\.0\.0\.1:.*\)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] proxy={server=out listen=127\.0\.0\.1:4140 remote=.*} linkerd2_proxy::(proxy::http::router service|app::errors unexpected) error: an error occurred trying to connect: Connection refused \(os error 111\) \(address: .*\)`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web)-.*-.* linkerd-proxy WARN \[ *\d+.\d+s\] .* linkerd2_proxy::proxy::reconnect connect error to ControlAddr .*`,

		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web)-.*-.* linkerd-proxy ERR! \[ *\d+.\d+s\] admin={server=metrics listen=0\.0\.0\.0:4191 remote=.*} linkerd2_proxy::control::serve_http error serving metrics: Error { kind: Shutdown, .* }`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|sp-validator|web)-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] admin={server=admin listen=127\.0\.0\.1:4191 remote=.*} linkerd2_proxy::control::serve_http error serving admin: Error { kind: Shutdown, cause: Os { code: 107, kind: NotConnected, message: "Transport endpoint is not connected" } }`,

		`.* linkerd-controller-.*-.* tap time=".*" level=error msg="\[.*\] encountered an error: rpc error: code = Canceled desc = context canceled"`,
		`.* linkerd-web-.*-.* linkerd-proxy WARN trust_dns_proto::xfer::dns_exchange failed to associate send_message response to the sender`,
		`.* linkerd-(controller|identity|grafana|prometheus|proxy-injector|web)-.*-.* linkerd-proxy WARN \[.*\] linkerd2_proxy::proxy::canonicalize failed to refine linkerd-.*\..*\.svc\.cluster\.local: deadline has elapsed; using original name`,

		`.* linkerd-web-.*-.* web time=".*" level=error msg="Post http://linkerd-controller-api\..*\.svc\.cluster\.local:8085/api/v1/Version: context canceled"`,

		// prometheus scrape failures of control-plane
		`.* linkerd-prometheus-.*-.* linkerd-proxy ERR! \[ +\d+.\d+s\] proxy={server=out listen=127\.0\.0\.1:4140 remote=.*} linkerd2_proxy::proxy::http::router service error: an error occurred trying to connect: .*`,
	}, "|"))
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
	if TestHelper.AutoInject() {
		cmd = append(cmd, []string{"--proxy-auto-inject"}...)
		linkerdDeployReplicas["linkerd-proxy-injector"] = deploySpec{1, []string{"proxy-injector"}}
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

func TestCheckPostInstall(t *testing.T) {
	cmd := []string{"check", "--expected-version", TestHelper.GetVersion(), "--wait=0"}
	golden := "check.golden"

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
	var out string
	var err error

	prefixedNs := TestHelper.GetTestNamespace("smoke-test")

	if TestHelper.AutoInject() {
		out, err = testutil.ReadFile("testdata/smoke_test.yaml")
		if err != nil {
			t.Fatalf("failed to read smoke test file: %s", err)
		}
		err = TestHelper.CreateNamespaceIfNotExists(prefixedNs, map[string]string{
			k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
		})
		if err != nil {
			t.Fatalf("failed to create %s namespace with auto inject enabled: %s", prefixedNs, err)
		}
	} else {
		cmd := []string{"inject", "testdata/smoke_test.yaml"}

		var injectReport string
		out, injectReport, err = TestHelper.LinkerdRun(cmd...)
		if err != nil {
			t.Fatalf("linkerd inject command failed: %s\n%s", err, out)
		}

		err = TestHelper.ValidateOutput(injectReport, "inject.report.golden")
		if err != nil {
			t.Fatalf("Received unexpected output\n%s", err.Error())
		}
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
}

func TestServiceProfileDeploy(t *testing.T) {
	bbProto, err := TestHelper.HTTPGetURL("https://raw.githubusercontent.com/BuoyantIO/bb/master/api.proto")
	if err != nil {
		t.Fatalf("Unexpected error: %v %s", err, bbProto)
	}

	prefixedNs := TestHelper.GetTestNamespace("smoke-test")

	cmd := []string{"profile", "-n", prefixedNs, "--proto", "-", "smoke-test-terminus-svc"}
	bbSP, stderr, err := TestHelper.PipeToLinkerdRun(bbProto, cmd...)
	if err != nil {
		t.Fatalf("Unexpected error: %v %s", err, stderr)
	}

	out, err := TestHelper.KubectlApply(bbSP, prefixedNs)
	if err != nil {
		t.Fatalf("kubectl apply command failed: %s\n%s", err, out)
	}
}

func TestCheckProxy(t *testing.T) {
	prefixedNs := TestHelper.GetTestNamespace("smoke-test")
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
}

func TestLogs(t *testing.T) {
	controllerRegex := regexp.MustCompile("level=(panic|fatal|error|warn)")
	proxyRegex := regexp.MustCompile(fmt.Sprintf("%s (ERR|WARN)", k8s.ProxyContainerName))

	for deploy, spec := range linkerdDeployReplicas {
		deploy := strings.TrimPrefix(deploy, "linkerd-")
		containers := append(spec.containers, k8s.ProxyContainerName)

		for _, container := range containers {
			errRegex := controllerRegex
			if container == k8s.ProxyContainerName {
				errRegex = proxyRegex
			}

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
			// does not return 10,000 after 1 second. We don't need 10,000 log lines.
			outputLines, _ := outputStream.ReadUntil(10000, 2*time.Second)
			if len(outputLines) == 0 {
				t.Errorf("No logs found for %s/%s", deploy, container)
			}

			for _, line := range outputLines {
				if errRegex.MatchString(line) && !knownErrorsRegex.MatchString(line) {
					t.Errorf("Found error in %s/%s log: %s", deploy, container, line)
				}
			}
		}
	}
}

func TestRestarts(t *testing.T) {
	for deploy, spec := range linkerdDeployReplicas {
		if err := TestHelper.CheckPods(TestHelper.GetLinkerdNamespace(), deploy, spec.replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating pods [%s]:\n%s", deploy, err))
		}
	}
}
