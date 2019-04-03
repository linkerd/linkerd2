package serviceprofiles

import (
	"os"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

// Assumptions:
// Linkerd control plane already installed
// Linkerd helper is aware of control plane

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

func TestServiceProfilesFromTap(t *testing.T) {
	//install emojivoto
	cmd := []string{"inject", "testdata/emojivoto.yml"}
	out, _, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("linkerd inject command failed: %s\n%s", err, out)
	}

	out, err = TestHelper.KubectlApply(out, "emojivoto")
	if err != nil {
		t.Fatalf("kubectl apply command failed:\n%s", out)
	}

	// confirm we've installed emojivoto
	for _, deploy := range []string{"emoji", "vote-bot", "voting", "web"} {
		err = TestHelper.CheckPods("emojivoto", deploy, 1)
		if err != nil {
			t.Fatalf("Unexpected error: %s\n", err.Error())
		}
	}

	// run routes before: Expected Default route only
	cmd = []string{"routes", "--namespace", "emojivoto", "deploy/voting"}
	out, _, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("routes command failed: %s\n", err)
	}
	routes := parseRouteDetails(out)
	if len(routes) > 1 {
		t.Fatalf("Expected route details for service to be at-most 1 but got %d\n", len(routes))
	}

	// run service profile from tap command
	cmd = []string{"profile", "--namespace", "emojivoto", "voting-svc", "--tap", "deploy/voting", "--tap-route-limit", "5"}
	out, _, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("profile command failed: %s\n", err.Error())
	}

	out, err = TestHelper.KubectlApply(out, "emojivoto")
	if err != nil {
		t.Fatalf("kubectl apply command failed:\n%s", out)
	}

	// run routes: Expected more than default route
	cmd = []string{"routes", "--namespace", "emojivoto", "deploy/voting"}
	out, rep, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("routes command failed: %s\n", err.Error())
	}

	routes = parseRouteDetails(rep)
	for _, route := range routes {
		if len(route) <= 1 {
			t.Fatalf("Expected route details for service to be at-most 1 but got %d\n", len(route))
		}
	}
}

func parseRouteDetails(cliOutput string) []string {
	var cliLines []string
	routesByDeployment := strings.SplitAfter(cliOutput, "\n") //FIXME use regular split
	for _, routes := range routesByDeployment {
		routes = strings.TrimSpace(routes)
		if routes != "" && !strings.HasPrefix(routes, "ROUTE") {
			cliLines = append(cliLines, routes)
		}

	}
	return cliLines
}
