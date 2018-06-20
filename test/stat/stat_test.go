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
	secured    string
}

var controllerDeployments = []string{"controller", "grafana", "prometheus", "web"}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// This test retries for up to 20 seconds, since each call to "conduit stat"
// generates traffic to the deployments in the conduit namespace, and we're
// testing that those deployments are properly reporting stats. It's ok if the
// first few attempts fail due to missing stats, since the requests from those
// failed attempts will eventually be recorded in the stats that we're
// requesting, and the test will pass.
func TestCliStatForConduitNamespace(t *testing.T) {
	t.Run("test conduit stat deploy", func(t *testing.T) {
		err := TestHelper.RetryFor(20*time.Second, func() error {
			out, err := TestHelper.ConduitRun("stat", "deploy", "-n", TestHelper.GetConduitNamespace())
			if err != nil {
				t.Fatalf("Unexpected stat error: %v", err)
			}

			rowStats, err := parseRows(out, 4)
			if err != nil {
				return err
			}

			for _, name := range controllerDeployments {
				if err := validateRowStats(name, "1/1", rowStats); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			t.Fatal(err.Error())
		}
	})

	t.Run("test conduit stat namespace", func(t *testing.T) {
		err := TestHelper.RetryFor(20*time.Second, func() error {
			fmt.Println("HEREEEEE")
			out, err := TestHelper.ConduitRun("stat", "ns", TestHelper.GetConduitNamespace())
			if err != nil {
				t.Fatalf("Unexpected stat error: %v", err)
			}

			rowStats, err := parseRows(out, 1)
			if err != nil {
				return err
			}

			return validateRowStats(TestHelper.GetConduitNamespace(), "4/4", rowStats)
		})
		if err != nil {
			t.Fatal(err.Error())
		}
	})

	t.Run("test named deploy query", func(t *testing.T) {
		err := TestHelper.RetryFor(10*time.Second, func() error {
			fmt.Println("HEREEEEE")
			out, err := TestHelper.ConduitRun("stat", "deploy/web", "--all-namespaces")
			if err == nil {
				t.Fatalf("Unexpected stat error: %v", err)
			}

			fmt.Println(out)
			return nil
		})
		if err != nil {
			t.Fatal(err.Error())
		}
	})
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
			secured:    fields[7],
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

	// this should be 100.00% when control plane is secure by default
	expectedSecuredRate := "0%"
	if stat.secured != expectedSecuredRate {
		return fmt.Errorf("Expected secured rate [%s] for [%s], got [%s]",
			expectedSecuredRate, name, stat.secured)
	}

	return nil
}
