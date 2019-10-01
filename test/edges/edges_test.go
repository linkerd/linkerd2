package edges

import (
	"bytes"
	"html/template"
	"os"
	"regexp"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

// TestEdges requires that there has been traffic recently between linkerd-web
// and linkerd-controller for edges to have been registered, which is the
// case when running this test in the context of the other integration tests.
func TestEdges(t *testing.T) {
	ns := TestHelper.GetLinkerdNamespace()
	cmd := []string{
		"edges",
		"-n", ns,
		"deploy",
		"-ojson",
	}
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("Unexpected error: %v\nError output: %s", err, stderr)
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
