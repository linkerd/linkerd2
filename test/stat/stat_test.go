package get

import (
	"fmt"
	"os"
	"strings"
	"testing"

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

type rowStat struct {
	name       string
	meshed     string
	success    string
	rps        string
	p50Latency string
	p95Latency string
	p99Latency string
	tlsPercent string
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

	pods, err := TestHelper.GetPodsForDeployment(TestHelper.GetLinkerdNamespace(), "prometheus")
	if err != nil {
		t.Fatalf("Failed to get pods for prometheus: %s", err)
	}
	if len(pods) != 1 {
		t.Fatalf("Expected 1 pod for prometheus, got %d", len(pods))
	}
	prometheusPod := pods[0]

	pods, err = TestHelper.GetPodsForDeployment(TestHelper.GetLinkerdNamespace(), "controller")
	if err != nil {
		t.Fatalf("Failed to get pods for controller: %s", err)
	}
	if len(pods) != 1 {
		t.Fatalf("Expected 1 pod for controller, got %d", len(pods))
	}
	controllerPod := pods[0]

	prometheusAuthority := "prometheus." + TestHelper.GetLinkerdNamespace() + ".svc.cluster.local:9090"

	for _, tt := range []struct {
		args         []string
		expectedRows map[string]string
	}{
		{
			args: []string{"stat", "deploy", "-n", TestHelper.GetLinkerdNamespace()},
			expectedRows: map[string]string{
				"controller": "1/1",
				"grafana":    "1/1",
				"prometheus": "1/1",
				"web":        "1/1",
			},
		},
		{
			args: []string{"stat", "po", "-n", TestHelper.GetLinkerdNamespace(), "--from", "deploy/controller"},
			expectedRows: map[string]string{
				prometheusPod: "1/1",
			},
		},
		{
			args: []string{"stat", "deploy", "-n", TestHelper.GetLinkerdNamespace(), "--to", "po/" + prometheusPod},
			expectedRows: map[string]string{
				"controller": "1/1",
			},
		},
		{
			args: []string{"stat", "svc", "-n", TestHelper.GetLinkerdNamespace(), "--from", "deploy/controller"},
			expectedRows: map[string]string{
				"prometheus": "1/1",
			},
		},
		{
			args: []string{"stat", "deploy", "-n", TestHelper.GetLinkerdNamespace(), "--to", "svc/prometheus"},
			expectedRows: map[string]string{
				"controller": "1/1",
			},
		},
		{
			args: []string{"stat", "ns", TestHelper.GetLinkerdNamespace()},
			expectedRows: map[string]string{
				TestHelper.GetLinkerdNamespace(): "4/4",
			},
		},
		{
			args: []string{"stat", "po", "-n", TestHelper.GetLinkerdNamespace(), "--to", "au/" + prometheusAuthority},
			expectedRows: map[string]string{
				controllerPod: "1/1",
			},
		},
		{
			args: []string{"stat", "au", "-n", TestHelper.GetLinkerdNamespace(), "--to", "po/" + prometheusPod},
			expectedRows: map[string]string{
				prometheusAuthority: "-",
			},
		},
	} {
		t.Run("linkerd "+strings.Join(tt.args, " "), func(t *testing.T) {
			err := TestHelper.RetryFor(func() error {
				out, _, err := TestHelper.LinkerdRun(tt.args...)
				if err != nil {
					t.Fatalf("Unexpected stat error: %s\n%s", err, out)
				}

				rowStats, err := parseRows(out, len(tt.expectedRows))
				if err != nil {
					return err
				}

				for name, meshed := range tt.expectedRows {
					if err := validateRowStats(name, meshed, rowStats); err != nil {
						return err
					}
				}

				return nil
			})
			if err != nil {
				t.Fatal(err.Error())
			}
		})
	}
}

// check that expectedRowCount rows have been returned
func checkRowCount(out string, expectedRowCount int) ([]string, error) {
	rows := strings.Split(out, "\n")
	rows = rows[1 : len(rows)-1] // strip header and trailing newline

	if len(rows) != expectedRowCount {
		return nil, fmt.Errorf(
			"Expected [%d] rows in stat output, got [%d]; full output:\n%s",
			expectedRowCount, len(rows), strings.Join(rows, "\n"))
	}

	return rows, nil
}

func parseRows(out string, expectedRowCount int) (map[string]*rowStat, error) {
	rows, err := checkRowCount(out, expectedRowCount)
	if err != nil {
		return nil, err
	}

	rowStats := make(map[string]*rowStat)
	for _, row := range rows {
		fields := strings.Fields(row)

		expectedColumnCount := 8
		if len(fields) != expectedColumnCount {
			return nil, fmt.Errorf(
				"Expected [%d] columns in stat output, got [%d]; full output:\n%s",
				expectedColumnCount, len(fields), row)
		}

		rowStats[fields[0]] = &rowStat{
			name:       fields[0],
			meshed:     fields[1],
			success:    fields[2],
			rps:        fields[3],
			p50Latency: fields[4],
			p95Latency: fields[5],
			p99Latency: fields[6],
			tlsPercent: fields[7],
		}
	}

	return rowStats, nil
}

func validateRowStats(name, expectedMeshCount string, rowStats map[string]*rowStat) error {
	stat, ok := rowStats[name]
	if !ok {
		return fmt.Errorf("No stats found for [%s]", name)
	}

	if stat.meshed != expectedMeshCount {
		return fmt.Errorf("Expected mesh count [%s] for [%s], got [%s]",
			expectedMeshCount, name, stat.meshed)
	}

	expectedSuccessRate := "100.00%"
	if stat.success != expectedSuccessRate {
		return fmt.Errorf("Expected success rate [%s] for [%s], got [%s]",
			expectedSuccessRate, name, stat.success)
	}

	if !strings.HasSuffix(stat.rps, "rps") {
		return fmt.Errorf("Unexpected rps for [%s], got [%s]",
			name, stat.rps)
	}

	if !strings.HasSuffix(stat.p50Latency, "ms") {
		return fmt.Errorf("Unexpected p50 latency for [%s], got [%s]",
			name, stat.p50Latency)
	}

	if !strings.HasSuffix(stat.p95Latency, "ms") {
		return fmt.Errorf("Unexpected p95 latency for [%s], got [%s]",
			name, stat.p95Latency)
	}

	if !strings.HasSuffix(stat.p99Latency, "ms") {
		return fmt.Errorf("Unexpected p99 latency for [%s], got [%s]",
			name, stat.p99Latency)
	}

	// this should be 100.00% when control plane is TLSed by default
	expectedTlsRate := "0%"
	if stat.tlsPercent != expectedTlsRate {
		return fmt.Errorf("Expected tls rate [%s] for [%s], got [%s]",
			expectedTlsRate, name, stat.tlsPercent)
	}

	return nil
}
