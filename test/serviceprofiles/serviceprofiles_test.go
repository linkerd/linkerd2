package serviceprofiles

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	cmd2 "github.com/linkerd/linkerd2/cli/cmd"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/testutil"
	"sigs.k8s.io/yaml"
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
	err := TestHelper.CreateDataPlaneNamespaceIfNotExists(testNamespace, nil)
	if err != nil {
		t.Fatalf("failed to create %s namespace: %s", testNamespace, err)
	}
	out, stderr, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/tap_application.yaml")
	if err != nil {
		t.Fatalf("'linkerd %s' command failed with %s: %s\n", "inject", err.Error(), stderr)
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
			deployName:     "deployment/t1",
			spName:         "t1-svc",
			expectedRoutes: []string{"POST /buoyantio.bb.TheService/theFunction", "[DEFAULT]"},
		},
		{
			sourceName:     "open-api",
			namespace:      testNamespace,
			spName:         "t3-svc",
			deployName:     "deployment/t3",
			expectedRoutes: []string{"DELETE /testpath", "GET /testpath", "PATCH /testpath", "POST /testpath", "[DEFAULT]"},
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.sourceName, func(t *testing.T) {
			routes, err := getRoutes(tc.deployName, tc.namespace, []string{})
			if err != nil {
				t.Fatalf("routes command failed: %s\n", err)
			}

			initialExpectedRoutes := []string{"[DEFAULT]"}

			assertExpectedRoutes(initialExpectedRoutes, routes, t)

			sourceFlag := fmt.Sprintf("--%s", tc.sourceName)
			cmd := []string{"profile", "--namespace", tc.namespace, tc.spName, sourceFlag}
			if tc.sourceName == "tap" {
				tc.args = []string{
					tc.deployName,
					"--tap-route-limit",
					"1",
					"--tap-duration",
					"25s",
				}
			}

			if tc.sourceName == "open-api" {
				tc.args = []string{
					"testdata/t3.swagger",
				}
			}

			cmd = append(cmd, tc.args...)
			out, stderr, err := TestHelper.LinkerdRun(cmd...)
			if err != nil {
				t.Fatalf("'linkerd %s' command failed with %s: %s\n", cmd, err.Error(), stderr)
			}

			_, err = TestHelper.KubectlApply(out, tc.namespace)
			if err != nil {
				t.Fatalf("kubectl apply command failed:\n%s", err)
			}

			routes, err = getRoutes(tc.deployName, tc.namespace, []string{})
			if err != nil {
				t.Fatalf("routes command failed: %s\n", err)
			}

			assertExpectedRoutes(tc.expectedRoutes, routes, t)
		})
	}
}

func TestServiceProfileMetrics(t *testing.T) {
	var (
		testNamespace        = TestHelper.GetTestNamespace("serviceprofile-test")
		testSP               = "world-svc"
		testDownstreamDeploy = "deployment/world"
		testUpstreamDeploy   = "deployment/hello"
		testYAML             = "testdata/hello_world.yaml"
	)

	out, stderr, err := TestHelper.LinkerdRun("inject", "--manual", testYAML)
	if err != nil {
		t.Errorf("'linkerd %s' command failed with %s: %s\n", "inject", err.Error(), stderr)
	}

	out, err = TestHelper.KubectlApply(out, testNamespace)
	if err != nil {
		t.Errorf("kubectl apply command failed\n%s", out)
	}

	cmd := []string{
		"profile",
		"--namespace",
		testNamespace,
		"--open-api",
		"testdata/world.swagger",
		testSP,
	}

	out, stderr, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Errorf("'linkerd %s' command failed with %s: %s\n", cmd, err.Error(), stderr)
	}

	_, err = TestHelper.KubectlApply(out, testNamespace)
	if err != nil {
		t.Errorf("kubectl apply command failed:\n%s", err)
	}

	assertRouteStat(testUpstreamDeploy, testNamespace, testDownstreamDeploy, t, func(stat *cmd2.JSONRouteStats) error {
		if *stat.ActualSuccess == 100.00 {
			return fmt.Errorf("expected Actual Success to be less than 100%% due to pre-seeded failure rate. But got %0.2f", *stat.ActualSuccess)
		}
		return nil
	})

	profile := &sp.ServiceProfile{}

	// Grab the output and convert it to a service profile object for modification
	err = yaml.Unmarshal([]byte(out), profile)
	if err != nil {
		t.Errorf("unable to unmarshall YAML: %s", err.Error())
	}

	// introduce retry in the service profile
	for _, route := range profile.Spec.Routes {
		if route.Name == "GET /testpath" {
			route.IsRetryable = true
			break
		}
	}

	bytes, err := yaml.Marshal(profile)
	if err != nil {
		t.Errorf("error marshalling service profile: %s", bytes)
	}

	out, err = TestHelper.KubectlApply(string(bytes), testNamespace)
	if err != nil {
		t.Errorf("kubectl apply command failed:\n%s :%s", err, out)
	}

	assertRouteStat(testUpstreamDeploy, testNamespace, testDownstreamDeploy, t, func(stat *cmd2.JSONRouteStats) error {
		if *stat.EffectiveSuccess < 0.95 {
			return fmt.Errorf("expected Effective Success to be at least 95%% with retries enabled. But got %.2f", *stat.ActualSuccess)
		}
		return nil
	})
}

func assertRouteStat(upstream, namespace, downstream string, t *testing.T, assertFn func(stat *cmd2.JSONRouteStats) error) {
	const routePath = "GET /testpath"
	err := TestHelper.RetryFor(2*time.Minute, func() error {
		routes, err := getRoutes(upstream, namespace, []string{"--to", downstream})
		if err != nil {
			return fmt.Errorf("routes command failed: %s", err)
		}

		var testRoute *cmd2.JSONRouteStats
		assertExpectedRoutes([]string{routePath, "[DEFAULT]"}, routes, t)

		for _, route := range routes {
			if route.Route == routePath {
				testRoute = route
			}
		}

		if testRoute == nil {
			return errors.New("expected test route not to be nil")
		}

		return assertFn(testRoute)
	})

	if err != nil {
		t.Error(err.Error())
	}
}

func assertExpectedRoutes(expected []string, actual []*cmd2.JSONRouteStats, t *testing.T) {

	if len(expected) != len(actual) {
		t.Errorf("mismatch routes count. Expected %d, Actual %d", len(expected), len(actual))
	}

	for _, expectedRoute := range expected {
		containsRoute := false
		for _, actualRoute := range actual {
			if actualRoute.Route == expectedRoute {
				containsRoute = true
				break
			}
		}
		if !containsRoute {
			sb := strings.Builder{}
			for _, route := range actual {
				sb.WriteString(fmt.Sprintf("%s ", route.Route))
			}
			t.Errorf("Expected route %s not found in %+v", expectedRoute, sb.String())
		}
	}
}

func getRoutes(deployName, namespace string, additionalArgs []string) ([]*cmd2.JSONRouteStats, error) {
	cmd := []string{"routes", "--namespace", namespace, deployName}

	if len(additionalArgs) > 0 {
		cmd = append(cmd, additionalArgs...)
	}

	cmd = append(cmd, "--output", "json")
	var out, stderr string
	err := TestHelper.RetryFor(2*time.Minute, func() error {
		var err error
		out, stderr, err = TestHelper.LinkerdRun(cmd...)
		return err
	})
	if err != nil {
		return nil, err
	}

	var list map[string][]*cmd2.JSONRouteStats
	err = yaml.Unmarshal([]byte(out), &list)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("Error: %s stderr: %s", err.Error(), stderr))
	}

	if deployment, ok := list[deployName]; ok {
		return deployment, nil
	}
	return nil, fmt.Errorf("could not retrieve route info for %s", deployName)
}
