package multiclustertest

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

// TestInstall will install the linkerd control plane to be used in the rest of
// the deep suite tests.
func TestInstall(t *testing.T) {
	certsPath := TestHelper.CertsPath()
	if certsPath == "" {
		testutil.AnnotatedFatal(t, "cannot run multicluster test without a valid certificate path")
	}

	cmd := []string{
		"install",
		"--controller-log-level", "debug",
		"--proxy-version", TestHelper.GetVersion(),
		"--set", "heartbeatSchedule=1 2 3 4 5",
		"--identity-trust-anchors-file", certsPath + "/ca.crt",
		"--identity-issuer-certificate-file", certsPath + "/issuer.crt",
		"--identity-issuer-key-file", certsPath + "/issuer.key",
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

func TestInstallMulticluster(t *testing.T) {
	out, err := TestHelper.LinkerdRun("multicluster", "install")
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd multicluster install' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}
	TestHelper.AddInstalledExtension("multicluster")
}

func TestMulticlusterResourcesPostInstall(t *testing.T) {
	multiclusterSvcs := []testutil.Service{
		{Namespace: "linkerd-multicluster", Name: "linkerd-gateway"},
	}

	testutil.TestResourcesPostInstall(TestHelper.GetMulticlusterNamespace(), multiclusterSvcs, testutil.MulticlusterDeployReplicas, TestHelper, t)
}

// TestInstallViz will install the viz extension, needed to verify whether the
// gateway probe succeeded.
// TODO (matei): can the dependency on viz be removed?
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

func TestCheckMulticluster(t *testing.T) {
	cmd := []string{"multicluster", "check", "--wait=10s"}
	golden := "check.multicluster.golden"
	timeout := time.Minute
	err := TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun(cmd...)
		if err != nil {
			return fmt.Errorf("'linkerd multicluster check' command failed\n%s", out)
		}
		err = TestHelper.ValidateOutput(out, golden)
		if err != nil {
			return fmt.Errorf("received unexpected output: %w", err)
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster check' command timed-out (%s)", timeout), err)
	}
}
