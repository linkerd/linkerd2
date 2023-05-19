package skipports

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
	"github.com/linkerd/linkerd2/testutil/prommatch"
)

var TestHelper *testutil.TestHelper

var (
	skipPortsNs         = "skip-ports-test"
	booksappDeployments = []string{"books", "traffic", "authors", "webapp"}
)

func secureRequestMatcher(dst string) *prommatch.Matcher {
	return prommatch.NewMatcher("request_total",
		prommatch.Labels{
			"direction":   prommatch.Equals("outbound"),
			"tls":         prommatch.Equals("true"),
			"dst_service": prommatch.Equals(dst),
		})
}

func insecureRequestMatcher(dst string) *prommatch.Matcher {
	return prommatch.NewMatcher("request_total",
		prommatch.Labels{
			"direction":   prommatch.Equals("outbound"),
			"tls":         prommatch.Equals("no_identity"),
			"dst_service": prommatch.Equals(dst),
		})
}

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
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
				//nolint:errorlint
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		t.Run("check webapp metrics", func(t *testing.T) {
			// Wait for slow-cookers to start sending requests by using a short
			// time window through RetryFor.
			err := testutil.RetryFor(30*time.Second, func() error {
				pods, err := TestHelper.GetPods(ctx, ns, map[string]string{"app": "webapp"})
				if err != nil {
					return fmt.Errorf("error getting pods\n%w", err)
				}

				podName := fmt.Sprintf("pod/%s", pods[0].Name)
				cmd := []string{"diagnostics", "proxy-metrics", "--namespace", ns, podName}

				metrics, err := TestHelper.LinkerdRun(cmd...)
				if err != nil {
					return fmt.Errorf("error getting metrics for pod\n%w", err)
				}
				s := prommatch.Suite{}.
					MustContain("secure requests to authors", secureRequestMatcher("authors")).
					MustContain("insecure requests to books", insecureRequestMatcher("books")).
					MustNotContain("insecure requests to authors", insecureRequestMatcher("authors")).
					MustNotContain("secure requests to books", secureRequestMatcher("books"))
				if err := s.CheckString(metrics); err != nil {
					return fmt.Errorf("error matching metrics\n%w", err)
				}
				return nil
			})

			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v", err)
			}
		})
	})
}
