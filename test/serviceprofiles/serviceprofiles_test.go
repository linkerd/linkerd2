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
	args           []string
	deployName     string
	expectedRoutes []string
	namespace      string
	sourceName     string
	spName         string
}

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

func TestServiceProfiles(t *testing.T) {

	testNamespace := TestHelper.GetTestNamespace("serviceprofile-test")
	out, _, err := TestHelper.LinkerdRun("inject", "testdata/tap_application.yaml")
	if err != nil {
		t.Fatalf("linkerd inject command failed\n%s", out)
	}

	out, err = TestHelper.KubectlApply(out, testNamespace)
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}

	// wait for deployments to start
	for _, deploy := range []string{"t1", "t2", "t3", "gateway"} {
		if err := TestHelper.CheckPods(testNamespace, deploy, 1); err != nil {
			t.Error(err)
		}

		if err := TestHelper.CheckDeployment(testNamespace, deploy, 1); err != nil {
			t.Error(fmt.Errorf("Error validating deployment [%s]:\n%s", deploy, err))
		}
	}

	testCases := []testCase{
		{
			sourceName:     "tap",
			namespace:      testNamespace,
			deployName:     "deploy/t1",
			spName:         "t1-svc",
			expectedRoutes: []string{"POST /buoyantio.bb.TheService/theFunction", "[DEFAULT]"},
		},
		{
			sourceName:     "open-api",
			namespace:      testNamespace,
			spName:         "t3-svc",
			deployName:     "deploy/t3",
			expectedRoutes: []string{"DELETE /testpath", "GET /testpath", "PATCH /testpath", "POST /testpath", "[DEFAULT]"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.sourceName, func(t *testing.T) {
			routes, err := getRoutes(tc.deployName, tc.namespace)
			if err != nil {
				t.Fatalf("routes command failed: %s\n", err)
			}

			initialExpectedRoutes := []string{"[DEFAULT]"}

			if !assertExpectedRoutes(initialExpectedRoutes, routes) {
				t.Fatalf("Expected routes to have prefixes:\n%s\nbut got:\n%s",
					strings.Join(initialExpectedRoutes, "\n"),
					strings.Join(routes, "\n"),
				)
			}

			sourceFlag := fmt.Sprintf("--%s", tc.sourceName)
			cmd := []string{"profile", "--namespace", tc.namespace, tc.spName, sourceFlag}
			if tc.sourceName == "tap" {
				tc.args = []string{
					tc.deployName,
					"--tap-route-limit",
					"5",
					"--tap-duration",
					"10s",
				}
			}

			if tc.sourceName == "open-api" {
				tc.args = []string{
					"testdata/t3.swagger",
				}
			}

			cmd = append(cmd, tc.args...)
			out, _, err := TestHelper.LinkerdRun(cmd...)
			if err != nil {
				t.Fatalf("profile command failed: %s\n", err.Error())
			}

			_, err = TestHelper.KubectlApply(out, tc.namespace)
			if err != nil {
				t.Fatalf("kubectl apply command failed:\n%s", err)
			}

			routes, err = getRoutes(tc.deployName, tc.namespace)
			if err != nil {
				t.Fatalf("routes command failed: %s\n", err)
			}

			if !assertExpectedRoutes(tc.expectedRoutes, routes) {
				t.Fatalf("Expected routes to have prefixes:\n%s\nbut got:\n%s",
					strings.Join(tc.expectedRoutes, "\n"),
					strings.Join(routes, "\n"),
				)
			}
		})
	}
}

func assertExpectedRoutes(expected, actual []string) bool {

	if len(expected) != len(actual) {
		return false
	}

	for _, expectedRoute := range expected {
		containsRoute := false
		for _, actualRoute := range actual {
			if strings.HasPrefix(actualRoute, expectedRoute) {
				containsRoute = true
			}
		}
		if !containsRoute {
			return false
		}
	}
	return true
}

func getRoutes(deployName, namespace string) ([]string, error) {
	cmd := []string{"routes", "--namespace", namespace, deployName}
	out, _, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		return nil, err
	}
	routes := parseRouteDetails(out)
	return routes, nil
}

func parseRouteDetails(cliOutput string) []string {
	var cliLines []string
	routesByDeployment := strings.SplitAfter(cliOutput, "\n")
	for _, routes := range routesByDeployment {
		routes = strings.TrimSpace(routes)
		if routes != "" && !strings.HasPrefix(routes, "ROUTE") {
			cliLines = append(cliLines, routes)
		}

	}
	return cliLines
}
