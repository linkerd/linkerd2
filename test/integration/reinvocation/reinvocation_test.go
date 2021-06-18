package inject

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	v1 "k8s.io/api/core/v1"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

// TestReinvocation installs https://github.com/kubemod/kubemod, then creates a modrule that
// adds the "linkerd.io/proxy-log-level: debug" annotation through a mutating webhook that
// gets called after the linkerd injector. The latter should be reinvoked so it reacts to that
// annotation.
func TestReinvocation(t *testing.T) {
	// We're using a slightly reduced version of kubemod
	// - Has the test.linkerd.io/is-test-data-plane label
	// - Doesn't contain the validating admission controller
	// - The mutating admission controller was renamed to z-kubemod-mutating-webhook-configuration
	//   so it runs after the linkerd injector (they're run alphabetically)
	// - The command from the job generating the mwc cert and secret has been slightly changed in order
	//   to account for that renaming (see yaml)
	kubemodYAML, err := testutil.ReadFile("testdata/kubemod.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read kubemod.yaml", "failed to read kubemod.yaml: %s", err)
	}
	o, err := TestHelper.KubectlApply(kubemodYAML, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to install kubemod",
			"failed to install kubemod: %s\n%s", err, o)
	}

	ctx := context.Background()

	nsAnnotations := map[string]string{
		k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
	}
	TestHelper.WithDataPlaneNamespace(ctx, "reinvocation", nsAnnotations, t, func(t *testing.T, ns string) {
		modruleYAML, err := testutil.ReadFile("testdata/modrule.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to read modrule.yaml", "failed to read modrule.yaml: %s", err)
		}
		err = TestHelper.RetryFor(30*time.Second, func() error {
			o, err := TestHelper.KubectlApply(modruleYAML, ns)
			if err != nil {
				return fmt.Errorf("%s\n%s", err, o)
			}
			return nil
		})
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to apply modrule.yaml",
				"failed to apply modrule.yaml: %s", err)
		}

		podsYAML, err := testutil.ReadFile("testdata/inject_test.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to read inject test file",
				"failed to read inject test file: %s", err)
		}
		o, err = TestHelper.KubectlApply(podsYAML, ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to install inject test file",
				"failed to install inject test file: %s\n%s", err, o)
		}

		deployName := "inject-test-terminus"
		var pod *v1.Pod
		err = TestHelper.RetryFor(30*time.Second, func() error {
			pods, err := TestHelper.GetPodsForDeployment(ctx, ns, deployName)
			if err != nil {
				return fmt.Errorf("failed to get pods for namespace %s", ns)
			}

			for _, p := range pods {
				p := p //pin
				creator, ok := p.Annotations[k8s.CreatedByAnnotation]
				if ok && strings.Contains(creator, "proxy-injector") {
					pod = &p
					break
				}
			}
			if pod == nil {
				return fmt.Errorf("failed to find auto injected pod for deployment %s", deployName)
			}
			return nil
		})

		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to find autoinjected pod: ", err.Error())
		}

		injectionValidator := testutil.InjectValidator{
			// ****** TODO ****** this proofs the changes made by the z-kubemod mutating webhook
			// weren't surfaced by the injector. Once the injector implements reinvocation
			// the log level should be "debug"
			LogLevel: "warn,linkerd=info",
		}
		if err := injectionValidator.ValidatePod(&pod.Spec); err != nil {
			testutil.AnnotatedFatalf(t, "received unexpected output", "received unexpected output\n%s", err.Error())
		}
	})

	o, err = TestHelper.Kubectl(kubemodYAML, "delete", "-f", "-")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to uninstall kubemod",
			"failed to uninstall kubemod: %s\n%s", err, o)
	}
}
