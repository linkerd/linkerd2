package skipports

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

var (
	skipPortsNs               = "skip-ports-test"
	booksappDeployments       = []string{"books", "traffic", "authors", "webapp"}
	httpResponseTotalMetricRE = regexp.MustCompile(
		`route_response_total\{direction="outbound",dst="books\.skip-ports-test\.svc\.cluster\.local:7002",classification="failure".*`,
	)
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestSkipInboundPorts(t *testing.T) {

	if os.Getenv("RUN_ARM_TEST") != "" {
		t.Skip("Skipping Skip Inbound Ports test. TODO: Build multi-arch emojivoto")
	}

	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, skipPortsNs, nil, t, func(t *testing.T, ns string) {
		out, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/skip_ports_application.yaml")
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd inject' command failed", err)
		}
		out, err = TestHelper.KubectlApply(out, ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		// Check all booksapp deployments are up and running
		for _, deploy := range booksappDeployments {
			if err := TestHelper.CheckPods(ctx, ns, deploy, 1); err != nil {
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		// Wait for slow-cookers to start sending requests
		time.Sleep(30 * time.Second)

		t.Run("expect webapp to not have any 5xx response errors", func(t *testing.T) {
			pods, err := TestHelper.GetPods(ctx, ns, map[string]string{"app": "webapp"})
			if err != nil {
				testutil.AnnotatedFatalf(t, "error getting pods", "error getting pods\n%s", err)
			}

			podName := fmt.Sprintf("pod/%s", pods[0].Name)
			cmd := []string{"diagnostics", "proxy-metrics", "--namespace", ns, podName}

			metrics, err := TestHelper.LinkerdRun(cmd...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "error getting metrics for pod", "error getting metrics for pod\n%s", err)
			}

			if httpResponseTotalMetricRE.MatchString(metrics) {
				testutil.AnnotatedFatalf(t, "expected not to find HTTP outbound response failures to dst=books.skip-ports-test.svc.cluster.local:7002",
					"expected not to find HTTP outbound requests when pod is skipping inbound port\n%s", metrics)
			}
		})
	})
}
