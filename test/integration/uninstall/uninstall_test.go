package uninstall

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	if !TestHelper.Uninstall() {
		fmt.Fprintln(os.Stderr, "Uninstall test disabled")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestInstall(t *testing.T) {
	args := []string{
		"install",
		"--controller-log-level", "debug",
		"--proxy-log-level", "warn,linkerd=debug",
		"--proxy-version", TestHelper.GetVersion(),
	}

	out, err := TestHelper.LinkerdRun(args...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	var (
		vizCmd  = []string{"viz", "install"}
		vizArgs = []string{
			"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace())}
	)

	// Install Linkerd Viz Extension
	exec := append(vizCmd, vizArgs...)
	out, err = TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd viz install' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

}

func TestResourcesPostInstall(t *testing.T) {
	ctx := context.Background()
	// Tests Namespace
	err := TestHelper.CheckIfNamespaceExists(ctx, TestHelper.GetLinkerdNamespace())
	if err != nil {
		testutil.AnnotatedFatalf(t, "received unexpected output",
			"received unexpected output\n%s", err.Error())
	}

	// Tests Pods and Deployments

	expectedDeployments := testutil.LinkerdDeployReplicasEdge
	if !TestHelper.ExternalPrometheus() {
		expectedDeployments["prometheus"] = testutil.DeploySpec{Namespace: "linkerd-viz", Replicas: 1}
	}
	// Upgrade Case
	if TestHelper.UpgradeHelmFromVersion() != "" {
		expectedDeployments = testutil.LinkerdDeployReplicasStable
	}
	for deploy, spec := range expectedDeployments {
		if err := TestHelper.CheckPods(ctx, spec.Namespace, deploy, spec.Replicas); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}
	}
}

func TestUninstall(t *testing.T) {
	var (
		vizCmd = []string{"viz", "uninstall"}
	)

	// Uninstall Linkerd Viz Extension
	out, err := TestHelper.LinkerdRun(vizCmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd viz uninstall' command failed", err)
	}

	args := []string{"delete", "-f", "-"}
	out, err = TestHelper.Kubectl(out, args...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl delete' command failed",
			"'kubectl delete' command failed\n%s", out)
	}

	args = []string{"uninstall"}
	out, err = TestHelper.LinkerdRun(args...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	args = []string{"delete", "-f", "-"}
	out, err = TestHelper.Kubectl(out, args...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl delete' command failed",
			"'kubectl delete' command failed\n%s", out)
	}
}

func TestCheckPostUninstall(t *testing.T) {
	cmd := []string{"check", "--pre", "--expected-version", TestHelper.GetVersion()}
	golden := "check.pre.golden"
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "check command failed", err)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		testutil.AnnotatedFatalf(t, "received unexpected output",
			"received unexpected output\n%s", err.Error())
	}
}
