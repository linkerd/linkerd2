package serviceprofiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/testutil"
	cmd2 "github.com/linkerd/linkerd2/viz/cmd"
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
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "serviceprofile-test", map[string]string{}, t, func(t *testing.T, ns string) {
		t.Run("service profiles", testProfiles)
		t.Run("service profiles metrics", testMetrics)
	})
}

func testProfiles(t *testing.T) {
	ctx := context.Background()
	testNamespace := TestHelper.GetTestNamespace("serviceprofile-test")
	out, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/tap_application.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd inject' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, testNamespace)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// wait for deployments to start
	for _, deploy := range []string{"t1", "t2", "t3", "gateway"} {
		if err := TestHelper.CheckPods(ctx, testNamespace, deploy, 1); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
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
				testutil.AnnotatedFatalf(t, "'linkerd routes' command failed",
					"'linkerd routes' command failed: %s\n", err)
			}

			initialExpectedRoutes := []string{"[DEFAULT]"}

			assertExpectedRoutes(initialExpectedRoutes, routes, t)

			sourceFlag := fmt.Sprintf("--%s", tc.sourceName)
			cmd := []string{"profile", "--namespace", tc.namespace, tc.spName, sourceFlag}
			if tc.sourceName == "tap" {
				cmd = append([]string{"viz"}, cmd...)
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
			out, err := TestHelper.LinkerdRun(cmd...)
			if err != nil {
				testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd %s' command failed", cmd), err)
			}

			_, err = TestHelper.KubectlApply(out, tc.namespace)
			if err != nil {
				testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
					"'kubectl apply' command failed:\n%s", err)
			}

			routes, err = getRoutes(tc.deployName, tc.namespace, []string{})
			if err != nil {
				testutil.AnnotatedFatalf(t, "'linkerd routes' command failed",
					"'linkerd routes' command failed: %s\n", err)
			}

			assertExpectedRoutes(tc.expectedRoutes, routes, t)
		})
	}
}

func testMetrics(t *testing.T) {
	var (
		testNamespace        = TestHelper.GetTestNamespace("serviceprofile-test")
		testSP               = "world-svc"
		testDownstreamDeploy = "deployment/world"
		testUpstreamDeploy   = "deployment/hello"
		testYAML             = "testdata/hello_world.yaml"
	)

	out, err := TestHelper.LinkerdRun("inject", "--manual", testYAML)
	if err != nil {
		testutil.AnnotatedError(t, "'linkerd inject' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, testNamespace)
	if err != nil {
		testutil.AnnotatedErrorf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	cmd := []string{
		"profile",
		"--namespace",
		testNamespace,
		"--open-api",
		"testdata/world.swagger",
		testSP,
	}

	out, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedError(t, fmt.Sprintf("'linkerd %s' command failed", cmd), err)
	}

	_, err = TestHelper.KubectlApply(out, testNamespace)
	if err != nil {
		testutil.AnnotatedErrorf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", err)
	}

	assertRouteStat(testUpstreamDeploy, testNamespace, testDownstreamDeploy, t, func(stat *cmd2.JSONRouteStats) error {
		if !(*stat.ActualSuccess > 0.00 && *stat.ActualSuccess < 100.00) {
			return fmt.Errorf("expected Actual Success to be greater than 0%% and less than 100%% due to pre-seeded failure rate. But got %0.2f", *stat.ActualSuccess)
		}
		return nil
	})

	profile := &sp.ServiceProfile{}

	// Grab the output and convert it to a service profile object for modification
	err = yaml.Unmarshal([]byte(out), profile)
	if err != nil {
		testutil.AnnotatedErrorf(t, "unable to unmarshal YAML",
			"unable to unmarshal YAML: %s", err)
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
		testutil.AnnotatedErrorf(t, "error marshalling service profile",
			"error marshalling service profile: %s", bytes)
	}

	out, err = TestHelper.KubectlApply(string(bytes), testNamespace)
	if err != nil {
		testutil.AnnotatedErrorf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed:\n%s :%s", err, out)
	}

	assertRouteStat(testUpstreamDeploy, testNamespace, testDownstreamDeploy, t, func(stat *cmd2.JSONRouteStats) error {
		if *stat.EffectiveSuccess < 0.95 {
			return fmt.Errorf("expected Effective Success to be at least 95%% with retries enabled. But got %.2f", *stat.EffectiveSuccess)
		}
		return nil
	})
}

func assertRouteStat(upstream, namespace, downstream string, t *testing.T, assertFn func(stat *cmd2.JSONRouteStats) error) {
	const routePath = "GET /testpath"
	timeout := 2 * time.Minute
	err := TestHelper.RetryFor(timeout, func() error {
		routes, err := getRoutes(upstream, namespace, []string{"--to", downstream})
		if err != nil {
			return fmt.Errorf("'linkerd routes' command failed: %s", err)
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
		testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out asserting route stat (%s)", timeout), err)
	}
}

func assertExpectedRoutes(expected []string, actual []*cmd2.JSONRouteStats, t *testing.T) {

	if len(expected) != len(actual) {
		testutil.Errorf(t, "mismatch routes count. Expected %d, Actual %d", len(expected), len(actual))
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
			testutil.Errorf(t, "expected route %s not found in %+v", expectedRoute, sb.String())
		}
	}
}

func getRoutes(deployName, namespace string, additionalArgs []string) ([]*cmd2.JSONRouteStats, error) {
	cmd := []string{"viz", "routes", "--namespace", namespace, deployName}

	if len(additionalArgs) > 0 {
		cmd = append(cmd, additionalArgs...)
	}

	cmd = append(cmd, "--output", "json")
	var results map[string][]*cmd2.JSONRouteStats
	err := TestHelper.RetryFor(2*time.Minute, func() error {
		out, err := TestHelper.LinkerdRun(cmd...)
		if err != nil {
			return err
		}

		if err := yaml.Unmarshal([]byte(out), &results); err != nil {
			return err
		}

		if _, ok := results[deployName]; ok {
			return nil
		}

		keys := []string{}
		for k := range results {
			keys = append(keys, k)
		}
		return fmt.Errorf("could not retrieve route info for %s; found [%s]", deployName, strings.Join(keys, ", "))
	})
	if err != nil {
		return nil, err
	}
	return results[deployName], nil

}
