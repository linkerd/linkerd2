package get

import (
	"fmt"
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
	os.Exit(testutil.Run(m, TestHelper))
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
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd routes' command failed", err)
	}

	routeStrings := []struct {
		s string
		c int
	}{
		{"linkerd-controller-api", 7},
		{"linkerd-destination", 1},
		{"linkerd-dst", 6},
		{"linkerd-dst-headless", 3},
		{"linkerd-grafana", 13},
		{"linkerd-identity", 3},
		{"linkerd-identity-headless", 1},
		{"linkerd-prometheus", 5},
		{"linkerd-web", 2},

		{"POST /api/v1/ListPods", 1},
		{"POST /api/v1/", 7},
		{"POST /io.linkerd.proxy.destination.Destination/Get", 4},
		{"GET /api/annotations", 1},
		{"GET /api/", 9},
		{"GET /public/", 3},
		{"GET /api/v1/", 2},
	}

	for _, r := range routeStrings {
		count := strings.Count(out, r.s)
		if count != r.c {
			testutil.AnnotatedFatalf(t, fmt.Sprintf("expected %d occurrences of \"%s\", got %d", r.c, r.s, count),
				"expected %d occurrences of \"%s\", got %d\n%s", r.c, r.s, count, out)
		}
	}

	// smoke test / bb routes
	prefixedNs := TestHelper.GetTestNamespace("smoke-test")
	cmd = []string{"routes", "--namespace", prefixedNs, "deploy"}
	golden := "routes.smoke.golden"

	out, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd routes' command failed", err)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		testutil.AnnotatedFatalf(t, "received unexpected output", "received unexpected output\n%s", err)
	}
}
