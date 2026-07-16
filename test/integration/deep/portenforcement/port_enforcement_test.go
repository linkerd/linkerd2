package portenforcement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

func TestPortEnforcement(t *testing.T) {
	testCases := []struct {
		name           string
		namespace      string
		serviceProfile bool
	}{
		{
			name:      "without ServiceProfile",
			namespace: "port-enforcement-no-service-profile-test",
		},
		{
			name:           "with ServiceProfile",
			namespace:      "port-enforcement-service-profile-test",
			serviceProfile: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			TestHelper.WithDataPlaneNamespace(ctx, tc.namespace, map[string]string{}, t, func(t *testing.T, ns string) {
				if tc.serviceProfile {
					applyServiceProfile(t, ns)
				}

				manifest, err := testutil.ReadFile("testdata/port_enforcement.yaml")
				if err != nil {
					testutil.AnnotatedFatal(t, "failed to read test manifest", err)
				}
				out, err := TestHelper.KubectlApply(manifest, ns)
				if err != nil {
					testutil.AnnotatedFatalf(t, "failed to apply test manifest",
						"failed to apply test manifest: %v\noutput:\n%s", err, out)
				}

				TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
					"metrics-test": {
						Namespace: ns,
						Replicas:  1,
					},
					"curl-test": {
						Namespace: ns,
						Replicas:  1,
					},
				})

				host := fmt.Sprintf("metrics-test.%s.svc.cluster.local", ns)
				assertRequestSucceeds(t, ns, host, 9090, "OK_FROM_9090")
				assertRequestDenied(t, ns, host, 9091)
			})
		})
	}
}

func applyServiceProfile(t *testing.T, ns string) {
	t.Helper()

	host := fmt.Sprintf("metrics-test.%s.svc.cluster.local", ns)
	profile := fmt.Sprintf(`apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: %[1]s
spec:
  dstOverrides:
  - authority: %[1]s
    weight: 1
`, host)
	out, err := TestHelper.KubectlApply(profile, ns)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to apply ServiceProfile",
			"failed to apply ServiceProfile: %v\noutput:\n%s", err, out)
	}
}

func assertRequestSucceeds(t *testing.T, ns, host string, port int, expected string) {
	t.Helper()

	var out string
	err := testutil.RetryFor(30*time.Second, func() error {
		var err error
		out, err = request(ns, host, port)
		if err != nil {
			return fmt.Errorf("request failed: %w; output: %s", err, out)
		}
		var rsp map[string]string
		err = json.Unmarshal([]byte(out), &rsp)
		if err != nil {
			return fmt.Errorf("failed to unmarshal response: %w; output: %s", err, out)
		}
		if rsp["payload"] != expected {
			return fmt.Errorf("expected response %q, got %q", expected, rsp["status"])
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatalf(t, "request to declared Service port failed",
			"request to %s:%d failed: %v", host, port, err)
	}
}

func assertRequestDenied(t *testing.T, ns, host string, port int) {
	t.Helper()

	out, err := request(ns, host, port)
	if err == nil {
		testutil.AnnotatedFatalf(t, "request to undeclared Service port succeeded",
			"request to %s:%d unexpectedly succeeded with response %q", host, port, out)
	}
}

func request(ns, host string, port int) (string, error) {
	return TestHelper.Kubectl("",
		"exec", "-n", ns, "deploy/curl-test", "-c", "curl", "--",
		"curl", "-fsS", "--connect-timeout", "5", "--max-time", "10",
		fmt.Sprintf("http://%s:%d", host, port),
	)
}
