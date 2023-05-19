package policy

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

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until viz extension is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdVizDeployReplicas)
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestPolicy(t *testing.T) {
	ctx := context.Background()

	// Test authorization stats
	TestHelper.WithDataPlaneNamespace(ctx, "stat-authz-test", map[string]string{}, t, func(t *testing.T, prefixedNs string) {
		emojivotoYaml, err := testutil.ReadFile("testdata/emojivoto.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to read emojivoto yaml",
				"failed to read emojivoto yaml\n%s\n", err)
		}
		emojivotoYaml = strings.ReplaceAll(emojivotoYaml, "___NS___", prefixedNs)
		out, stderr, err := TestHelper.PipeToLinkerdRun(emojivotoYaml, "inject", "-")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
				"'linkerd inject' command failed\n%s\n%s", out, stderr)
		}

		out, err = TestHelper.KubectlApply(out, prefixedNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to apply emojivoto resources",
				"failed to apply emojivoto resources: %s\n %s", err, out)
		}

		emojivotoPolicy, err := testutil.ReadFile("testdata/emoji-policy.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to read emoji-policy yaml",
				"failed to read emoji-policy yaml\n%s\n", err)
		}

		out, err = TestHelper.KubectlApply(emojivotoPolicy, prefixedNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to apply emojivoto policy resources",
				"failed to apply emojivoto policy resources: %s\n %s", err, out)
		}

		// wait for deployments to start
		for _, deploy := range []string{"web", "emoji", "vote-bot", "voting"} {
			if err := TestHelper.CheckPods(ctx, prefixedNs, deploy, 1); err != nil {
				//nolint:errorlint
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
			timeout := 3 * time.Minute
			t.Run("linkerd "+strings.Join(tt.args, " "), func(t *testing.T) {
				err := testutil.RetryFor(timeout, func() error {
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
					testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out checking policy stats (%s)", timeout), err)
				}
			})
		}
	})
}

type noSuccess struct{ name string }

func (e noSuccess) Error() string {
	return fmt.Sprintf("no success rate reported for %s", e.name)
}

func validateAuthzRows(name string, rowStats map[string]*testutil.RowStat, isServer bool) error {
	stat, ok := rowStats[name]
	if !ok {
		return fmt.Errorf("No stats found for [%s]", name)
	}

	// Check for suffix only, as the value will not be 100% always with
	// the normal emojivoto sample
	if stat.Success == "-" {
		return noSuccess{name}
	}
	if !strings.HasSuffix(stat.Success, "%") {
		return fmt.Errorf("Unexpected success rate for [%s], got [%s]",
			name, stat.Success)
	}

	if isServer {
		if !strings.HasSuffix(stat.UnauthorizedRPS, "rps") {
			return fmt.Errorf("Unexpected Unauthorized RPS for [%s], got [%s]",
				name, stat.UnauthorizedRPS)
		}
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
			return fmt.Errorf("Error parsing number of TCP connections [%s]: %w", stat.TCPOpenConnections, err)
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
