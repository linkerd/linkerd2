package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

var (
	TestHelper *testutil.TestHelper
	contexts   map[string]string
	sourceCtx  string
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until viz extension (last extension to be installed)
	// is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdVizDeployReplicas)
	// Before starting, get source context
	contexts = TestHelper.GetMulticlusterContexts()
	sourceCtx = contexts["src"]
	os.Exit(m.Run())
}

func TestGateways(t *testing.T) {
	timeout := time.Minute
	err := TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun("--context="+sourceCtx, "multicluster", "gateways")
		if err != nil {
			return err
		}
		rows := strings.Split(out, "\n")
		if len(rows) < 2 {
			return errors.New("response is empty")
		}
		fields := strings.Fields(rows[1])
		if len(fields) < 6 {
			return fmt.Errorf("unexpected number of columns: %d", len(fields))
		}
		if fields[0] != "target" {
			return fmt.Errorf("unexpected target cluster name: %s", fields[0])
		}
		if fields[1] != "True" {
			return errors.New("target cluster is not alive")
		}
		if fields[2] != "3" {
			return fmt.Errorf("invalid NUM_SVC: %s", fields[2])
		}

		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster gateways' command timed-out (%s)", timeout), err)
	}
}

func TestCheck(t *testing.T) {
	// Re-build the clientset with the source context
	if err := TestHelper.SwitchContext(sourceCtx); err != nil {
		testutil.AnnotatedFatalf(t, "failed to rebuild helper clientset with new context", "failed to rebuild helper clientset with new context [%s]: %v", sourceCtx, err)
	}
	check(t)
}

// TestCheckAfterRepairEndpoints calls `linkerd mc check` again after 1 minute,
// so that the RepairEndpoints event has already been processed, making sure
// that resyncing didn't break things
func TestCheckAfterRepairEndpoints(t *testing.T) {
	time.Sleep(time.Minute + 5*time.Second)
	check(t)
}

func check(t *testing.T) {
	cmd := []string{"--context=" + sourceCtx, "multicluster", "check", "--wait=10s"}
	timeout := 20 * time.Second
	err := TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun(cmd...)
		if err != nil {
			return fmt.Errorf("'linkerd multicluster check' command failed\n%s", out)
		}

		pods, err := TestHelper.KubernetesHelper.GetPods(context.Background(), "linkerd-multicluster", nil)
		if err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("failed to retrieve pods: %s", err), err)
		}

		tpl := template.Must(template.ParseFiles("testdata/check.multicluster.golden"))
		versionErr := healthcheck.CheckProxyVersionsUpToDate(pods, version.Channels{})
		versionErrMsg := ""
		if versionErr != nil {
			versionErrMsg = versionErr.Error()
		}
		vars := struct {
			ProxyVersionErr string
			HintURL         string
			LinkName        string
		}{
			versionErrMsg,
			healthcheck.HintBaseURL(TestHelper.GetVersion()),
			"target", // name of linked cluster, not its context
		}

		var expected bytes.Buffer
		if err := tpl.Execute(&expected, vars); err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("failed to parse check.multicluster.golden template: %s", err), err)
		}

		if out != expected.String() {
			return fmt.Errorf(
				"Expected:\n%s\nActual:\n%s", expected.String(), out)
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster check' command timed-out (%s)", timeout), err)
	}
}
