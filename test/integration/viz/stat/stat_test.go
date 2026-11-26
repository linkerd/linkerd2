package get

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
	"github.com/linkerd/linkerd2/testutil/prommatch"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until viz extension is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdVizDeployReplicas)
	os.Exit(m.Run())
}

// statWindow is the time window passed to every "linkerd viz stat" call. It is
// short so that transient errors at startup fall out of the window.
const statWindow = "30s"

// clientTrafficTimeout is a maximum wait for the client application to start
// sending traffic.
const clientTrafficTimeout = 2 * time.Minute

type statTestCase struct {
	args         []string
	expectedRows map[string]string
	status       string
}

// clientSuccessMatcher matches successful responses recorded by slow-cooker's
// proxy for the requests slow-cooker sends to backend-svc.
var clientSuccessMatcher = prommatch.NewMatcher("response_total",
	prommatch.Labels{
		"direction":      prommatch.Equals("outbound"),
		"classification": prommatch.Equals("success"),
	},
	prommatch.HasPositiveValue(),
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestCliStatForLinkerdNamespace(t *testing.T) {
	ctx := context.Background()
	var prometheusPod, prometheusNamespace, prometheusDeployment, metricsPod string
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
	prometheusNamespace = TestHelper.GetVizNamespace()
	prometheusDeployment = "prometheus"

	pods, err = TestHelper.GetPodNamesForDeployment(ctx, prometheusNamespace, prometheusDeployment)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to get pods for prometheus",
			"failed to get pods for prometheus: %s", err)
	}
	if len(pods) != 1 {
		testutil.Fatalf(t, "expected 1 pod for prometheus, got %d", len(pods))
	}
	prometheusPod = pods[0]

	testCases := []statTestCase{
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
	}

	if !TestHelper.ExternalPrometheus() {
		testCases = append(testCases, []statTestCase{
			{
				args: []string{"viz", "stat", "deploy", "-n", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					"metrics-api":  "1/1",
					"prometheus":   "1/1",
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
			{
				args: []string{"viz", "stat", "svc", "prometheus", "-n", TestHelper.GetVizNamespace(), "--from", "deploy/metrics-api", "--from-namespace", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					"prometheus": "-",
				},
			},
		}...,
		)
	} else {
		testCases = append(testCases, []statTestCase{
			{
				args: []string{"viz", "stat", "deploy", "-n", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					"metrics-api":  "1/1",
					"tap":          "1/1",
					"web":          "1/1",
					"tap-injector": "1/1",
				},
			},
			{
				args: []string{"viz", "stat", "ns", TestHelper.GetVizNamespace()},
				expectedRows: map[string]string{
					TestHelper.GetVizNamespace(): "4/4",
				},
			},
		}...,
		)
	}

	// preceding test cases do not require sample apps they only check for
	// formatted viz stat output that result from the install test
	runStatCases(t, testCases, 20*time.Second)

	// Apply a sample application
	TestHelper.WithDataPlaneNamespace(ctx, "stat-test", map[string]string{}, t, func(t *testing.T, prefixedNs string) {
		// separate the sample apps into server + client applications and only
		// start the client after the server is deployed
		if err := deploySampleApp(ctx, t, prefixedNs, "testdata/servers.yaml", "backend", "failing"); err != nil {
			testutil.AnnotatedError(t, "failed to deploy the sample application's servers", err)
			return
		}
		if err := deploySampleApp(ctx, t, prefixedNs, "testdata/client.yaml", "slow-cooker"); err != nil {
			testutil.AnnotatedError(t, "failed to deploy the sample application's client", err)
			return
		}

		// wait for client traffic (using linkerd diagnostics to inspect proxy metrics)
		if err := waitForClientTraffic(prefixedNs); err != nil {
			testutil.AnnotatedError(t, "slow-cooker did not receive successful responses", err)
			return
		}
		// wait for metrics to arrive in prometheus
		if err := waitForPrometheusSamples(ctx, prefixedNs, prometheusNamespace, prometheusDeployment); err != nil {
			testutil.AnnotatedError(t, "Prometheus did not ingest slow-cooker's metrics", err)
			return
		}

		// the data is already in Prometheus, so each case runs once
		//
		// a failure here means the stat output is wrong, not that the data
		// arrived late.
		runStatCases(t, []statTestCase{
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
		}, 0)
	})
}

// runStatCases runs each case as a subtest. With a non-zero retryTimeout, a
// case is retried every second until it passes or the timeout expires; with
// zero, it runs once.
func runStatCases(t *testing.T, testCases []statTestCase, retryTimeout time.Duration) {
	t.Helper()
	for _, tt := range testCases {
		args := append(append([]string{}, tt.args...), "-t", statWindow)
		t.Run("linkerd "+strings.Join(tt.args, " "), func(t *testing.T) {
			check := func() error {
				out, err := TestHelper.LinkerdRun(args...)
				if err != nil {
					testutil.AnnotatedFatalf(t, "unexpected stat error",
						"unexpected stat error: %s\n%s", err, out)
				}

				expectedColumnCount := 8
				if tt.status != "" {
					expectedColumnCount++
				}
				rowStats, err := testutil.ParseRows(out, len(tt.expectedRows), expectedColumnCount)
				if err != nil {
					return err
				}

				for name, meshed := range tt.expectedRows {
					if err := validateRowStats(name, meshed, tt.status, rowStats); err != nil {
						return err
					}
				}

				return nil
			}

			if retryTimeout == 0 {
				if err := check(); err != nil {
					testutil.AnnotatedFatal(t, "unexpected stats", err)
				}
				return
			}
			if err := testutil.RetryFor(retryTimeout, check); err != nil {
				testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out checking stats (%s)", retryTimeout), err)
			}
		})
	}
}

// deploySampleApp injects the proxy into the manifest at path, applies it to
// ns, and waits until the pods of the given deployments are running and ready.
func deploySampleApp(ctx context.Context, t *testing.T, ns, path string, deploys ...string) error {
	t.Helper()
	out, err := TestHelper.LinkerdRun("inject", "--manual", path)
	if err != nil {
		return fmt.Errorf("'linkerd inject' command failed: %w", err)
	}

	out, err = TestHelper.KubectlApply(out, ns)
	if err != nil {
		return fmt.Errorf("'kubectl apply' command failed: %w\n%s", err, out)
	}

	for _, deploy := range deploys {
		if err := TestHelper.CheckPods(ctx, ns, deploy, 1); err != nil {
			//nolint:errorlint
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				continue
			}
			return err
		}
	}
	return nil
}

// waitForClientTraffic waits until slow-cooker's proxy has recorded successful
// responses from the backend. It reads the proxy's own metrics endpoint, so it
// does not depend on Prometheus.
func waitForClientTraffic(ns string) error {
	return testutil.RetryFor(clientTrafficTimeout, func() error {
		metrics, err := TestHelper.LinkerdRun("diagnostics", "proxy-metrics", "-n", ns, "deploy/slow-cooker")
		if err != nil {
			return err
		}
		found, err := clientSuccessMatcher.HasMatchInString(metrics)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("slow-cooker's proxy has not recorded any successful outbound responses")
		}
		return nil
	})
}

// waitForPrometheusSamples waits until Prometheus holds at least two samples of
// slow-cooker's outbound response counts within the stat window. "linkerd viz
// stat" computes rates with increase(), which needs two samples of a series.
func waitForPrometheusSamples(ctx context.Context, ns, promNamespace, promDeploy string) error {
	query := fmt.Sprintf(
		`count_over_time(response_total{namespace=%q, deployment="slow-cooker", direction="outbound"}[%s]) >= 2`,
		ns, statWindow,
	)
	return TestHelper.WaitForPrometheusQuery(ctx, promNamespace, promDeploy, query, clientTrafficTimeout)
}

func validateRowStats(name, expectedMeshCount, expectedStatus string, rowStats map[string]*testutil.RowStat) error {
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

	return nil
}
