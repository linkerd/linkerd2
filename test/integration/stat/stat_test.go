package get

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// These tests retry for up to 20 seconds, since each call to "linkerd stat"
// generates traffic to the components in the linkerd namespace, and we're
// testing that those components are properly reporting stats. It's ok if the
// first few attempts fail due to missing stats, since the requests from those
// failed attempts will eventually be recorded in the stats that we're
// requesting, and the test will pass.
func TestCliStatForLinkerdNamespace(t *testing.T) {
	ctx := context.Background()
	var prometheusPod, prometheusAuthority, prometheusNamespace, prometheusDeployment, metricsPod string
	// Get Metrics Pod
	pods, err := TestHelper.GetPodNamesForDeployment(ctx, TestHelper.GetVizNamespace(), "metrics-api")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to get pods for metrics-api",
			"failed to get pods for metrics-api: %s", err)
	}
	if len(pods) != 1 {
		testutil.Fatalf(t, "expected 1 pod for metrics-api, got %d", len(pods))
	}
	metricsPod = pods[0]

	// Retrieve Prometheus pod details
	if TestHelper.ExternalPrometheus() {
		prometheusNamespace = "external-prometheus"
		prometheusDeployment = "prometheus"
	} else {
		prometheusNamespace = TestHelper.GetVizNamespace()
		prometheusDeployment = "prometheus"
	}

	pods, err = TestHelper.GetPodNamesForDeployment(ctx, prometheusNamespace, prometheusDeployment)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to get pods for prometheus",
			"failed to get pods for prometheus: %s", err)
	}
	if len(pods) != 1 {
		testutil.Fatalf(t, "expected 1 pod for prometheus, got %d", len(pods))
	}
	prometheusPod = pods[0]
	prometheusAuthority = prometheusDeployment + "." + prometheusNamespace + ".svc.cluster.local:9090"

	testCases := []struct {
		args         []string
		expectedRows map[string]string
		status       string
		isAuthority  bool
	}{
		{
			args: []string{"viz", "stat", "deploy", "-n", TestHelper.GetLinkerdNamespace()},
			expectedRows: map[string]string{
				"linkerd-destination":    "1/1",
				"linkerd-identity":       "1/1",
				"linkerd-proxy-injector": "1/1",
			},
		},
		{
			args: []string{"viz", "stat", "ns", TestHelper.GetLinkerdNamespace()},
			expectedRows: map[string]string{
				TestHelper.GetLinkerdNamespace(): "3/3",
			},
		},
		{
			args: []string{"viz", "stat", fmt.Sprintf("po/%s", prometheusPod), "-n", prometheusNamespace, "--from", fmt.Sprintf("po/%s", metricsPod), "--from-namespace", TestHelper.GetVizNamespace()},
			expectedRows: map[string]string{
				prometheusPod: "1/1",
			},
			status: "Running",
		},
		{
			args: []string{"viz", "stat", "deploy", "-n", TestHelper.GetVizNamespace(), "--to", fmt.Sprintf("po/%s", prometheusPod), "--to-namespace", prometheusNamespace},
			expectedRows: map[string]string{
				"metrics-api": "1/1",
			},
		},
		{
			args: []string{"viz", "stat", "deploy", "-n", TestHelper.GetVizNamespace(), "--to", fmt.Sprintf("svc/%s", prometheusDeployment), "--to-namespace", prometheusNamespace},
			expectedRows: map[string]string{
				"metrics-api": "1/1",
			},
		},
		{
			args: []string{"viz", "stat", "po", "-n", TestHelper.GetVizNamespace(), "--to", fmt.Sprintf("au/%s", prometheusAuthority), "--to-namespace", prometheusNamespace},
			expectedRows: map[string]string{
				metricsPod: "1/1",
			},
			status: "Running",
		},
		{
			args: []string{"viz", "stat", "au", "-n", TestHelper.GetVizNamespace(), "--to", fmt.Sprintf("po/%s", prometheusPod), "--to-namespace", prometheusNamespace},
			expectedRows: map[string]string{
				prometheusAuthority: "-",
			},
			isAuthority: true,
		},
		{
			args: []string{"viz", "stat", "au", "-n", TestHelper.GetVizNamespace(), "--to", fmt.Sprintf("po/%s", prometheusPod), "--to-namespace", prometheusNamespace},
			expectedRows: map[string]string{
				prometheusAuthority: "-",
			},
			isAuthority: true,
		},
	}

	if !TestHelper.ExternalPrometheus() {
		testCases = append(testCases, []struct {
			args         []string
			expectedRows map[string]string
			status       string
			isAuthority  bool
		}{
			{
				args: []string{"viz", "stat", "deploy", "-n", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					"metrics-api":  "1/1",
					"grafana":      "1/1",
					"prometheus":   "1/1",
					"tap":          "1/1",
					"web":          "1/1",
					"tap-injector": "1/1",
				},
			},
			{
				args: []string{"viz", "stat", "ns", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					TestHelper.GetVizNamespace(): "6/6",
				},
			},
			{
				args: []string{"viz", "stat", "svc", "prometheus", "-n", TestHelper.GetVizNamespace(), "--from", "deploy/metrics-api", "--from-namespace", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					"prometheus": "-",
				},
			},
		}...,
		)
	} else {
		testCases = append(testCases, []struct {
			args         []string
			expectedRows map[string]string
			status       string
			isAuthority  bool
		}{
			{
				args: []string{"viz", "stat", "deploy", "-n", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					"metrics-api":  "1/1",
					"grafana":      "1/1",
					"tap":          "1/1",
					"web":          "1/1",
					"tap-injector": "1/1",
				},
			},
			{
				args: []string{"viz", "stat", "ns", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					TestHelper.GetVizNamespace(): "5/5",
				},
			},
		}...,
		)
	}

	// Apply a sample application
	TestHelper.WithDataPlaneNamespace(ctx, "stat-test", map[string]string{}, t, func(t *testing.T, prefixedNs string) {
		out, err := TestHelper.LinkerdRun("inject", "--manual", "../trafficsplit/testdata/application.yaml")
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd inject' command failed", err)
		}

		out, err = TestHelper.KubectlApply(out, prefixedNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		// wait for deployments to start
		for _, deploy := range []string{"backend", "failing", "slow-cooker"} {
			if err := TestHelper.CheckPods(ctx, prefixedNs, deploy, 1); err != nil {
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		testCases = append(testCases, []struct {
			args         []string
			expectedRows map[string]string
			status       string
			isAuthority  bool
		}{
			{
				args: []string{"viz", "stat", "svc", "-n", prefixedNs},
				expectedRows: map[string]string{
					"backend-svc": "-",
				},
			},
			{
				args: []string{"viz", "stat", "svc/backend-svc", "-n", prefixedNs, "--from", "deploy/slow-cooker"},
				expectedRows: map[string]string{
					"backend-svc": "-",
				},
			},
		}...,
		)

		for _, tt := range testCases {
			tt := tt // pin
			timeout := 20 * time.Second
			t.Run("linkerd "+strings.Join(tt.args, " "), func(t *testing.T) {
				err := TestHelper.RetryFor(timeout, func() error {
					// Use a short time window so that transient errors at startup
					// fall out of the window.
					tt.args = append(tt.args, "-t", "30s")
					out, err := TestHelper.LinkerdRun(tt.args...)
					if err != nil {
						testutil.AnnotatedFatalf(t, "unexpected stat error",
							"unexpected stat error: %s\n%s", err, out)
					}

					expectedColumnCount := 8
					if tt.isAuthority {
						expectedColumnCount = 7
					}
					if tt.status != "" {
						expectedColumnCount++
					}
					rowStats, err := testutil.ParseRows(out, len(tt.expectedRows), expectedColumnCount)
					if err != nil {
						return err
					}

					for name, meshed := range tt.expectedRows {
						if err := validateRowStats(name, meshed, tt.status, rowStats, tt.isAuthority); err != nil {
							return err
						}
					}

					return nil
				})
				if err != nil {
					testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out checking stats (%s)", timeout), err)
				}
			})
		}
	})

	// Test authorization stats
	TestHelper.WithDataPlaneNamespace(ctx, "stat-authz-test", map[string]string{}, t, func(t *testing.T, prefixedNs string) {
		out, err := TestHelper.LinkerdRun("inject", "--manual", "../tracing/testdata/emojivoto.yaml")
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd inject' command failed", err)
		}

		out, err = TestHelper.KubectlApply(out, prefixedNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		policyFile := "testdata/emoji-policy.yaml"
		policyResources, err := testutil.ReadFile(policyFile)
		if err != nil {
			testutil.AnnotatedFatalf(t, "cannot read updated traffic split resource",
				"cannot read updated traffic split resource: %s, %s", policyResources, err)
		}

		out, err = TestHelper.KubectlApply(policyResources, prefixedNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to update traffic split resource",
				"failed to update traffic split resource: %s\n %s", err, out)
		}

		// wait for deployments to start
		for _, deploy := range []string{"web", "emoji", "vote-bot", "voting"} {
			if err := TestHelper.CheckPods(ctx, prefixedNs, deploy, 1); err != nil {
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		testCases := []struct {
			args         []string
			expectedRows []string
			isServer     bool
		}{
			{
				args: []string{"viz", "stat", "srv", "-n", prefixedNs},
				expectedRows: []string{
					"emoji-grpc",
					"prom",
					"voting-grpc",
					"web-http",
				},
				isServer: true,
			},
			{
				args: []string{"viz", "stat", "srv/emoji-grpc", "-n", prefixedNs},
				expectedRows: []string{
					"emoji-grpc",
				},
				isServer: true,
			},
			{
				args: []string{"viz", "stat", "saz", "-n", prefixedNs},
				expectedRows: []string{
					"emoji-grpc",
					"prom-prometheus",
					"voting-grpc",
					"web-public",
				},
				isServer: false,
			},
			{
				args: []string{"viz", "stat", "saz/emoji-grpc", "-n", prefixedNs},
				expectedRows: []string{
					"emoji-grpc",
				},
				isServer: false,
			},
		}

		for _, tt := range testCases {
			tt := tt // pin
			timeout := 20 * time.Second
			t.Run("linkerd "+strings.Join(tt.args, " "), func(t *testing.T) {
				err := TestHelper.RetryFor(timeout, func() error {
					// Use a short time window so that transient errors at startup
					// fall out of the window.
					tt.args = append(tt.args, "-t", "30s")
					out, err := TestHelper.LinkerdRun(tt.args...)
					if err != nil {
						testutil.AnnotatedFatalf(t, "unexpected stat error",
							"unexpected stat error: %s\n%s", err, out)
					}

					var expectedColumnCount int
					if tt.isServer {
						expectedColumnCount = 8
					} else {
						expectedColumnCount = 6
					}

					rowStats, err := ParseAuthzRows(out, len(tt.expectedRows), expectedColumnCount, tt.isServer)
					if err != nil {
						return err
					}

					for _, name := range tt.expectedRows {
						if err := validateAuthzRows(name, rowStats, tt.isServer); err != nil {
							return err
						}
					}

					return nil
				})
				if err != nil {
					testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out checking stats (%s)", timeout), err)
				}
			})
		}
	})
}

func validateAuthzRows(name string, rowStats map[string]*testutil.RowStat, isServer bool) error {
	stat, ok := rowStats[name]
	if !ok {
		return fmt.Errorf("No stats found for [%s]", name)
	}

	expectedSuccessRate := "100.00%"
	if stat.Success != expectedSuccessRate {
		return fmt.Errorf("Expected success rate [%s] for [%s], got [%s]",
			expectedSuccessRate, name, stat.Success)
	}

	if !strings.HasSuffix(stat.UnauthorizedRPS, "rps") {
		return fmt.Errorf("Unexpected rps for [%s], got [%s]",
			name, stat.Rps)
	}

	if !strings.HasSuffix(stat.Rps, "rps") {
		return fmt.Errorf("Unexpected rps for [%s], got [%s]",
			name, stat.Rps)
	}

	if !strings.HasSuffix(stat.P50Latency, "ms") {
		return fmt.Errorf("Unexpected p50 latency for [%s], got [%s]",
			name, stat.P50Latency)
	}

	if !strings.HasSuffix(stat.P95Latency, "ms") {
		return fmt.Errorf("Unexpected p95 latency for [%s], got [%s]",
			name, stat.P95Latency)
	}

	if !strings.HasSuffix(stat.P99Latency, "ms") {
		return fmt.Errorf("Unexpected p99 latency for [%s], got [%s]",
			name, stat.P99Latency)
	}

	if isServer {
		_, err := strconv.Atoi(stat.TCPOpenConnections)
		if err != nil {
			return fmt.Errorf("Error parsing number of TCP connections [%s]: %s", stat.TCPOpenConnections, err.Error())
		}
	}

	return nil
}

// ParseRows parses the output of linkerd stat on a policy resource
func ParseAuthzRows(out string, expectedRowCount, expectedColumnCount int, isServer bool) (map[string]*testutil.RowStat, error) {
	rows, err := testutil.CheckRowCount(out, expectedRowCount)
	if err != nil {
		return nil, err
	}

	rowStats := make(map[string]*testutil.RowStat)
	for _, row := range rows {
		fields := strings.Fields(row)

		if len(fields) != expectedColumnCount {
			return nil, fmt.Errorf(
				"Expected [%d] columns in stat output, got [%d]; full output:\n%s",
				expectedColumnCount, len(fields), row)
		}

		i := 0
		rowStats[fields[0]] = &testutil.RowStat{
			Name: fields[0],
		}

		if isServer {
			rowStats[fields[0]].UnauthorizedRPS = fields[1+i]
			rowStats[fields[0]].Success = fields[2+i]
			rowStats[fields[0]].Rps = fields[3+i]
			rowStats[fields[0]].P50Latency = fields[4+i]
			rowStats[fields[0]].P95Latency = fields[5+i]
			rowStats[fields[0]].P99Latency = fields[6+i]
			rowStats[fields[0]].TCPOpenConnections = fields[7+i]
		} else {
			rowStats[fields[0]].Success = fields[1+i]
			rowStats[fields[0]].Rps = fields[2+i]
			rowStats[fields[0]].P50Latency = fields[3+i]
			rowStats[fields[0]].P95Latency = fields[4+i]
			rowStats[fields[0]].P99Latency = fields[5+i]
		}

	}

	return rowStats, nil
}

func validateRowStats(name, expectedMeshCount, expectedStatus string, rowStats map[string]*testutil.RowStat, isAuthority bool) error {
	stat, ok := rowStats[name]
	if !ok {
		return fmt.Errorf("No stats found for [%s]", name)
	}

	if stat.Status != expectedStatus {
		return fmt.Errorf("Expected status '%s' for '%s', got '%s'",
			expectedStatus, name, stat.Status)
	}

	if stat.Meshed != expectedMeshCount {
		return fmt.Errorf("Expected mesh count [%s] for [%s], got [%s]",
			expectedMeshCount, name, stat.Meshed)
	}

	expectedSuccessRate := "100.00%"
	if stat.Success != expectedSuccessRate {
		return fmt.Errorf("Expected success rate [%s] for [%s], got [%s]",
			expectedSuccessRate, name, stat.Success)
	}

	if !strings.HasSuffix(stat.Rps, "rps") {
		return fmt.Errorf("Unexpected rps for [%s], got [%s]",
			name, stat.Rps)
	}

	if !strings.HasSuffix(stat.P50Latency, "ms") {
		return fmt.Errorf("Unexpected p50 latency for [%s], got [%s]",
			name, stat.P50Latency)
	}

	if !strings.HasSuffix(stat.P95Latency, "ms") {
		return fmt.Errorf("Unexpected p95 latency for [%s], got [%s]",
			name, stat.P95Latency)
	}

	if !strings.HasSuffix(stat.P99Latency, "ms") {
		return fmt.Errorf("Unexpected p99 latency for [%s], got [%s]",
			name, stat.P99Latency)
	}

	if stat.TCPOpenConnections != "-" && !isAuthority {
		_, err := strconv.Atoi(stat.TCPOpenConnections)
		if err != nil {
			return fmt.Errorf("Error parsing number of TCP connections [%s]: %s", stat.TCPOpenConnections, err.Error())
		}
	}

	return nil
}
