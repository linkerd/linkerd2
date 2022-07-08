package localhost

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
	"github.com/linkerd/linkerd2/testutil/prommatch"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

var (
	nginxPodRE  = regexp.MustCompile(`nginx.*`)
	nginxLabels = prommatch.Labels{
		"direction":             prommatch.Equals("outbound"),
		"authority":             prommatch.Equals("nginx.linkerd-localhost-test.svc.cluster.local:8080"),
		"dst_deployment":        prommatch.Equals("nginx"),
		"dst_namespace":         prommatch.Equals("linkerd-localhost-test"),
		"dst_pod":               prommatch.Like(nginxPodRE),
		"dst_pod_template_hash": prommatch.Any(),
		"dst_service":           prommatch.Equals("nginx"),
		"dst_serviceaccount":    prommatch.Equals("default"),
	}
	requestsToNGINXMatcher = prommatch.NewMatcher("request_total",
		nginxLabels,
		prommatch.HasPositiveValue(),
	)
	failedResponsesFromNGINXMatcher = prommatch.NewMatcher("response_total",
		nginxLabels,
		prommatch.Labels{
			"classification": prommatch.Equals("failure"),
		},
		prommatch.HasPositiveValue(),
	)
	successResponsesMatcher = prommatch.NewMatcher("response_total",
		prommatch.Labels{
			"direction":      prommatch.Equals("outbound"),
			"classification": prommatch.Equals("success"),
		},
		prommatch.HasPositiveValue(),
	)
	tcpOpenMatcher = prommatch.NewMatcher("tcp_open_connections",
		nginxLabels,
		prommatch.HasValueOf(0),
	)
)

// TestLocalhostServer creates an nginx deployment which listens on localhost
// and a slow-cooker which attempts to send traffic to the nginx.  Since
// slow-cooker should not be able to connect to nginx's localhost address,
// these requests should fail.
func TestLocalhostServer(t *testing.T) {
	ctx := context.Background()
	nginx, err := TestHelper.LinkerdRun("inject", "testdata/nginx.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}
	slowcooker, err := TestHelper.LinkerdRun("inject", "testdata/slow-cooker.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	TestHelper.WithDataPlaneNamespace(ctx, "localhost-test", map[string]string{}, t, func(t *testing.T, ns string) {

		out, err := TestHelper.KubectlApply(nginx, ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}
		out, err = TestHelper.KubectlApply(slowcooker, ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}

		for _, deploy := range []string{"nginx", "slow-cooker"} {
			err = TestHelper.CheckPods(ctx, ns, deploy, 1)
			if err != nil {
				//nolint:errorlint
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		err = TestHelper.RetryFor(50*time.Second, func() error {
			// Use a short time window so that transient errors at startup
			// fall out of the window.
			metrics, err := TestHelper.LinkerdRun("diagnostics", "proxy-metrics", "-n", ns, "deploy/slow-cooker")
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected diagnostics error",
					"unexpected diagnostics error: %s\n%s", err, out)
			}

			m := prommatch.Suite{}.
				MustContain("requests from slowcooker to nginx", requestsToNGINXMatcher).
				MustContain("failed responses returned to slowcooker from nginx", failedResponsesFromNGINXMatcher).
				MustNotContain("success responses returned to slowcooker from nginx", successResponsesMatcher).
				MustContain("zero open tcp connections to nginx", tcpOpenMatcher)

			if err := m.CheckString(metrics); err != nil {
				return fmt.Errorf("metrics check failed: %w", err)
			}

			return nil
		})
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected stat output", "unexpected stat output: %v", err)
		}
	})
}
