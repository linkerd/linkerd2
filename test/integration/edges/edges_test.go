package edges

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
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
	os.Exit(testutil.Run(m, TestHelper))
}

// TestEdges requires that there has been traffic recently between linkerd-web
// and linkerd-controller for edges to have been registered, which is the
// case when running this test in the context of the other integration tests.

// This test has been disabled because it can fail due to
// https://github.com/linkerd/linkerd2/issues/3706
// This test should be updated and re-enabled when that issue is addressed.
/*
func TestEdges(t *testing.T) {
	ns := TestHelper.GetLinkerdNamespace()
	cmd := []string{
		"edges",
		"-n", ns,
		"deploy",
		"-ojson",
	}
	out, err := TestHelper.LinkerdRunOk(cmd...)
	if err != nil {
		t.Fatalf("%s", err)
	}

	tpl := template.Must(template.ParseFiles("testdata/linkerd_edges.golden"))
	vars := struct{ Ns string }{ns}
	var b bytes.Buffer
	if err := tpl.Execute(&b, vars); err != nil {
		t.Fatalf("failed to parse linkerd_edges.golden template: %s", err)
	}

	r := regexp.MustCompile(b.String())
	if !r.MatchString(out) {
		t.Errorf("Expected output:\n%s\nactual:\n%s", b.String(), out)
	}
}
*/

// TestDirectEdges deploys a terminus and then generates a load generator which
// sends traffic directly to the pod ip of the terminus pod.
func TestDirectEdges(t *testing.T) {

	ctx := context.Background()
	// setup

	testNamespace := TestHelper.GetTestNamespace("direct-edges-test")
	err := TestHelper.CreateDataPlaneNamespaceIfNotExists(ctx, testNamespace, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create namespace", "failed to create %s namespace: %s", testNamespace, err)
	}

	// inject terminus

	out, err := TestHelper.LinkerdRunOk("inject", "--manual", "testdata/terminus.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd inject' command failed", "%s", err)
	}

	// deploy terminus

	out, err = TestHelper.KubectlApply(out, testNamespace)
	if err != nil {
		testutil.AnnotatedFatalf(t, "kubectl apply command failed", "kubectl apply command failed\n%s", out)
	}

	if err := TestHelper.CheckPods(ctx, testNamespace, "terminus", 1); err != nil {
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}

	if err := TestHelper.CheckDeployment(ctx, testNamespace, "terminus", 1); err != nil {
		testutil.AnnotatedErrorf(t, "CheckDeployment timed-out", "Error validating deployment [%s]:\n%s", "terminus", err)
	}

	// get terminus pod ip

	ip, err := TestHelper.Kubectl("", "-n", testNamespace, "get", "pod", "-ojsonpath=\"{.items[*].status.podIP}\"")
	if err != nil {
		testutil.AnnotatedError(t, "'kubectl get pod' command failed", err)
	}
	ip = strings.Trim(ip, "\"") // strip quotes

	b, err := ioutil.ReadFile("testdata/slow-cooker.yaml")
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
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}

	if err := TestHelper.CheckDeployment(ctx, testNamespace, "slow-cooker", 1); err != nil {
		testutil.AnnotatedErrorf(t, "CheckDeployment timed-out", "error validating deployment [%s]:\n%s", "terminus", err)
	}

	// check edges
	timeout := 50 * time.Second
	err = TestHelper.RetryFor(timeout, func() error {
		out, err = TestHelper.LinkerdRunOk("-n", testNamespace, "-o", "json", "edges", "deploy")
		if err != nil {
			return fmt.Errorf("linkerd %s command failed with %s: %s", "edges", err, stderr)
		}

		tpl := template.Must(template.ParseFiles("testdata/direct_edges.golden"))
		vars := struct {
			Ns        string
			ControlNs string
		}{
			testNamespace,
			TestHelper.GetLinkerdNamespace(),
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, vars); err != nil {
			return fmt.Errorf("failed to parse direct_edges.golden template: %s", err)
		}

		r := regexp.MustCompile(buf.String())
		if !r.MatchString(out) {
			return fmt.Errorf("Expected output:\n%s\nactual:\n%s", buf.String(), out)
		}
		return nil
	})

	if err != nil {
		testutil.AnnotatedError(t, fmt.Sprintf("timed-out checking edges (%s)", timeout), err)
	}
}
