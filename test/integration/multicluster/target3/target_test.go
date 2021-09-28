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

// TestSetupNginx applies the nginx-statefulset.yml manifest in the target cluster in the "default" namespace, and mirrors nginx-svc to source cluster
func TestSetupNginx(t *testing.T) {
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
		testutil.AnnotatedFatalf(t, "failed to set up nginx resources", "failed to set up target nginx resources: %s\n%s", err, out)
	}

	// mirror nginx-svc to source
	out, err = TestHelper.Kubectl("", "--namespace=default", "label", "svc", "nginx-svc", "mirror.linkerd.io/exported=true")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to mirror nginx-svc", "failed to mirror nginx-svc: %s\n%s", err, out)
	}
}

// TestSetupSlowCooker should apply the slow-cooker.yml manifest in the source cluster in the "default" namespace
func TestSetupSlowCooker(t *testing.T) {
	// Switch context to k3d-source
	out, err := TestHelper.Kubectl("", "config", "use-context", "k3d-source")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to switch context", "failed to switch context: %s\n%s", err, out)
	}
	// Apply the slow-cooker.yml manifest
	out, err = TestHelper.Kubectl("", "apply", "-f", "testdata/slow-cooker.yml", "--namespace=default")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to set up slow-cooker", "failed to set up target slow-cooker: %s\n%s", err, out)
	}
}
