package get

import (
	"os"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// TestRoutes exercises the "linkerd routes" command, validating the
// installation and output of ServiceProfiles for both the control-plane and
// smoke test.
func TestRoutes(t *testing.T) {
	// control-plane routes
	cmd := []string{"routes", "--namespace", TestHelper.GetLinkerdNamespace(), "deploy"}
	out, _, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("Routes command failed\n%s", out)
	}

	routeStrings := []struct {
		s string
		c int
	}{
		{"linkerd-controller-api", 9},
		{"linkerd-destination", 3},
		{"linkerd-grafana", 12},
		{"linkerd-identity", 2},
		{"linkerd-prometheus", 5},
		{"linkerd-web", 2},

		{"POST /api/v1/ListPods", 1},
		{"POST /api/v1/", 8},
		{"POST /io.linkerd.proxy.destination.Destination/Get", 2},
		{"GET /api/annotations", 1},
		{"GET /api/", 9},
		{"GET /public/", 3},
		{"GET /api/v1/", 3},
	}

	for _, r := range routeStrings {
		count := strings.Count(out, r.s)
		if count != r.c {
			t.Fatalf("Expected %d occurrences of \"%s\", got %d\n%s", r.c, r.s, count, out)
		}
	}

	// smoke test / bb routes
	prefixedNs := TestHelper.GetTestNamespace("smoke-test")
	cmd = []string{"routes", "--namespace", prefixedNs, "deploy"}
	golden := "routes.smoke.golden"

	out, _, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("Routes command failed\n%s", out)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err)
	}
}
