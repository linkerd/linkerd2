package localhost

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

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	if err := TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge); err != nil {
		panic(fmt.Sprintf("error running test: %v", err))
	}
	os.Exit(m.Run())
}

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

			rpsRE := regexp.MustCompile(
				`request_total\{direction="outbound",authority="nginx\.linkerd-localhost-test\.svc\.cluster\.local:8080",.*,dst_deployment="nginx",dst_namespace="linkerd-localhost-test",dst_pod="nginx.*",dst_pod_template_hash=".*",dst_service="nginx",dst_serviceaccount="default"\} [1-9]\d*`,
			)
			if !rpsRE.MatchString(metrics) {
				return fmt.Errorf("expected non-zero RPS from slowcooker to nginx\nexpected: %s, got: %s", rpsRE, metrics)
			}

			// Requests sent to a port which is only bound to localhost should
			// fail.
			successRE := regexp.MustCompile(
				`response_total\{direction="outbound",authority="nginx\.linkerd-localhost-test\.svc\.cluster\.local:8080",.*,classification="success"\} [1-9]\d*`,
			)
			if successRE.MatchString(metrics) {
				return fmt.Errorf("expected zero success-rate from slowcooker to nginx\nexpected: %s, got: %s", successRE, metrics)
			}

			tcpConnRE := regexp.MustCompile(
				`tcp_open_connections\{direction="outbound",peer="dst",authority="nginx\.linkerd-localhost-test\.svc\.cluster\.local:8080",.*\} 0`,
			)
			if !tcpConnRE.MatchString(metrics) {
				return fmt.Errorf("expected no tcp connections from slowcooker to nginx\nexpected: %s, got: %s", tcpConnRE, metrics)
			}

			return nil
		})
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected stat output", "unexpected stat output: %v", err)
		}
	})
}
