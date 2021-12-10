package endpoints

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

func TestGoodEndpoints(t *testing.T) {
	ctx := context.Background()
	controlNs := TestHelper.GetLinkerdNamespace()
	testDataPath := "testdata"
	cmd := []string{
		"diagnostics",
		"endpoints",
		fmt.Sprintf("linkerd-dst.%s.svc.cluster.local:8086", controlNs),
		fmt.Sprintf("linkerd-identity.%s.svc.cluster.local:8080", controlNs),
		fmt.Sprintf("linkerd-proxy-injector.%s.svc.cluster.local:443", controlNs),
		fmt.Sprintf("nginx.%s.svc.cluster.local:8080", "linkerd-endpoints-test"),
		"-ojson",
	}

	TestHelper.WithDataPlaneNamespace(ctx, "endpoints-test", map[string]string{}, t, func(t *testing.T, ns string) {
		out, err := TestHelper.Kubectl("", "apply", "-f", fmt.Sprintf("%s/%s", testDataPath, "nginx.yaml"), "-n", ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}

		err = TestHelper.CheckPods(ctx, ns, "nginx", 1)
		if err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}

		out, err = TestHelper.LinkerdRun(cmd...)
		if err != nil {
			testutil.AnnotatedFatal(t, "unexpected error", err)
		}

		tpl := template.Must(template.ParseFiles(testDataPath + "/linkerd_endpoints.golden"))
		vars := struct {
			Ns         string
			EndpointNs string
		}{
			controlNs,
			ns,
		}
		var b bytes.Buffer
		if err := tpl.Execute(&b, vars); err != nil {
			testutil.AnnotatedFatalf(t, "failed to parse linkerd_endpoints.golden template", "failed to parse linkerd_endpoints.golden template: %s", err)
		}

		r := regexp.MustCompile(b.String())
		if !r.MatchString(out) {
			testutil.AnnotatedErrorf(t, "unexpected output", "expected output:\n%s\nactual:\n%s", b.String(), out)
		}
	})
}

// TODO: when #3004 gets fixed, add a negative test for mismatched ports
func TestBadEndpoints(t *testing.T) {
	_, stderr, err := TestHelper.PipeToLinkerdRun("", "diagnostics", "endpoints", "foo")
	if err == nil {
		testutil.AnnotatedFatalf(t, "was expecting an error", "was expecting an error: %v", err)
	}
	stderrOut := strings.Split(stderr, "\n")
	if len(stderrOut) == 0 {
		testutil.AnnotatedFatalf(t, "unexpected output", "unexpected output: %s", stderr)
	}
	if stderrOut[0] != "Destination API error: Invalid authority: foo" {
		testutil.AnnotatedErrorf(t, "unexpected error string", "unexpected error string: %s", stderrOut[0])
	}
}
