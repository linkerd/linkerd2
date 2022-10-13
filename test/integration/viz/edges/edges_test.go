package edges

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until viz extension is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdVizDeployReplicas)
	os.Exit(m.Run())
}

// TestEdges requires that there has been traffic recently between linkerd-web
// and linkerd-controller for edges to have been registered, which is the
// case when running this test in the context of the other integration tests.

func TestEdges(t *testing.T) {
	ns := TestHelper.GetLinkerdNamespace()
	promNs := TestHelper.GetVizNamespace()
	vars := struct {
		Ns     string
		PromNs string
	}{ns, promNs}

	var b bytes.Buffer
	tpl := template.Must(template.ParseFiles("testdata/linkerd_edges.golden"))
	if err := tpl.Execute(&b, vars); err != nil {
		t.Fatalf("failed to parse linkerd_edges.golden template: %s", err)
	}

	timeout := 50 * time.Second
	cmd := []string{
		"edges",
		"-n", ns,
		"deploy",
		"-ojson",
	}
	r := regexp.MustCompile(b.String())
	err := TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun(cmd...)
		if err != nil {
			t.Fatal(err)
		}
		pods, err := TestHelper.Kubectl("", []string{"get", "pods", "-A"}...)
		if err != nil {
			return err
		}
		if !r.MatchString(out) {
			t.Errorf("Expected output:\n%s\nactual:\n%s\nAll pods: %s", b.String(), out, pods)
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedError(t, fmt.Sprintf("timed-out checking edges (%s)", timeout), err)
	}
}

// TestDirectEdges deploys a terminus and then generates a load generator which
// sends traffic directly to the pod ip of the terminus pod.
func TestDirectEdges(t *testing.T) {

	ctx := context.Background()
	// setup
	TestHelper.WithDataPlaneNamespace(ctx, "direct-edges-test", map[string]string{}, t, func(t *testing.T, testNamespace string) {

		// inject terminus

		out, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/terminus.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd inject' command failed", "'linkerd inject' command failed: %s", err)
		}

		// deploy terminus

		out, err = TestHelper.KubectlApply(out, testNamespace)
		if err != nil {
			testutil.AnnotatedFatalf(t, "kubectl apply command failed", "kubectl apply command failed\n%s", out)
		}

		if err := TestHelper.CheckPods(ctx, testNamespace, "terminus", 1); err != nil {
			//nolint:errorlint
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}

		// get terminus pod ip

		ip, err := TestHelper.Kubectl("", "-n", testNamespace, "get", "pod", "-ojsonpath=\"{.items[*].status.podIP}\"")
		if err != nil {
			testutil.AnnotatedError(t, "'kubectl get pod' command failed", err)
		}
		ip = strings.Trim(ip, "\"") // strip quotes

		b, err := os.ReadFile("testdata/slow-cooker.yaml")
		if err != nil {
			testutil.AnnotatedError(t, "error reading file slow-cooker.yaml", err)
		}

		slowcooker := string(b)
		slowcooker = strings.ReplaceAll(slowcooker, "___TERMINUS_POD_IP___", ip)

		// inject slow cooker

		out, stderr, err := TestHelper.PipeToLinkerdRun(slowcooker, "inject", "--manual", "-")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd 'inject' command failed", "'linkerd %s' command failed with %s: %s\n", "inject", err.Error(), stderr)
		}

		// deploy slow cooker

		out, err = TestHelper.KubectlApply(out, testNamespace)
		if err != nil {
			testutil.AnnotatedFatalf(t, "kubectl apply command failed", "kubectl apply command failed\n%s", out)
		}

		if err := TestHelper.CheckPods(ctx, testNamespace, "slow-cooker", 1); err != nil {
			//nolint:errorlint
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}

		// check edges
		timeout := 50 * time.Second
		testDataPath := "testdata"
		err = TestHelper.RetryFor(timeout, func() error {
			out, err = TestHelper.LinkerdRun("-n", testNamespace, "-o", "json", "viz", "edges", "deploy")
			if err != nil {
				return err
			}

			tpl := template.Must(template.ParseFiles(testDataPath + "/direct_edges.golden"))
			vars := struct {
				Ns    string
				VizNs string
			}{
				testNamespace,
				TestHelper.GetVizNamespace(),
			}
			var buf bytes.Buffer
			if err := tpl.Execute(&buf, vars); err != nil {
				return fmt.Errorf("failed to parse direct_edges.golden template: %w", err)
			}

			pods, err := TestHelper.Kubectl("", []string{"get", "pods", "-A"}...)
			if err != nil {
				return err
			}

			r := regexp.MustCompile(buf.String())
			if !r.MatchString(out) {
				return fmt.Errorf("Expected output:\n%s\nactual:\n%s\nAll pods: %s", buf.String(), out, pods)
			}
			return nil
		})

		if err != nil {
			testutil.AnnotatedError(t, fmt.Sprintf("timed-out checking edges (%s)", timeout), err)
		}
	})

}
