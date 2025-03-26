package rsaca

import (
	"context"
	"fmt"
	"os"
	"testing"

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

func TestRsaCa(t *testing.T) {
	err := TestHelper.InstallGatewayAPI()
	if err != nil {
		testutil.AnnotatedFatal(t, "failed to install gateway-api", err)
	}

	err = TestHelper.CreateControlPlaneNamespaceIfNotExists(context.Background(), TestHelper.GetLinkerdNamespace())
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", TestHelper.GetLinkerdNamespace()),
			"failed to create %s namespace: %s", TestHelper.GetLinkerdNamespace(), err)
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
		testutil.AnnotatedFatal(t, "'linkerd install --crds' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// Install control-plane with a RSA CA which has issued an ECDSA issuer certificate
	cmd = []string{
		"install",
		"--controller-log-level", "debug",
		"--set", fmt.Sprintf("proxy.image.version=%s", TestHelper.GetVersion()),
		"--identity-trust-anchors-file", "testdata/ca.crt",
		"--identity-issuer-certificate-file", "testdata/issuer.crt",
		"--identity-issuer-key-file", "testdata/issuer.key",
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

	TestHelper.WaitRollout(t, testutil.LinkerdDeployReplicasEdge)
}
