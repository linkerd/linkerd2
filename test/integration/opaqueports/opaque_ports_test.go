package opaqueports

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
	v1 "k8s.io/api/core/v1"
)

var TestHelper *testutil.TestHelper

var (
	opaquePodApp = "opaque-pod"
	opaquePodSC  = "slow-cooker-opaque-pod"
	opaqueSvcApp = "opaque-service"
	opaqueSvcSC  = "slow-cooker-opaque-service"
	tcpMetricRE  = regexp.MustCompile(
		`tcp_open_total\{direction="inbound",peer="src",target_addr="[0-9\.]+:[0-9]+",tls="true",client_id="default\.linkerd-opaque-ports-test\.serviceaccount\.identity\.linkerd\.cluster\.local"\} [0-9]+`,
	)
	httpRequestTotalMetricRE = regexp.MustCompile(
		`request_total\{direction="outbound",authority="[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:8080",target_addr="[0-9\.]+:8080",tls="true",.*`,
	)
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

	// Check that the opaque pod test application started correctly.
	if err := TestHelper.CheckPods(ctx, opaquePortsNs, opaquePodApp, 1); err != nil {
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}

	// Check that the opaque service test application started correctly.
	if err := TestHelper.CheckPods(ctx, opaquePortsNs, opaqueSvcApp, 1); err != nil {
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}

	// Wait for slow-cookers to start sending requests
	time.Sleep(20 * time.Second)

	t.Run("expect absent HTTP outbound requests for opaque-pod slow clooker", func(t *testing.T) {
		// Check the slow cooker metrics
		pods, err := TestHelper.GetPods(ctx, opaquePortsNs, map[string]string{"app": opaquePodSC})
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting pods", "error getting pods\n%s", err)
		}
		metrics, err := getPodMetrics(pods[0], opaquePortsNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting metrics for pod", "error getting metrics for pod\n%s", err)
		}
		if httpRequestTotalMetricRE.MatchString(metrics) {
			testutil.AnnotatedFatalf(t, "expected not to find HTTP outbound requests when pod is opaque", "expected not to find HTTP outbound requests when pod is opaque\n%s", metrics)
		}
		// Check the application metrics
		pods, err = TestHelper.GetPods(ctx, opaquePortsNs, map[string]string{"app": opaquePodApp})
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting pods", "error getting pods\n%s", err)
		}
		metrics, err = getPodMetrics(pods[0], opaquePortsNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting metrics for pod", "error getting metrics for pod\n%s", err)
		}
		if !tcpMetricRE.MatchString(metrics) {
			testutil.AnnotatedFatalf(t, "failed to find expected TCP metric when pod is opaque", "failed to find expected TCP metric when pod is opaque\n%s", metrics)
		}
	})

	t.Run("expect inbound TCP connection metric with expected TLS identity for opaque service app", func(t *testing.T) {
		// Check the slow cooker metrics
		pods, err := TestHelper.GetPods(ctx, opaquePortsNs, map[string]string{"app": opaqueSvcSC})
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting pods", "error getting pods\n%s", err)
		}
		metrics, err := getPodMetrics(pods[0], opaquePortsNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting metrics for pod", "error getting metrics for pod\n%s", err)
		}
		if httpRequestTotalMetricRE.MatchString(metrics) {
			testutil.AnnotatedFatalf(t, "expected not to find HTTP outbound requests when service is opaque", "expected not to find HTTP outbound requests when service is opaque\n%s", metrics)
		}
		// Check the application metrics
		pods, err = TestHelper.GetPods(ctx, opaquePortsNs, map[string]string{"app": opaqueSvcApp})
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting pods", "error getting pods\n%s", err)
		}
		metrics, err = getPodMetrics(pods[0], opaquePortsNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "error getting metrics for pod", "error getting metrics for pod\n%s", err)
		}
		if !tcpMetricRE.MatchString(metrics) {
			testutil.AnnotatedFatalf(t, "failed to find expected TCP metric when pod is opaque", "failed to find expected TCP metric when pod is opaque\n%s", metrics)
		}
	})
}

func getPodMetrics(pod v1.Pod, ns string) (string, error) {
	podName := fmt.Sprintf("pod/%s", pod.Name)
	cmd := []string{"diagnostics", "proxy-metrics", "--namespace", ns, podName}
	metrics, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		return "", err
	}
	return metrics, nil
}
