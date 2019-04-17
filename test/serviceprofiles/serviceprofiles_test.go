package serviceprofiles

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/testutil"
	"sigs.k8s.io/yaml"
)

var TestHelper *testutil.TestHelper

type rowStat struct {
	Route            string  `json:"route"`
	EffectiveSuccess float64 `json:"effective_success"`
	ActualSuccess    float64 `json:"actual_success"`
}

type testCase struct {
	args           []string
	deployName     string
	expectedRoutes []string
	namespace      string
	sourceName     string
	spName         string
}

type routeStatAssertion struct {
	upstream   string
	downstream string
	namespace  string
	assertFunc func(stat *rowStat) error
}

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

func TestServiceProfiles(t *testing.T) {

	testNamespace := TestHelper.GetTestNamespace("serviceprofile-test")
	out, stderr, err := TestHelper.LinkerdRun("inject", "testdata/tap_application.yaml")
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
			routes, err := getRoutes(tc.deployName, tc.namespace, false, []string{})
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

			routes, err = getRoutes(tc.deployName, tc.namespace, false, []string{})
			if err != nil {
				t.Fatalf("routes command failed: %s\n", err)
			}

			assertExpectedRoutes(tc.expectedRoutes, routes, t)
		})
	}
}

func TestServiceProfileMetrics(t *testing.T) {

	testNamespace := TestHelper.GetTestNamespace("serviceprofile-test")
	testCases := []string{
		"retries",
		"latency",
	}

	for _, tc := range testCases {
		var (
			tc                   = tc
			testSP               = fmt.Sprintf("world-%s-svc", tc)
			testDownstreamDeploy = fmt.Sprintf("deployment/world-%s", tc)
			testUpstreamDeploy   = fmt.Sprintf("deployment/hello-%s", tc)
			testYAML             = fmt.Sprintf("testdata/hello_world_%s.yaml", tc)
		)

		t.Run(tc, func(t *testing.T) {
			out, stderr, err := TestHelper.LinkerdRun("inject", testYAML)
			if err != nil {
				t.Errorf("'linkerd %s' command failed with %s: %s\n", "inject", err.Error(), stderr)
			}

			out, err = TestHelper.KubectlApply(out, testNamespace)
			if err != nil {
				t.Errorf("kubectl apply command failed\n%s", out)
			}

			assertion := &routeStatAssertion{
				upstream:   testUpstreamDeploy,
				downstream: testDownstreamDeploy,
				namespace:  testNamespace,
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

			assertRouteStat(assertion, t, func(stat *rowStat) error {
				if stat.EffectiveSuccess != stat.ActualSuccess {
					return fmt.Errorf(
						"expected Effective Success to be equal to Actual Success but got: Effective [%.2f] <> Actual [%.2f]",
						stat.EffectiveSuccess, stat.ActualSuccess)
				}
				return nil
			})

			profile := &sp.ServiceProfile{}

			// Grab the output and convert it to a service profile object for modification
			err = yaml.Unmarshal([]byte(out), profile)
			if err != nil {
				t.Errorf("unable to unmarshall YAML: %s", err.Error())
			}

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

			assertRouteStat(assertion, t, func(stat *rowStat) error {
				if stat.EffectiveSuccess <= stat.ActualSuccess {
					return fmt.Errorf(
						"expected Effective Success to be greater than Actual Success but got: Effective [%f] <> Actual [%f]",
						stat.EffectiveSuccess, stat.ActualSuccess)
				}
				return nil
			})
		})
	}
}

func assertRouteStat(assertion *routeStatAssertion, t *testing.T, assertFn func(stat *rowStat) error) {
	const routePath = "GET /testpath"
	err := TestHelper.RetryFor(2*time.Minute, func() error {
		routes, err := getRoutes(assertion.upstream, assertion.namespace, true, []string{"--to", assertion.downstream})
		if err != nil {
			return fmt.Errorf("routes command failed: %s", err)
		}

		var testRoute *rowStat
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

func assertExpectedRoutes(expected []string, actual []*rowStat, t *testing.T) {

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

func getRoutes(deployName, namespace string, isWideOutput bool, additionalArgs []string) ([]*rowStat, error) {
	cmd := []string{"routes", "--namespace", namespace, deployName}

	if len(additionalArgs) > 0 {
		cmd = append(cmd, additionalArgs...)
	}

	if isWideOutput {
		cmd = append(cmd, "-owide")
	}

	cmd = append(cmd, "--output", "json")
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		return nil, err
	}
	var list map[string][]*rowStat
	err = yaml.Unmarshal([]byte(out), &list)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("Error: %s stderr: %s", err.Error(), stderr))
	}
	return list[deployName], nil
}
