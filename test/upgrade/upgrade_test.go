package test

import (
	"os"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestUpgradeWithUpdatedConfig(t *testing.T) {
	upgradeResources, err := testutil.ReadFile("testdata/label_update.yaml")
	if err != nil {
		t.Fatalf("failed to read label update file: %s", err)
	}

	kubectlOut, err := TestHelper.Kubectl(upgradeResources, "apply", "--prune", "-l", "linkerd.io/control-plane-ns=linkerd", "-oyaml", "-f", "-")
	if err != nil {
		t.Fatalf("kubectl get command failed with %s\n%s", err, kubectlOut)
	}

	golden := "kubectl.apply.golden"
	err = TestHelper.ValidateOutput(kubectlOut, golden)
	if err != nil {
		t.Fatal(err.Error())
	}
}
