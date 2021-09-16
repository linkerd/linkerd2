package localhost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

type jsonStats struct {
	Namespace          string   `json:"namespace"`
	Name               string   `json:"name"`
	Success            *float64 `json:"success"`
	Rps                *float64 `json:"rps"`
	TCPOpenConnections *int     `json:"tcp_open_connections"`
}

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

// TestLocalhostServer creates an nginx deployment which listens on localhost
// and a slow-cooker which attempts to send traffic to the nginx.  Since
// slow-cooker should not be able to connect to nginx's localhost address,
// these requests should fail.
func TestLocalhostServer(t *testing.T) {
	ctx := context.Background()
	nginx, err := TestHelper.LinkerdRun("inject", "testdata/nginx.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}
	slowcooker, err := TestHelper.LinkerdRun("inject", "testdata/slow-cooker.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	TestHelper.WithDataPlaneNamespace(ctx, "localhost-test", map[string]string{}, t, func(t *testing.T, ns string) {

		out, err := TestHelper.KubectlApply(nginx, ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}
		out, err = TestHelper.KubectlApply(slowcooker, ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}

		for _, deploy := range []string{"nginx", "slow-cooker"} {
			err = TestHelper.CheckPods(ctx, ns, deploy, 1)
			if err != nil {
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		err = TestHelper.RetryFor(50*time.Second, func() error {
			// Use a short time window so that transient errors at startup
			// fall out of the window.
			out, err := TestHelper.LinkerdRun("viz", "stat", "-n", ns, "deploy/slow-cooker", "--to", "deploy/nginx", "-t", "30s", "-o", "json")
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected stat error",
					"unexpected stat error: %s\n%s", err, out)
			}

			var stats []jsonStats
			err = json.Unmarshal([]byte(out), &stats)
			if err != nil {
				return fmt.Errorf("failed to parse json stat output: %s\n%s", err, out)
			}

			if len(stats) != 1 {
				return fmt.Errorf("expected 1 row of stat output, got: %s", out)
			}

			if stats[0].Rps == nil || *stats[0].Rps == 0.0 {
				return fmt.Errorf("expected non-zero RPS from slowcooker to nginx: %s", out)
			}

			// Requests sent to a port which is only bound to localhost should
			// fail.
			if *stats[0].Success >= 1.0 {
				return fmt.Errorf("expected zero success-rate from slowcooker to nginx: %s", out)
			}
			if *stats[0].TCPOpenConnections > 0 {
				return fmt.Errorf("expected no tcp connection from slowcooker to nginx: %s", out)
			}

			return nil
		})
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected stat output", "unexpected stat output: %v", err)
		}
	})
}
