package test

import (
	"os"
	"regexp"
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
	cmd := "upgrade"
	out, stderr, err := TestHelper.LinkerdRun(cmd)
	if err != nil {
		t.Fatalf("Update command failed\n%s\n%s", out, stderr)
	}

	match, _ := regexp.MatchString("linkerd.io/control-plane-ns", out)
	if !match {
		t.Fatalf("Linkerd control plane namespace label not found in yaml.\n%s", out)
	}
}
