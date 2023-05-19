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
	// Block test execution until viz extension is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdVizDeployReplicas)
	os.Exit(m.Run())
}

type testCase struct {
	s string
	c int
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// TestRoutes exercises the "linkerd routes" command, validating the
// installation and output of ServiceProfiles for both the control-plane and
// smoke test.
func TestRoutes(t *testing.T) {
	// control-plane routes
	cmd := []string{"viz", "routes", "--namespace", TestHelper.GetLinkerdNamespace(), "deploy"}
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd routes' command failed", err)
	}

	routeStrings := []testCase{
		{"linkerd-destination", 1},
		{"linkerd-identity", 3},
		{"linkerd-proxy-injector", 2},
	}

	for _, r := range routeStrings {
		count := strings.Count(out, r.s)
		if count != r.c {
			testutil.AnnotatedFatalf(t, fmt.Sprintf("expected %d occurrences of \"%s\", got %d", r.c, r.s, count),
				"expected %d occurrences of \"%s\", got %d\n%s", r.c, r.s, count, out)
		}
	}

	// viz routes
	cmd = []string{"viz", "routes", "--namespace", TestHelper.GetVizNamespace(), "deploy"}
	out, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd routes' command failed", err)
	}

	vizRouteStrings := []testCase{
		{"metrics-api", 9},
		{"tap", 4},
		{"tap-injector", 2},
		{"web", 2},
	}

	if !TestHelper.ExternalPrometheus() {
		vizRouteStrings = append(vizRouteStrings, testCase{
			"prometheus",
			5,
		})
	}
	for _, r := range vizRouteStrings {
		count := strings.Count(out, r.s)
		if count != r.c {
			testutil.AnnotatedFatalf(t, fmt.Sprintf("expected %d occurrences of \"%s\", got %d", r.c, r.s, count),
				"expected %d occurrences of \"%s\", got %d\n%s", r.c, r.s, count, out)
		}
	}
}
