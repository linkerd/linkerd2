package uninstall

import (
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
		"--proxy-log-level", "warn,linkerd2_proxy=debug",
		"--proxy-version", TestHelper.GetVersion(),
	}

	out, stderr, err := TestHelper.LinkerdRun(args...)
	if err != nil {
		t.Fatalf("linkerd install command failed: \n%s\n%s", out, stderr)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}
}

func TestResourcesPostInstall(t *testing.T) {
	// Tests Namespace
	err := TestHelper.CheckIfNamespaceExists(TestHelper.GetLinkerdNamespace())
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}

	// Tests Pods and Deployments
	for deploy, spec := range testutil.LinkerdDeployReplicas {
		if err := TestHelper.CheckPods(TestHelper.GetLinkerdNamespace(), deploy, spec.Replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating pods for deploy [%s]:\n%s", deploy, err))
		}
		if err := TestHelper.CheckDeployment(TestHelper.GetLinkerdNamespace(), deploy, spec.Replicas); err != nil {
			t.Fatal(fmt.Errorf("Error validating deploy [%s]:\n%s", deploy, err))
		}
	}
}

func TestUninstall(t *testing.T) {
	args := []string{"uninstall"}
	out, stderr, err := TestHelper.LinkerdRun(args...)
	if err != nil {
		t.Fatalf("linkerd install command failed: \n%s\n%s", out, stderr)
	}

	args = []string{"delete", "-f", "-"}
	out, err = TestHelper.Kubectl(out, args...)
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}
}

func TestCheckPostUninstall(t *testing.T) {
	cmd := []string{"check", "--pre", "--expected-version", TestHelper.GetVersion()}
	golden := "check.pre.golden"
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("Check command failed\n%s\n%s", out, stderr)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}
}
