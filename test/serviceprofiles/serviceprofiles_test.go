package serviceprofiles

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

type testCase struct {
	namespace   string
	injectYAML  string
	deployments []string
	deployName  string
	spName      string
}

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	code := m.Run()
	out, err := TestHelper.Kubectl("delete", "ns", "emojivoto")
	if err != nil {
		os.Exit(code)
	}

	fmt.Println(out)

	out, err = TestHelper.Kubectl("delete", "ns", "booksapp")
	if err != nil {
		os.Exit(code)
	}
	fmt.Println(out)
	os.Exit(code)
}

func TestServiceProfilesFromTap(t *testing.T) {
	testCases := []testCase{
		{
			namespace:   "emojivoto",
			injectYAML:  "emojivoto.yml",
			deployments: []string{"emoji", "vote-bot", "voting", "web"},
			deployName:  "deploy/voting",
			spName:      "voting-svc",
		},
		{
			namespace:   "booksapp",
			injectYAML:  "booksapp.yml",
			deployments: []string{"webapp", "authors", "books", "traffic"},
			deployName:  "deploy/books",
			spName:      "books-svc",
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("service profiles from tap: %s", tc.namespace), func(t *testing.T) {
			cmd := []string{"inject", fmt.Sprintf("testdata/%s", tc.injectYAML)}
			out, stdout, err := TestHelper.LinkerdRun(cmd...)
			if err != nil {
				t.Fatalf("linkerd inject command failed: %s\n%s", err, out)
			}

			out, err = TestHelper.KubectlApply(out, tc.namespace)
			if err != nil {
				t.Fatalf("kubectl apply command failed:\n%s", out)
			}

			for _, deploy := range tc.deployments {
				err = TestHelper.CheckPods(tc.namespace, deploy, 1)
				if err != nil {
					t.Fatalf("Unexpected error: %s\n", err.Error())
				}
			}

			// run routes before: Expected default route only
			cmd = []string{"routes", "--namespace", tc.namespace, tc.deployName}
			out, _, err = TestHelper.LinkerdRun(cmd...)
			if err != nil {
				t.Fatalf("routes command failed: %s\n", err)
			}
			routes := parseRouteDetails(out)
			if len(routes) > 1 {
				t.Fatalf("Expected route details for service to be at-most 1 but got %d\n", len(routes))
			}

			// run service profile from tap command
			cmd = []string{"profile", "--namespace", tc.namespace, tc.spName, "--tap", tc.deployName, "--tap-route-limit", "5", "--tap-duration", "10s"}
			out, stdout, err = TestHelper.LinkerdRun(cmd...)
			if err != nil {
				t.Fatalf("profile command failed: %s\n%s\n", err.Error(), stdout)
			}

			out, err = TestHelper.KubectlApply(out, tc.namespace)
			if err != nil {
				t.Fatalf("kubectl apply command failed:\n%s", out)
			}

			// run routes: Expected more than default route
			cmd = []string{"routes", "--namespace", tc.namespace, tc.deployName}
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
		})
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
