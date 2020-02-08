package egress

import (
	"fmt"
	"os"
	"testing"

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

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestEgressHttp(t *testing.T) {
	out, stderr, err := TestHelper.LinkerdRun("inject", "testdata/proxy.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %v\n%s", err, stderr)
	}

	prefixedNs := TestHelper.GetTestNamespace("egress-test")
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(prefixedNs, nil)
	if err != nil {
		t.Fatalf("failed to create %s namespace: %s", prefixedNs, err)
	}
	out, err = TestHelper.KubectlApply(out, prefixedNs)
	if err != nil {
		t.Fatalf("Unexpected error: %v output:\n%s", err, out)
	}

	err = TestHelper.CheckPods(prefixedNs, "egress-test", 1)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	testCase := func(url, methodToUse string) {
		testName := fmt.Sprintf("Can use egress to send %s request to (%s)", methodToUse, url)
		t.Run(testName, func(t *testing.T) {
			cmd := []string{
				"-n", prefixedNs, "exec", "deploy/egress-test", "-c", "http-egress",
				"--", "curl", "-sko", "/dev/null", "-w", "%{http_code}", "-X", methodToUse, url,
			}
			out, err := TestHelper.Kubectl("", cmd...)
			if err != nil {
				t.Fatalf("Failed to exec %s: %s (%s)", cmd, err, out)
			}

			var status int
			_, err = fmt.Sscanf(out, "%d", &status)
			if err != nil {
				t.Fatalf("Failed to parse status code (%s): %s", out, err)
			}

			if status < 100 || status >= 500 {
				t.Fatalf("Got HTTP error code: %d\n", status)
			}
		})
	}

	supportedProtocols := []string{"http", "https"}
	methods := []string{"GET", "POST"}
	for _, protocolToUse := range supportedProtocols {
		for _, methodToUse := range methods {
			serviceName := fmt.Sprintf("%s://www.linkerd.io", protocolToUse)
			testCase(serviceName, methodToUse)
		}
	}

	// Test egress for a domain with fewer than 3 segments.
	testCase("http://linkerd.io", "GET")
}
