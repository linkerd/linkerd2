package serviceprofiles

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cloudflare/cfssl/log"
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
	tearDown()
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
			routes, err := getRoutes(tc.deployName, tc.namespace, TestHelper)
			if err != nil {
				t.Fatalf("routes command failed: %s\n", err)
			}

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
			routes, err = getRoutes(tc.deployName, tc.namespace, TestHelper)
			if err != nil {
				t.Fatalf("routes command failed: %s\n", err.Error())
			}
			for _, route := range routes {
				if len(route) <= 1 {
					t.Fatalf("Expected route details for service to be greater than or equal to 1 but got %d\n", len(route))
				}
			}
		})
	}
}

func TestServiceProfilesFromSwagger(t *testing.T) {
	// Check that authors only has one route
	routes, err := getRoutes("deploy/authors", "booksapp", TestHelper)
	if err != nil {
		t.Fatalf("routes command failed: %s\n", err)
	}

	if len(routes) > 1 {
		t.Fatalf("Expected route details for service to be at-most 1 but got %d\n", len(routes))
	}
	// apply swagger profile
	cmd := []string{"profile", "--namespace", "booksapp", "authors", "--open-api", "testdata/authors.swagger"}
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("profile command failed: %s\n%s\n", err.Error(), stderr)
	}

	out, err = TestHelper.KubectlApply(out, "booksapp")
	if err != nil {
		t.Fatalf("kubectl apply command failed:\n%s", err)
	}

	// check that authors now has more than one route
	routes, err = getRoutes("deploy/authors", "booksapp", TestHelper)
	if err != nil {
		t.Fatalf("routes command failed: %s\n", err)
	}

	if len(routes) <= 1 {
		t.Fatalf("Expected route details for service to be greater than 1 but got %d\n", len(routes))
	}

}

func TestServiceProfilesFromProto(t *testing.T) {
	// Check that authors only has one route
	routes, err := getRoutes("deploy/emoji", "emojivoto", TestHelper)
	if err != nil {
		t.Fatalf("routes command failed: %s\n", err)
	}

	if len(routes) > 1 {
		t.Fatalf("Expected route details for service to be at-most 1 but got %d\n", len(routes))
	}

	// apply proto profile
	cmd := []string{"profile", "--namespace", "emojivoto", "emoji-svc", "--proto", "testdata/Emoji.proto"}
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("profile command failed: %s\n%s\n", err.Error(), stderr)
	}

	out, err = TestHelper.KubectlApply(out, "emojivoto")
	if err != nil {
		t.Fatalf("kubectl apply command failed:\n%s", err)
	}

	expectedRoutes := []string{
		"FindByShortcode",
		"ListAll",
		"[DEFAULT]",
	}

	// check that authors now has more than one route
	routes, err = getRoutes("deploy/emoji", "emojivoto", TestHelper)
	if err != nil {
		t.Fatalf("routes command failed: %s\n", err)
	}

	if !assertExpectedRoutes(expectedRoutes, routes) {
		t.Fatalf("Unexepected routes: Expected\n %s\nbut got:\n%s\n",
			strings.Join(expectedRoutes, "\n"), strings.Join(routes, "\n"))
	}
}

func assertExpectedRoutes(expected, actual []string) bool {
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

func getRoutes(deployName, namespace string, helper *testutil.TestHelper) ([]string, error) {
	cmd := []string{"routes", "--namespace", namespace, deployName}
	out, stderr, err := helper.LinkerdRun(cmd...)
	if err != nil {
		log.Infof("error getting routes: %s\n", stderr)
		return nil, err
	}
	routes := parseRouteDetails(out)
	return routes, nil
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

func tearDown() {
	out, err := TestHelper.Kubectl("delete", "ns", "emojivoto")
	if err != nil {
		log.Errorf("Unexpected error occurred: %s\n", out)
	}

	out, err = TestHelper.Kubectl("delete", "ns", "booksapp")
	if err != nil {
		log.Errorf("Unexpected error occurred: %s\n", out)
	}

}
