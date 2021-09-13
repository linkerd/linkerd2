package target3

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

// TestSetupTargetClusterResources applies the nginx-statefulset manifest in the target cluster in the "default" namespace.
func TestSetupTargetClusterResources(t *testing.T) {
	if err := TestHelper.CreateDataPlaneNamespaceIfNotExists(context.Background(), "default", nil); err != nil {
		testutil.AnnotatedFatalf(t, "failed to create default namespace",
			"failed to create default namespace: %s", err)
	}
	yaml, err := testutil.ReadFile("testdata/nginx-statefulset.yml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read 'nginx-statefulset.yml'", "failed to read 'nginx-statefulset.yml': %s", err)
	}
	out, err := TestHelper.KubectlApply(yaml, "default")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to set up target cluster resources", "failed to set up target cluster resources: %s\n%s", err, out)
	}
}
