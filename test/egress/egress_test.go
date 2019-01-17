package egress

import (
	"encoding/json"
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

func TestEgressHttp(t *testing.T) {
	out, _, err := TestHelper.LinkerdRun("inject", "testdata/proxy.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	prefixedNs := TestHelper.GetTestNamespace("egress-test")
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
			expectedURL := fmt.Sprintf("%s://%s/%s", protocolToUse, dnsName, strings.ToLower(methodToUse))

			url, err := TestHelper.URLFor(prefixedNs, deployName, 8080)
			if err != nil {
				t.Fatalf("Failed to get proxy URL: %s", err)
			}

			output, err := TestHelper.HTTPGetURL(url)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			var jsonResponse map[string]interface{}
			json.Unmarshal([]byte(output), &jsonResponse)

			payloadText := jsonResponse["payload"]
			if payloadText == nil {
				t.Fatalf("Expected [%s] request to [%s] to return a payload, got nil. Response:\n%s\n", methodToUse, expectedURL, output)
			}

			var messagePayload map[string]interface{}
			json.Unmarshal([]byte(payloadText.(string)), &messagePayload)

			actualURL := messagePayload["url"]
			if actualURL != expectedURL {
				t.Fatalf("Expecting response to say egress sent [%s] request to URL [%s] but got [%s]. Response:\n%s\n", methodToUse, expectedURL, actualURL, output)
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
