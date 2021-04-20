package trafficsplit

import (
	"context"
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
	cmd := []string{"viz", "stat", "ts", "--from", from, "--namespace", ns, "-t", "30s", "--unmeshed"}
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		return nil, err
	}
	return parseStatTsRow(out, 2, 9)
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

	for _, version := range []string{"v1alpha1", "v1alpha2"} {
		// pin version variable
		version := version
		ctx := context.Background()
		TestHelper.WithDataPlaneNamespace(ctx, fmt.Sprintf("trafficsplit-test-%s", version), map[string]string{}, t, func(t *testing.T, prefixedNs string) {
			out, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/application.yaml")
			if err != nil {
				testutil.AnnotatedFatal(t, "'linkerd inject' command failed", err)
			}

			out, err = TestHelper.KubectlApply(out, prefixedNs)
			if err != nil {
				testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
					"'kubectl apply' command failed\n%s", out)
			}

			TsResourceFile := fmt.Sprintf("testdata/%s/traffic-split-leaf-weights.yaml", version)
			TsResource, err := testutil.ReadFile(TsResourceFile)

			if err != nil {
				testutil.AnnotatedFatalf(t, "cannot read updated traffic split resource",
					"cannot read updated traffic split resource: %s, %s", TsResource, err)
			}

			out, err = TestHelper.KubectlApply(TsResource, prefixedNs)
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to update traffic split resource",
					"failed to update traffic split resource: %s\n %s", err, out)
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

			t.Run(fmt.Sprintf("ensure traffic is sent to one backend only for %s", version), func(t *testing.T) {
				timeout := 40 * time.Second
				err := TestHelper.RetryFor(timeout, func() error {

					rows, err := statTrafficSplit("deploy/slow-cooker", prefixedNs)
					if err != nil {
						testutil.AnnotatedFatal(t, "error ensuring traffic is sent to one backend only", err)
					}

					var weight string
					if version == "v1alpha1" {
						weight = "500m"
					} else {
						weight = "500"
					}

					expectedBackendSvcOutput := &statTsRow{
						name:    "backend-traffic-split",
						apex:    "backend-svc",
						leaf:    "backend-svc",
						weight:  weight,
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
					testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out ensuring traffic is sent to one backend only (%s)", timeout), err)
				}
			})

			t.Run(fmt.Sprintf("update traffic split resource with equal weights for %s", version), func(t *testing.T) {

				updatedTsResourceFile := fmt.Sprintf("testdata/%s/updated-traffic-split-leaf-weights.yaml", version)
				updatedTsResource, err := testutil.ReadFile(updatedTsResourceFile)

				if err != nil {
					testutil.AnnotatedFatalf(t, "cannot read updated traffic split resource",
						"cannot read updated traffic split resource: %s, %s", updatedTsResource, err)
				}

				out, err := TestHelper.KubectlApply(updatedTsResource, prefixedNs)
				if err != nil {
					testutil.AnnotatedFatalf(t, "failed to update traffic split resource",
						"failed to update traffic split resource: %s\n %s", err, out)
				}
			})

			t.Run(fmt.Sprintf("ensure traffic is sent to both backends for %s", version), func(t *testing.T) {
				timeout := 40 * time.Second
				err := TestHelper.RetryFor(timeout, func() error {
					rows, err := statTrafficSplit("deploy/slow-cooker", prefixedNs)
					if err != nil {
						testutil.AnnotatedFatal(t, "error ensuring traffic is sent to both backends", err)
					}

					var weight string
					if version == "v1alpha1" {
						weight = "500m"
					} else {
						weight = "500"
					}

					expectedBackendSvcOutput := &statTsRow{
						name:    "backend-traffic-split",
						apex:    "backend-svc",
						leaf:    "backend-svc",
						weight:  weight,
						success: "100.00%",
						rps:     "0.5rps",
					}
					expectedFailingSvcOutput := &statTsRow{
						name:    "backend-traffic-split",
						apex:    "backend-svc",
						leaf:    "backend-svc",
						weight:  weight,
						success: "0.00%",
						rps:     "0.5rps",
					}

					if err := validateExpectedTsOutput(rows, expectedBackendSvcOutput, expectedFailingSvcOutput); err != nil {
						return err
					}
					return nil
				})

				if err != nil {
					testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out ensuring traffic is sent to both backends (%s)", timeout), err)
				}
			})
		})
	}
}
