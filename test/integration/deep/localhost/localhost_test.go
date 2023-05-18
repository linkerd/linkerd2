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
		"authority":             prommatch.Equals("nginx.linkerd-localhost-server-test.svc.cluster.local:8080"),
		"dst_deployment":        prommatch.Equals("nginx"),
		"dst_namespace":         prommatch.Equals("linkerd-localhost-server-test"),
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

	TestHelper.WithDataPlaneNamespace(ctx, "localhost-server-test", map[string]string{}, t, func(t *testing.T, ns string) {

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

		err = testutil.RetryFor(50*time.Second, func() error {
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

// TestLocalhostRouting creates a pod with two containers: nginx and curl, and
// tests traffic can be successfully routed when packets stay local. It will
// test that the pod can send a request to itself successfully via its pod IP
// (concrete address). And it will also test that a pod can send a request to
// itself via its service IP (logical address).
func TestLocalhostRouting(t *testing.T) {
	ctx := context.Background()
	nginx, err := TestHelper.LinkerdRun("inject", "testdata/nginx-and-curl.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	TestHelper.WithDataPlaneNamespace(ctx, "localhost-routing-test", map[string]string{}, t, func(t *testing.T, ns string) {
		out, err := TestHelper.KubectlApply(nginx, ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}

		err = TestHelper.CheckPods(ctx, ns, "nginx", 1)
		if err != nil {
			//nolint:errorlint
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}

		pods, err := TestHelper.GetPodsForDeployment(ctx, ns, "nginx")
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v", err)
		}

		podName := pods[0].ObjectMeta.Name
		execCommand := []string{"exec", "-n", ns, podName, "-c", "curl", "--", "curl", "-w", "%{http_code}", "-so", "/dev/null"}
		t.Run("Route to Concrete Address Over Loopback", func(t *testing.T) {
			podIP := pods[0].Status.PodIP
			if podIP == "" {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: no IP address found for %s/%s", ns, podName)
			}

			statusCode, err := TestHelper.Kubectl("", append(execCommand, podIP)...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error received when calling 'kubectl exec'", "unexpected error received when calling 'kubectl exec': %v", err)
			}

			if statusCode != "200" {
				testutil.AnnotatedFatalf(t, "unexpected http status code received", "unexpected http status code received: expected: '200', got: '%s'", statusCode)
			}
		})

		t.Run("Route to Logical Address Over Loopback", func(t *testing.T) {
			statusCode, err := TestHelper.Kubectl("", append(execCommand, "http://nginx-svc:80")...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error received when calling 'kubectl exec'", "unexpected error received when calling 'kubectl exec': %v", err)
			}

			if statusCode != "200" {
				testutil.AnnotatedFatalf(t, "unexpected http status code received", "unexpected http status code received: expected: '200', got: '%s'", statusCode)
			}
		})
	})
}
