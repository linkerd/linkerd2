package egress

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

var egressHTTPDeployments = []string{
	"egress-test-https-post",
	"egress-test-http-post",
	"egress-test-https-get",
	"egress-test-http-get",
	"egress-test-not-www-get",
}

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// issue: https://github.com/linkerd/linkerd2/issues/2316
//
// The response from `http://httpbin.org/get` is non-deterministic--returning
// either `http://..` or `https://..` for GET requests. As #2316 mentions,
// this test should not have an external dependency on this endpoint. As a
// workaround for edge-20.1.3, temporarily expect either `http` or `https` so
// that the test is not completely disabled.

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

	for _, deploy := range egressHTTPDeployments {
		err = TestHelper.CheckPods(prefixedNs, deploy, 1)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	}

	testCase := func(deployName, dnsName, protocolToUse, methodToUse string) {
		testName := fmt.Sprintf("Can use egress to send %s request to %s (%s)", methodToUse, protocolToUse, deployName)
		t.Run(testName, func(t *testing.T) {
			url, err := TestHelper.URLFor(prefixedNs, deployName, 8080)
			if err != nil {
				t.Fatalf("Failed to get proxy URL: %s", err)
			}

			rsp, err := TestHelper.HTTPGetRsp(url)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if rsp.StatusCode < 100 || rsp.StatusCode >= 500 {
				t.Fatalf("Got HTTP error code: %d\n", rsp.StatusCode)
			}
		})
	}

	supportedProtocols := []string{"http", "https"}
	methods := []string{"GET", "POST"}
	for _, protocolToUse := range supportedProtocols {
		for _, methodToUse := range methods {
			serviceName := fmt.Sprintf("egress-test-%s-%s", protocolToUse, strings.ToLower(methodToUse))
			testCase(serviceName, "www.httpbin.org", protocolToUse, methodToUse)
		}
	}

	// Test egress for a domain with fewer than 3 segments.
	testCase("egress-test-not-www-get", "httpbin.org", "https", "GET")
}
