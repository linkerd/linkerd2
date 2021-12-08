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
	nginxNs := "linkerd-endpoints-test"
	controlNs := TestHelper.GetLinkerdNamespace()
	testDataPath := "testdata"
	cmd := []string{
		"diagnostics",
		"endpoints",
		fmt.Sprintf("linkerd-dst.%s.svc.cluster.local:8086", controlNs),
		fmt.Sprintf("linkerd-identity.%s.svc.cluster.local:8080", controlNs),
		fmt.Sprintf("linkerd-proxy-injector.%s.svc.cluster.local:443", controlNs),
	}

	if !TestHelper.ExternalPrometheus() {
		cmd = append(cmd, fmt.Sprintf("nginx.%s.svc.cluster.local:8080", nginxNs))
		cmd = append(cmd, "-ojson")
		TestHelper.WithDataPlaneNamespace(ctx, "endpoints-test", map[string]string{}, t, func(t *testing.T, ns string) {
			nginx, err := TestHelper.LinkerdRun("inject", "testdata/nginx.yaml")
			if err != nil {
				testutil.AnnotatedFatal(t, "unexpected error", err)
			}

			out, err := TestHelper.KubectlApply(nginx, ns)
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

		})
	} else {
		cmd = append(cmd, "prometheus.external-prometheus.svc.cluster.local:9090")
		cmd = append(cmd, "-ojson")
		testDataPath += "/external_prometheus"
		checkEndpoints(t, cmd, testDataPath, controlNs, "external-prometheus")
	}

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

func checkEndpoints(t *testing.T, cmd []string, testDataPath, controlNs, endpointNs string) {
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	tpl := template.Must(template.ParseFiles(testDataPath + "/linkerd_endpoints.golden"))
	vars := struct {
		Ns         string
		EndpointNs string
	}{
		controlNs,
		endpointNs,
	}
	var b bytes.Buffer
	if err := tpl.Execute(&b, vars); err != nil {
		testutil.AnnotatedFatalf(t, "failed to parse linkerd_endpoints.golden template", "failed to parse linkerd_endpoints.golden template: %s", err)
	}

	r := regexp.MustCompile(b.String())
	if !r.MatchString(out) {
		testutil.AnnotatedErrorf(t, "unexpected output", "expected output:\n%s\nactual:\n%s", b.String(), out)
	}
}
