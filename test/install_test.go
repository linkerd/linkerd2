package test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/runconduit/conduit/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

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

func TestInstall(t *testing.T) {
	out, err := TestHelper.ConduitRun("install", "--conduit-version", TestHelper.GetVersion())
	if err != nil {
		t.Fatalf("conduit install command failed\n%s", out)
	}

	out, err = TestHelper.KubectlApply(out, TestHelper.GetConduitNamespace())
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}

	// Tests Namespace
	err = TestHelper.CheckIfNamespaceExists(TestHelper.GetConduitNamespace())
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}

	// Tests Services
	err = TestHelper.RetryFor(10*time.Second, func() error {
		for _, svc := range []string{"api", "proxy-api", "web"} {
			if err := TestHelper.CheckService(TestHelper.GetConduitNamespace(), svc); err != nil {
				return fmt.Errorf("Error validating service [%s]:\n%s", svc, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// Tests Pods
	err = TestHelper.RetryFor(30*time.Second, func() error {
		for _, deploy := range []string{"prometheus", "controller", "web"} {
			if err := TestHelper.CheckPods(TestHelper.GetConduitNamespace(), deploy, 1); err != nil {
				return fmt.Errorf("Error validating pods for deploy [%s]:\n%s", deploy, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// Tests Deployments
	err = TestHelper.RetryFor(30*time.Second, func() error {
		for deploy, replicas := range map[string]int{"controller": 1, "prometheus": 1, "web": 1} {
			if err := TestHelper.CheckDeployment(TestHelper.GetConduitNamespace(), deploy, replicas); err != nil {
				return fmt.Errorf("Error validating Deployment [%s]:\n%s", deploy, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func TestVersionPostInstall(t *testing.T) {
	err := TestHelper.RetryFor(30*time.Second, func() error {
		return TestHelper.CheckVersion(TestHelper.GetVersion())
	})
	if err != nil {
		t.Fatalf("Version command failed\n%s", err.Error())
	}
}

func TestCheck(t *testing.T) {
	var out string
	var err error
	overallErr := TestHelper.RetryFor(30*time.Second, func() error {
		out, err = TestHelper.ConduitRun("check", "--expected-version", TestHelper.GetVersion())
		return err
	})
	if overallErr != nil {
		t.Fatalf("Check command failed\n%s", out)
	}

	err = TestHelper.ValidateOutput(out, "check.golden")
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}
}

func TestDashboard(t *testing.T) {
	dashboardPort := 52237
	dashboardURL := fmt.Sprintf(
		"http://127.0.0.1:%d/api/v1/namespaces/%s/services/web:http/proxy",
		dashboardPort, TestHelper.GetConduitNamespace(),
	)

	outputStream, err := TestHelper.ConduitRunStream("dashboard", "-p",
		strconv.Itoa(dashboardPort), "--show", "url")
	if err != nil {
		t.Fatalf("Error running command:\n%s", err)
	}
	defer outputStream.Stop()

	outputLines, err := outputStream.ReadUntil(5, 1*time.Minute)
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
	out, err := TestHelper.ConduitRun("inject", "testdata/smoke_test.yaml")
	if err != nil {
		t.Fatalf("conduit inject command failed\n%s", out)
	}

	prefixedNs := TestHelper.GetTestNamespace("smoke-test")
	out, err = TestHelper.KubectlApply(out, prefixedNs)
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}

	svcURL, err := TestHelper.GetURLForService(prefixedNs, "smoke-test-gateway-svc")
	if err != nil {
		t.Fatalf("Failed to get service URL: %v", err)
	}

	output, err := TestHelper.HTTPGetURL(svcURL)
	if err != nil {
		t.Fatalf("Unexpected error: %v %s", err, output)
	}

	expectedStringInPayload := "\"payload\":\"BANANA\""
	if !strings.Contains(output, expectedStringInPayload) {
		t.Fatalf("Expected application response to contain string [%s], but it was [%s]",
			expectedStringInPayload, output)
	}
}
