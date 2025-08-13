package smoke

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper *testutil.TestHelper
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane pods are running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestSmoke(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "smoke-test", map[string]string{}, t, func(t *testing.T, ns string) {
		cmd := []string{"inject", "testdata/smoke_test.yaml"}
		out, injectReport, err := TestHelper.PipeToLinkerdRun("", cmd...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
				"'linkerd inject' command failed: %s\n%s", err, out)
		}

		err = TestHelper.ValidateOutput(injectReport, "inject.report.golden")
		if err != nil {
			testutil.AnnotatedFatalf(t, "received unexpected output",
				"received unexpected output\n%s", err.Error())
		}

		out, err = TestHelper.KubectlApply(out, ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		// Wait for pods to in smoke-test deployment to come up
		for _, deploy := range []string{"smoke-test-terminus", "smoke-test-gateway"} {
			if err := TestHelper.CheckPods(ctx, ns, deploy, 1); err != nil {
				//nolint:errorlint
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedFatal(t, "CheckPods timed-out", err)
				}
			}
		}

		// Test 'linkerd check --proxy' with the current image version
		cmd = []string{"check", "--proxy", "--expected-version", TestHelper.GetVersion(), "--namespace", ns, "--wait=5m"}
		expected := getCheckOutput(t, "check.proxy.golden", TestHelper.GetLinkerdNamespace())

		// Use a short time window for check tests to get rid of transient
		// errors
		timeout := 5 * time.Minute
		err = testutil.RetryFor(timeout, func() error {
			out, err := TestHelper.LinkerdRun(cmd...)
			if err != nil {
				return fmt.Errorf("'linkerd check' command failed\n%w\n%s", err, out)
			}

			if !strings.Contains(out, expected) {
				return fmt.Errorf(
					"Expected:\n%s\nActual:\n%s", expected, out)
			}

			return nil
		})

		if err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd check' command timed-out (%s)", timeout), err)
		}

		// Test traffic from smoke-test client to server
		url, err := TestHelper.URLFor(ctx, ns, "smoke-test-gateway", 8080)
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to get URL",
				"failed to get URL: %s", err)
		}

		output, err := TestHelper.HTTPGetURL(url)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error",
				"unexpected error: %v %s", err, output)
		}

		expectedStringInPayload := "\"payload\":\"BANANA\""
		if !strings.Contains(output, expectedStringInPayload) {
			testutil.AnnotatedFatalf(t, "application response doesn't contain the expected response",
				"expected application response to contain string [%s], but it was [%s]",
				expectedStringInPayload, output)
		}
	})

}

func getCheckOutput(t *testing.T, goldenFile string, namespace string) string {
	pods, err := TestHelper.KubernetesHelper.GetPods(context.Background(), namespace, nil)
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to retrieve pods: %s", err), err)
	}

	proxyVersionErr := ""
	err = healthcheck.CheckProxyVersionsUpToDate(pods, version.Channels{})
	if err != nil {
		proxyVersionErr = err.Error()
	}

	tpl := template.Must(template.ParseFiles("testdata" + "/" + goldenFile))
	vars := struct {
		ProxyVersionErr string
		HintURL         string
	}{
		proxyVersionErr,
		healthcheck.HintBaseURL(TestHelper.GetVersion()),
	}

	var expected bytes.Buffer
	if err := tpl.Execute(&expected, vars); err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to parse %s template: %s", goldenFile, err), err)
	}

	return expected.String()
}
