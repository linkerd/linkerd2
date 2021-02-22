package target1

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
	if !TestHelper.Multicluster() {
		fmt.Fprintln(os.Stderr, "Multicluster test disabled")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestInstallEmojivoto(t *testing.T) {
	if err := TestHelper.CreateDataPlaneNamespaceIfNotExists(context.Background(), "emojivoto", nil); err != nil {
		testutil.AnnotatedFatalf(t, "failed to create emojivoto namespace",
			"failed to create emojivoto namespace: %s", err)
	}
	yaml, err := testutil.ReadFile("testdata/emojivoto-no-bot.yml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read 'emojivoto-no-bot.yml'", "failed to read 'emojivoto-no-bot.yml': %s", err)
	}
	out, err := TestHelper.KubectlApply(yaml, "emojivoto")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to install emojivoto", "failed to install emojivoto: %s\n%s", err, out)
	}
}
