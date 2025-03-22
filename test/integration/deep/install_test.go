package deeptest

import (
	"fmt"
	"os"
	"testing"
	"time"

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

func TestInstallCalico(t *testing.T) {
	if !TestHelper.CNI() {
		return
	}

	out, err := TestHelper.Kubectl("", []string{"apply", "-f", "https://k3d.io/v5.1.0/usage/advanced/calico.yaml"}...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"kubectl apply command failed\n%s", out)
	}

	time.Sleep(10 * time.Second)
	o, err := TestHelper.Kubectl("", "--namespace=kube-system", "wait", "--for=condition=available", "--timeout=120s", "deploy/calico-kube-controllers")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to wait for condition=available for calico resources",
			"failed to wait for condition=available for calico resources: %s: %s", err, o)
	}
}

func TestInstallCNIPlugin(t *testing.T) {
	if !TestHelper.CNI() {
		return
	}

	// install the CNI plugin in the cluster
	var (
		cmd  = "install-cni"
		args = []string{
			"--use-wait-flag",
			"--cni-log-level=debug",
			// For Flannel (k3d's default CNI) the following settings are required.
			// For Calico the default ones are fine.
			// "--dest-cni-net-dir=/var/lib/rancher/k3s/agent/etc/cni/net.d",
			// "--dest-cni-bin-dir=/bin",
		}
	)

	exec := append([]string{cmd}, args...)
	out, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install-cni' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// perform a linkerd check with --linkerd-cni-enabled
	timeout := time.Minute
	err = testutil.RetryFor(timeout, func() error {
		out, err = TestHelper.LinkerdRun("check", "--pre", "--linkerd-cni-enabled", "--wait=60m")
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd check' command timed-out (%s)", timeout), err)
	}
}

// TestInstall will install the linkerd control plane to be used in the rest of
// the deep suite tests.
func TestInstall(t *testing.T) {
	err := TestHelper.InstallGatewayAPI()
	if err != nil {
		testutil.AnnotatedFatal(t, "failed to install gateway-api", err)
	}

	// Install CRDs
	cmd := []string{
		"install",
		"--crds",
		"--controller-log-level", "debug",
		"--set", fmt.Sprintf("proxy.image.version=%s", TestHelper.GetVersion()),
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

	// Install control-plane
	cmd = []string{
		"install",
		"--controller-log-level", "debug",
		"--set", fmt.Sprintf("proxy.image.version=%s", TestHelper.GetVersion()),
		"--set", "heartbeatSchedule=1 2 3 4 5",
	}

	// If testing deep suite with CNI, set --cni-enabled to true
	if TestHelper.CNI() {
		cmd = append(cmd, "--linkerd-cni-enabled")
	}

	if TestHelper.NativeSidecar() {
		cmd = append(cmd, "--set", "proxy.nativeSidecar=true")
	}

	if TestHelper.DualStack() {
		cmd = append(cmd, "--set", "disableIPv6=false")
	}

	// Pipe cmd & args to `linkerd`
	out, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	out, err = TestHelper.LinkerdRun("check", "--wait=3m")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd check' command failed",
			"'linkerd check' command failed\n%s", out)
	}
}
