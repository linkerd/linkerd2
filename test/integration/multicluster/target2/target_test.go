package target2

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

var (
	tcpConnRE = regexp.MustCompile(
		`tcp_open_total\{direction="outbound",peer="dst",target_addr="[0-9\.]+:[0-9]+",target_ip="[0-9\.]+",target_port="[0-9]+",tls="true",server_id="default\.multicluster-statefulset\.serviceaccount\.identity\.linkerd\.cluster\.local",dst_control_plane_ns="linkerd",dst_namespace="multicluster-statefulset",dst_pod="nginx-statefulset-0",dst_serviceaccount="default",dst_statefulset="nginx-statefulset"\} [1-9]\d*`,
	)
	httpReqRE = regexp.MustCompile(
		`request_total\{direction="outbound",target_addr="[0-9\.]+:8080",target_ip="[0-9\.]+",target_port="[0-9\.]+",tls="true",server_id="default\.multicluster-statefulset\.serviceaccount\.identity\.linkerd\.cluster\.local",dst_control_plane_ns="linkerd",dst_namespace="multicluster-statefulset",dst_pod="nginx-statefulset-0",dst_serviceaccount="default",dst_statefulset="nginx-statefulset"\} [1-9]\d*`,
	)
	dgCmd = []string{"diagnostics", "proxy-metrics", "--namespace",
		"linkerd-multicluster", "deploy/linkerd-gateway"}
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

// TestTargetTraffic inspects the target cluster's web-svc pod to see if the
// source cluster's vote-bot has been able to hit it with requests. If it has
// successfully issued requests, then we'll see log messages indicating that the
// web-svc can't reach the voting-svc (because it's not running).
//
// TODO it may be clearer to invoke `linkerd diagnostics proxy-metrics` to check whether we see
// connections from the gateway pod to the web-svc?
func TestTargetTraffic(t *testing.T) {
	timeout := time.Minute
	err := TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.Kubectl("",
			"--namespace", "emojivoto",
			"logs",
			"--selector", "app=web-svc",
			"--container", "web-svc",
		)
		if err != nil {
			return fmt.Errorf("%w\n%s", err, out)
		}
		// Check for expected error messages
		for _, row := range strings.Split(out, "\n") {
			if strings.Contains(row, " /api/vote?choice=:doughnut: ") {
				return nil
			}
		}
		return fmt.Errorf("web-svc logs in target cluster do not include voting errors\n%s", out)
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster gateways' command timed-out (%s)", timeout), err)
	}
}

// TestMulticlusterStatefulSetTargetTraffic will test that a statefulset can be
// mirrored from a target cluster to a source cluster. The test deploys two
// workloads: a slow cooker (as a client) in the src, and an nginx statefulset in
// (as a server) in the tgt. The slow-cooker is configured to send traffic to an
// nginx endpoint mirror (nginx-statefulset-0). The traffic should be received
// by the nginx pod in the tgt. To assert this, we get proxy metrics from the
// gateway to make sure our connections from the source cluster were routed
// correctly.
func TestMulticlusterStatefulSetTargetTraffic(t *testing.T) {
	t.Run("expect open outbound TCP connection from gateway to nginx", func(t *testing.T) {
		// Use a short time window so that slow-cooker can warm-up and send
		// requests.
		err := TestHelper.RetryFor(30*time.Second, func() error {
			// Check gateway metrics
			metrics, err := TestHelper.LinkerdRun(dgCmd...)
			if err != nil {
				return fmt.Errorf("failed to get metrics for gateway deployment: %w", err)
			}

			// If no match, it means there are no open tcp conns from gateway to
			// nginx pod.
			if !tcpConnRE.MatchString(metrics) {
				return fmt.Errorf("failed to find expected TCP connection open outbound metric from gateway to nginx")
			}

			return nil
		})

		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v", err)
		}

	})

	t.Run("expect non-empty HTTP request metric from gateway to nginx", func(t *testing.T) {
		// Use a short time window so that slow-cooker can warm-up and send
		// requests.
		err := TestHelper.RetryFor(30*time.Second, func() error {
			// Check gateway metrics
			metrics, err := TestHelper.LinkerdRun(dgCmd...)
			if err != nil {
				return fmt.Errorf("failed to get metrics for gateway deployment: %w", err)
			}

			// If no match, it means there are no outbound HTTP requests from
			// gateway to nginx pod.
			if !httpReqRE.MatchString(metrics) {
				return fmt.Errorf("failed to find expected outbound HTTP request metric from gateway to nginx\nexpected: %s, got: %s", httpReqRE, metrics)
			}
			return nil
		})

		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v", err)
		}
	})
}
