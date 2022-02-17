package viztest

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper *testutil.TestHelper
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// TestInstallLinkerd will install the linkerd control plane to be used in the rest of
// the deep suite tests
func TestInstallLinkerd(t *testing.T) {
	cmd := []string{
		"install",
		"--controller-log-level", "debug",
		"--proxy-version", TestHelper.GetVersion(),
		"--set", "heartbeatSchedule=1 2 3 4 5",
	}

	// Pipe cmd & args to `linkerd`
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	TestHelper.WaitRollout(t, testutil.LinkerdDeployReplicasEdge)
}

// TestInstallViz will install the viz extension to be used by the rest of the
// tests in the viz suite
func TestInstallViz(t *testing.T) {
	cmd := []string{
		"viz",
		"install",
		"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()),
	}

	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd viz install' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	TestHelper.WaitRollout(t, testutil.LinkerdVizDeployReplicas)

}

func TestCheckViz(t *testing.T) {
	cmd := []string{"viz", "check", "--wait=60m"}
	golden := "check.viz.golden"

	pods, err := TestHelper.KubernetesHelper.GetPods(context.Background(), TestHelper.GetVizNamespace(), nil)
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to retrieve pods: %s", err), err)
	}

	tpl := template.Must(template.ParseFiles("testdata" + "/" + golden))
	vars := struct {
		ProxyVersionErr string
		HintURL         string
	}{
		healthcheck.CheckProxyVersionsUpToDate(pods, version.Channels{}).Error(),
		healthcheck.HintBaseURL(TestHelper.GetVersion()),
	}

	var expected bytes.Buffer
	if err := tpl.Execute(&expected, vars); err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to parse %s template: %s", golden, err), err)
	}

	timeout := 5 * time.Minute
	err = TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun(cmd...)
		if err != nil {
			return fmt.Errorf("'linkerd viz check' command failed\n%w", err)
		}

		if out != expected.String() {
			return fmt.Errorf(
				"Expected:\n%s\nActual:\n%s", expected.String(), out)
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd viz check' command timed-out (%s)", timeout), err)
	}
}
