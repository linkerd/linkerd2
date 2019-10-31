package trafficsplit

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

const zeroRPS = "0.0rps"

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

type statTsRow struct {
	name       string
	apex       string
	leaf       string
	weight     string
	success    string
	rps        string
	latencyP50 string
	latencyP95 string
	latencyP99 string
}

func parseStatTsRow(out string, expectedRowCount, expectedColumnCount int) (map[string]*statTsRow, error) {
	rows, err := testutil.CheckRowCount(out, expectedRowCount)
	if err != nil {
		return nil, err
	}

	statRows := make(map[string]*statTsRow)

	for _, row := range rows {
		fields := strings.Fields(row)

		if len(fields) != expectedColumnCount {
			return nil, fmt.Errorf(
				"Expected [%d] columns in stat output, got [%d]; full output:\n%s",
				expectedColumnCount, len(fields), row)
		}

		row := &statTsRow{
			name:       fields[0],
			apex:       fields[1],
			leaf:       fields[2],
			weight:     fields[3],
			success:    fields[4],
			rps:        fields[5],
			latencyP50: fields[6],
			latencyP95: fields[7],
			latencyP99: fields[8],
		}

		statRows[row.leaf] = row

	}
	return statRows, nil
}

func statTrafficSplit(from string, ns string) (map[string]*statTsRow, error) {
	cmd := []string{"stat", "ts", "--from", from, "--namespace", ns}
	stdOut, _, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		return nil, err
	}
	return parseStatTsRow(stdOut, 2, 9)
}

func validateTrafficSplit(actual *statTsRow, expected *statTsRow) error {

	if actual.name != expected.name && expected.name != "" {
		return fmt.Errorf("expected name '%s' for leaf %s, got '%s'", expected.name, expected.leaf, actual.name)
	}

	if actual.apex != expected.apex && expected.apex != "" {
		return fmt.Errorf("expected apex '%s' for leaf %s, got '%s'", expected.apex, expected.leaf, actual.apex)
	}

	if actual.weight != expected.weight && expected.weight != "" {
		return fmt.Errorf("expected weight '%s' for leaf %s, got '%s'", expected.weight, expected.leaf, actual.weight)
	}

	if actual.success != expected.success && expected.success != "" {
		return fmt.Errorf("expected success '%s' for leaf %s, got '%s'", expected.success, expected.leaf, actual.success)
	}

	if expected.rps != "" {
		if expected.rps == "-" && actual.rps != "-" {
			return fmt.Errorf("expected no rps for leaf %s, got '%s'", expected.leaf, actual.rps)
		}

		if expected.rps == zeroRPS && actual.rps != zeroRPS {
			return fmt.Errorf("expected zero rps for leaf %s, got '%s'", expected.leaf, actual.rps)
		}

		if expected.rps != zeroRPS && actual.rps == zeroRPS {
			return fmt.Errorf("expected non zero rps for leaf %s, got '%s'", expected.leaf, actual.rps)
		}
	}

	if actual.latencyP50 != expected.latencyP50 && expected.latencyP50 != "" {
		return fmt.Errorf("expected latencyP50 '%s' for leaf %s, got '%s'", expected.latencyP50, expected.leaf, actual.latencyP50)
	}

	if actual.latencyP95 != expected.latencyP95 && expected.latencyP95 != "" {
		return fmt.Errorf("expected latencyP95 '%s' for leaf %s, got '%s'", expected.latencyP95, expected.leaf, actual.latencyP95)
	}

	if actual.latencyP99 != expected.latencyP99 && expected.latencyP99 != "" {
		return fmt.Errorf("expected latencyP99 '%s' for leaf %s, got '%s'", expected.latencyP99, expected.leaf, actual.latencyP99)
	}

	return nil
}

func validateExpectedTsOutput(rows map[string]*statTsRow, expectedBackendSvc, expectedFailingSvc *statTsRow) error {
	backendSvcLeafKey := "backend-svc"
	backendFailingSvcLeafKey := "failing-svc"

	backendRow, ok := rows[backendSvcLeafKey]
	if !ok {
		return fmt.Errorf("no stats found for [%s]", backendSvcLeafKey)
	}

	if err := validateTrafficSplit(backendRow, expectedBackendSvc); err != nil {
		return err
	}

	backendFailingRow, ok := rows[backendFailingSvcLeafKey]
	if !ok {
		return fmt.Errorf("no stats found for [%s]", backendFailingSvcLeafKey)
	}

	if err := validateTrafficSplit(backendFailingRow, expectedFailingSvc); err != nil {
		return err
	}
	return nil
}

func TestTrafficSplitCli(t *testing.T) {
	out, _, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/traffic_split_application.yaml")
	if err != nil {
		t.Fatalf("linkerd inject command failed\n%s", out)
	}

	prefixedNs := TestHelper.GetTestNamespace("trafficsplit-test")
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(prefixedNs, nil)
	if err != nil {
		t.Fatalf("failed to create %s namespace: %s", prefixedNs, err)
	}
	out, err = TestHelper.KubectlApply(out, prefixedNs)
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}

	// wait for deployments to start
	for _, deploy := range []string{"backend", "failing", "slow-cooker"} {
		if err := TestHelper.CheckPods(prefixedNs, deploy, 1); err != nil {
			t.Error(err)
		}

		if err := TestHelper.CheckDeployment(prefixedNs, deploy, 1); err != nil {
			t.Error(fmt.Errorf("Error validating deployment [%s]:\n%s", deploy, err))
		}
	}

	t.Run("ensure traffic is sent to one backend only", func(t *testing.T) {
		err := TestHelper.RetryFor(40*time.Second, func() error {

			rows, err := statTrafficSplit("deploy/slow-cooker", prefixedNs)
			if err != nil {
				t.Fatal(err.Error())
			}
			expectedBackendSvcOutput := &statTsRow{
				name:    "backend-traffic-split",
				apex:    "backend-svc",
				leaf:    "backend-svc",
				weight:  "500m",
				success: "100.00%",
				rps:     "0.5rps",
			}
			expectedFailingSvcOutput := &statTsRow{
				name:       "backend-traffic-split",
				apex:       "backend-svc",
				leaf:       "backend-svc",
				weight:     "0",
				success:    "-",
				rps:        "-",
				latencyP50: "-",
				latencyP95: "-",
				latencyP99: "-",
			}

			if err := validateExpectedTsOutput(rows, expectedBackendSvcOutput, expectedFailingSvcOutput); err != nil {
				return err
			}
			return nil
		})

		if err != nil {
			t.Fatal(err.Error())
		}
	})

	t.Run("update traffic split resource with equal weights", func(t *testing.T) {

		updatedTsResourceFile := "testdata/updated-traffic-split-leaf-weights.yaml"
		updatedTsResource, err := testutil.ReadFile(updatedTsResourceFile)

		if err != nil {
			t.Fatalf("Cannot read updated traffic split resource: %s, %s", updatedTsResource, err)
		}

		out, err := TestHelper.KubectlApply(updatedTsResource, prefixedNs)
		if err != nil {
			t.Fatalf("Failed to update traffic split resource: %s\n %s", err, out)
		}
	})

	t.Run("ensure traffic is sent to both backends", func(t *testing.T) {
		err := TestHelper.RetryFor(40*time.Second, func() error {
			rows, err := statTrafficSplit("deploy/slow-cooker", prefixedNs)
			if err != nil {
				t.Fatal(err.Error())
			}
			expectedBackendSvcOutput := &statTsRow{
				name:    "backend-traffic-split",
				apex:    "backend-svc",
				leaf:    "backend-svc",
				weight:  "500m",
				success: "100.00%",
				rps:     "0.5rps",
			}
			expectedFailingSvcOutput := &statTsRow{
				name:    "backend-traffic-split",
				apex:    "backend-svc",
				leaf:    "backend-svc",
				weight:  "500m",
				success: "0.00%",
				rps:     "0.5rps",
			}

			if err := validateExpectedTsOutput(rows, expectedBackendSvcOutput, expectedFailingSvcOutput); err != nil {
				return err
			}
			return nil
		})

		if err != nil {
			t.Fatal(err.Error())
		}
	})
}
