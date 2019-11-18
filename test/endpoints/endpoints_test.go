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
	cmd := []string{
		"endpoints",
		fmt.Sprintf("linkerd-controller-api.%s.svc.cluster.local:8085", ns),
		fmt.Sprintf("linkerd-dst.%s.svc.cluster.local:8086", ns),
		fmt.Sprintf("linkerd-grafana.%s.svc.cluster.local:3000", ns),
		fmt.Sprintf("linkerd-identity.%s.svc.cluster.local:8080", ns),
		fmt.Sprintf("linkerd-prometheus.%s.svc.cluster.local:9090", ns),
		fmt.Sprintf("linkerd-proxy-injector.%s.svc.cluster.local:443", ns),
		fmt.Sprintf("linkerd-sp-validator.%s.svc.cluster.local:443", ns),
		fmt.Sprintf("linkerd-tap.%s.svc.cluster.local:8088", ns),
		fmt.Sprintf("linkerd-web.%s.svc.cluster.local:8084", ns),
		"-ojson",
	}
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("Unexpected error: %v\nError output: %s", err, stderr)
	}

	tpl := template.Must(template.ParseFiles("testdata/linkerd_endpoints.golden"))
	vars := struct{ Ns string }{ns}
	var b bytes.Buffer
	if err := tpl.Execute(&b, vars); err != nil {
		t.Fatalf("failed to parse linkerd_endpoints.golden template: %s", err)
	}

	r := regexp.MustCompile(b.String())
	if !r.MatchString(out) {
		t.Errorf("Expected output:\n%s\nactual:\n%s", b.String(), out)
	}
}

// TODO: when #3004 gets fixed, add a negative test for mismatched ports
func TestBadEndpoints(t *testing.T) {
	_, stderr, err := TestHelper.LinkerdRun("endpoints", "foo")
	if err == nil {
		t.Fatalf("was expecting an error: %v", err)
	}
	stderrOut := strings.Split(stderr, "\n")
	if len(stderrOut) == 0 {
		t.Fatalf("unexpected output: %s", stderr)
	}
	if stderrOut[0] != "Error: Destination API error: Invalid authority: foo" {
		t.Errorf("unexpected error string: %s", stderrOut[0])
	}
}
