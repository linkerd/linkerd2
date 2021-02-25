package target2

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	if !TestHelper.Multicluster() {
		fmt.Fprintln(os.Stderr, "Multicluster test disabled")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestTargetTraffic inspects the target cluster's web-svc pod to see if the
// source cluster's vote-bot has been able to hit it with requests. If it has
// successfully issued requests, then we'll see log messages indicating that the
// web-svc can't reach the voting-svc (because it's not running).
//
// TODO it may be clearer to invoke `linkerd diagnostics proxy-metrics` to check whether we see
// connections from the gateway pod to the web-svc?
func TestTargetTraffic(t *testing.T) {
	timeout := time.Minute
	err := TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.Kubectl("",
			"--namespace", "emojivoto",
			"logs",
			"--selector", "app=web-svc",
			"--container", "web-svc",
		)
		if err != nil {
			return fmt.Errorf("%s\n%s", err, out)
		}
		// Check for expected error messages
		for _, row := range strings.Split(out, "\n") {
			if strings.Contains(row, " /api/vote?choice=:doughnut: ") {
				return nil
			}
		}
		return fmt.Errorf("web-svc logs in target cluster do not include voting errors\n%s", out)
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster gateways' command timed-out (%s)", timeout), err)
	}
}
