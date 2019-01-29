package test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
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
		"linkerd-proxy-api",
		"linkerd-web",
	}

	linkerdDeployReplicas = map[string]int{
		"linkerd-controller": 1,
		"linkerd-grafana":    1,
		"linkerd-prometheus": 1,
		"linkerd-web":        1,
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
	out, _, err := TestHelper.LinkerdRun("check", "--pre", "--expected-version", TestHelper.GetVersion())
	if err != nil {
		t.Fatalf("Check command failed\n%s", out)
	}

	err = TestHelper.ValidateOutput(out, "check.pre.golden")
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
		linkerdDeployReplicas["linkerd-ca"] = 1
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
	for deploy, replicas := range linkerdDeployReplicas {
		if err := TestHelper.CheckPods(TestHelper.GetLinkerdNamespace(), deploy, replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating pods for deploy [%s]:\n%s", deploy, err))
		}
		if err := TestHelper.CheckDeployment(TestHelper.GetLinkerdNamespace(), deploy, replicas); err != nil {
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
	out, _, err := TestHelper.LinkerdRun(
		"check",
		"--expected-version",
		TestHelper.GetVersion(),
		"--wait=0",
	)

	if err != nil {
		t.Fatalf("Check command failed\n%s", out)
	}

	err = TestHelper.ValidateOutput(out, "check.golden")
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
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
	out, _, err := TestHelper.LinkerdRun(
		"check",
		"--proxy",
		"--expected-version",
		TestHelper.GetVersion(),
		"--namespace",
		prefixedNs,
		"--wait=0",
	)

	if err != nil {
		t.Fatalf("Check command failed\n%s", out)
	}

	err = TestHelper.ValidateOutput(out, "check.proxy.golden")
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}
}
