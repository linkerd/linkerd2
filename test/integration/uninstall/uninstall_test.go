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
	os.Exit(testutil.Run(m, TestHelper))
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
	for deploy, spec := range testutil.LinkerdDeployReplicas {
		if err := TestHelper.CheckPods(ctx, TestHelper.GetLinkerdNamespace(), deploy, spec.Replicas); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}
		if err := TestHelper.CheckDeployment(ctx, TestHelper.GetLinkerdNamespace(), deploy, spec.Replicas); err != nil {
			testutil.AnnotatedFatalf(t, "CheckDeployment timed-out", "Error validating deployment [%s]:\n%s", deploy, err)
		}
	}
}

func TestUninstall(t *testing.T) {
	args := []string{"uninstall"}
	out, err := TestHelper.LinkerdRun(args...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	args = []string{"delete", "-f", "-"}
	out, err = TestHelper.Kubectl(out, args...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
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
