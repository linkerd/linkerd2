package endpoints

import (
	"bytes"
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
	ns := TestHelper.GetLinkerdNamespace()
	vizNs := TestHelper.GetVizNamespace()
	testDataPath := "testdata"
	cmd := []string{
		"diagnostics",
		"endpoints",
		fmt.Sprintf("linkerd-dst.%s.svc.cluster.local:8086", ns),
		fmt.Sprintf("grafana.%s.svc.cluster.local:3000", vizNs),
		fmt.Sprintf("linkerd-identity.%s.svc.cluster.local:8080", ns),
		fmt.Sprintf("linkerd-proxy-injector.%s.svc.cluster.local:443", ns),
		fmt.Sprintf("tap.%s.svc.cluster.local:8088", vizNs),
		fmt.Sprintf("web.%s.svc.cluster.local:8084", vizNs),
	}

	if !TestHelper.ExternalPrometheus() {
		cmd = append(cmd, fmt.Sprintf("prometheus.%s.svc.cluster.local:9090", vizNs))
	} else {
		cmd = append(cmd, "prometheus.external-prometheus.svc.cluster.local:9090")
		testDataPath += "/external_prometheus"
	}

	cmd = append(cmd, "-ojson")
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	tpl := template.Must(template.ParseFiles(testDataPath + "/linkerd_endpoints.golden"))
	vars := struct {
		Ns    string
		VizNs string
	}{
		ns,
		vizNs,
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
