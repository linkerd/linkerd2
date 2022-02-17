package target_statefulset_test

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

/////////////////////
//  TEST EXECUTION //
/////////////////////

// TestMulticlusterStatefulSetTargetTraffic will test that a statefulset can be
// mirrored from a target cluster to a source cluster. The test deploys two
// workloads: a slow cooker (as a client) in the src, and an nginx statefulset in
// (as a server) in the tgt. The slow-cooker is configured to send traffic to an
// nginx endpoint mirror (nginx-statefulset-0). The traffic should be received
// by the nginx pod in the tgt. To assert this, we get proxy metrics from the
// gateway to make sure our connections from the source cluster were routed
// correctly.
func TestMulticlusterStatefulSetTargetTraffic(t *testing.T) {
	err := TestHelper.CreateDataPlaneNamespaceIfNotExists(context.Background(),
		"multicluster-statefulset", nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create multicluster-statefulset namespace", "failed to create multicluster-statefulset namespace: %s", err)
	}

	nginx, err := TestHelper.LinkerdRun("inject", "testdata/nginx.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to inject nginx manifest", "failed to inject nginx manifest: %s", err)
	}

	out, err := TestHelper.KubectlApply(nginx, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to install nginx", "failed to install nginx: %s\ngot: %s", err, out)
	}

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
				return fmt.Errorf("failed to find expected TCP connection open outbound metric from gateway to nginx\nexpected: %s, got: %s", tcpConnRE, metrics)
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
