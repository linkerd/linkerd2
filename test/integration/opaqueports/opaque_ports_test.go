package opaqueports

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
	v1 "k8s.io/api/core/v1"
)

var TestHelper *testutil.TestHelper

var (
	appName = "app"

	// With the app's port marked as opaque, we expect to find a single open
	// TCP connection that is not TLS'd because the port is skipped.
	tcpMetric = "tcp_open_total{peer=\"src\",direction=\"inbound\",tls=\"true\",client_id=\"default.default.serviceaccount.identity.linkerd.cluster.local\"}"
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestOpaquePorts(t *testing.T) {
	ctx := context.Background()

	opaquePortsNs := TestHelper.GetTestNamespace("opaque-ports-test")
	err := TestHelper.CreateDataPlaneNamespaceIfNotExists(ctx, opaquePortsNs, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", opaquePortsNs),
			"failed to create %s namespace: %s", opaquePortsNs, err)
	}

	out, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/opaque_ports_application.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd inject' command failed", err)
	}
	out, err = TestHelper.KubectlApply(out, opaquePortsNs)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// Check that the test application started correctly
	if err := TestHelper.CheckPods(ctx, opaquePortsNs, appName, 1); err != nil {
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}

	if err := TestHelper.CheckDeployment(ctx, opaquePortsNs, appName, 1); err != nil {
		testutil.AnnotatedErrorf(t, "CheckDeployment timed-out", "Error validating deployment [%s]:\n%s", appName, err)
	}

	t.Run("expect inbound TCP connection metric with expected TLS identity", func(t *testing.T) {
		pods, err := TestHelper.GetPods(ctx, opaquePortsNs, map[string]string{"app": appName})
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting opaque ports app pods", "error getting opaque ports app pods\n%s", err)
		}

		// Wait for slow-cooker to start sending requests
		time.Sleep(20 * time.Second)

		// Get metrics for the app pod expecting to find TCP connection counters
		metrics, err := getPodMetrics(pods[0], opaquePortsNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting metrics for pod", "error getting metrics for pod\n%s", err)
		}
		if !strings.Contains(metrics, tcpMetric) {
			testutil.AnnotatedFatalf(t, "failed to find expected TCP metric when port is marked as opaque", "failed to find expected TCP metric when port is marked as opaque\n%s", metrics)
		}
	})
}

func getPodMetrics(pod v1.Pod, ns string) (string, error) {
	podName := fmt.Sprintf("pod/%s", pod.Name)
	cmd := []string{"metrics", "--namespace", ns, podName}
	metrics, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		return "", err
	}
	return metrics, nil
}
