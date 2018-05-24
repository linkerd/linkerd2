package get

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/runconduit/conduit/testutil"
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
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// This test retries for up to 20 seconds, since each call to "conduit stat"
// generates traffic to the controller and prometheus deployments in the conduit
// namespace, and we're testing that those deployments are properly reporting
// stats. It's ok if the first few attempts fail due to missing stats, since the
// requests from those failed attempts will eventually be recorded in the stats
// that we're requesting, and the test will pass. Note that we're not validating
// stats for the web and grafana deployments, since we aren't guaranteeing that
// they're receiving traffic as part of this test.
func TestCliStatForConduitNamespace(t *testing.T) {

	err := TestHelper.RetryFor(20*time.Second, func() error {
		out, err := TestHelper.ConduitRun("stat", "deploy", "-n", TestHelper.GetConduitNamespace())
		if err != nil {
			t.Fatalf("Unexpected stat error: %v", err)
		}

		rowStats, err := parseRowStats(out)
		if err != nil {
			return err
		}

		for _, name := range []string{"controller", "prometheus"} {
			if err := validateRowStats(name, rowStats); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err.Error())
	}

}

func parseRowStats(out string) (map[string]*rowStat, error) {
	rows := strings.Split(out, "\n")
	rows = rows[1 : len(rows)-1] // strip header and trailing newline

	expectedRowCount := 4
	if len(rows) != expectedRowCount {
		return nil, fmt.Errorf(
			"Expected [%d] rows in stat output, got [%d]; full output:\n%s",
			expectedRowCount, len(rows), strings.Join(rows, "\n"))
	}

	rowStats := make(map[string]*rowStat)
	for _, row := range rows {
		fields := strings.Fields(row)

		expectedColumnCount := 7
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
		}
	}

	return rowStats, nil
}

func validateRowStats(name string, rowStats map[string]*rowStat) error {
	stat, ok := rowStats[name]
	if !ok {
		return fmt.Errorf("No stats found for [%s]", name)
	}

	expectedMeshCount := "1/1"
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

	return nil
}
