package smimetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
	"github.com/servicemeshinterface/smi-sdk-go/pkg/apis/metrics/v1alpha1"
)

var TestHelper *testutil.TestHelper

type testCase struct {
	name string
	kind string
	// edges > 0 denotes that its a edges query, otherwise a resource query
	edges int
}

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

func TestSMIMetrics(t *testing.T) {

	t.Skip("Skipped, as SMI-Metrics currently hardcodes the prometheusUrl of Linkerd which changed")

	ctx := context.Background()

	if os.Getenv("RUN_ARM_TEST") != "" {
		t.Skip("Skipped. SMI-metrics image does not support ARM yet")
	}

	// Install smi-metrics using Helm chart
	testNamespace := TestHelper.GetTestNamespace("smi-metrics-test")
	err := TestHelper.CreateDataPlaneNamespaceIfNotExists(ctx, testNamespace, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create %s namespace: %s", testNamespace, err)
	}

	// Currently, SMI-Metrics Helm chart is saved locally and can be updated to a newer version
	// by downloading from https://github.com/servicemeshinterface/smi-metrics/releases and
	// moving the package here, along with a version bump below
	args := []string{
		"install",
		"smi-metrics",
		"smi-metrics-0.2.1.tgz",
		"--set",
		"adapter=linkerd",
		"--namespace",
		testNamespace,
	}

	if stdout, stderr, err := TestHelper.HelmRun(args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm install' command failed\n%s\n%s\n%v", stdout, stderr, err)
	}

	if err := TestHelper.CheckPods(ctx, testNamespace, "smi-metrics", 1); err != nil {
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}

	testCases := []testCase{
		{
			name: "linkerd-destination",
			kind: "deployments",
		},
		{
			name: "linkerd-prometheus",
			kind: "deployments",
		},
		{
			name:  "linkerd-destination",
			kind:  "deployments",
			edges: 2,
		},
		{
			name:  "linkerd-identity",
			kind:  "deployments",
			edges: 1,
		},
	}

	timeout := 50 * time.Second
	// check resource queries
	for _, tc := range testCases {
		tc := tc // pin
		err = TestHelper.RetryFor(timeout, func() error {

			queryURL := fmt.Sprintf("/apis/metrics.smi-spec.io/v1alpha1/namespaces/%s/%s/%s", TestHelper.GetLinkerdNamespace(), tc.kind, tc.name)
			if tc.edges > 0 {
				queryURL += "/edges"
			}

			queryArgs := []string{
				"get",
				"--raw",
				queryURL,
			}

			out, err := TestHelper.Kubectl("", queryArgs...)
			if err != nil {
				return fmt.Errorf("failed to query smi-metrics URL %s: %s\n%s", queryURL, err, out)
			}

			if tc.edges > 0 {
				// edges query
				var metrics v1alpha1.TrafficMetricsList
				err = json.Unmarshal([]byte(out), &metrics)
				if err != nil {
					return fmt.Errorf("failed to unmarshal output for query %s into TrafficMetricsList type: %s", queryURL, err)
				}

				if err = checkTrafficMetricsList(metrics, tc.name, tc.edges); err != nil {
					return err
				}

			} else {
				// resource query
				var metrics v1alpha1.TrafficMetrics
				err = json.Unmarshal([]byte(out), &metrics)
				if err != nil {
					return fmt.Errorf("failed to unmarshal output for query %s into TrafficMetrics type: %s", queryURL, err)
				}

				if err = checkTrafficMetrics(metrics, tc.name); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			testutil.AnnotatedError(t, fmt.Sprintf("timed-out checking smi-metrics output (%s)", timeout), err)
		}
	}
}

func checkTrafficMetrics(metrics v1alpha1.TrafficMetrics, name string) error {
	if metrics.Name == name {
		return nil
	}
	return fmt.Errorf("excpected %s, but got %s", name, metrics.Name)

}

func checkTrafficMetricsList(metrics v1alpha1.TrafficMetricsList, name string, numberOfEdges int) error {
	if metrics.Resource.Name == name && len(metrics.Items) == numberOfEdges {
		return nil
	}
	return fmt.Errorf("excpected %s with %d edges, but got %s with %d edges", name, numberOfEdges, metrics.Resource.Name, len(metrics.Items))
}
