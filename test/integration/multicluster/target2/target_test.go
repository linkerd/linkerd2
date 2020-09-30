package target2

import (
	"errors"
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
	os.Exit(testutil.Run(m, TestHelper))
}

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
		for _, row := range strings.Split(out, "\n") {
			if strings.Contains(row, "http://web-svc.emojivoto.svc.cluster.local:80/api/vote") {
				return nil
			}
		}
		return errors.New("web-svc logs in target cluster were empty")
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster gateways' command timed-out (%s)", timeout), err)
	}
}
