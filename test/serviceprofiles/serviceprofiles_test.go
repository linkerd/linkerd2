package serviceprofiles

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/testutil"
	"sigs.k8s.io/yaml"
)

var TestHelper *testutil.TestHelper

type rowStat struct {
	Route            string  `json:"route"`
	Authority        string  `json:"authority"`
	Success          float64 `json:"success"`
	EffectiveSuccess float64 `json:"effective_success"`
	ActualSuccess    float64 `json:"actual_success"`
	RPS              float64 `json:"rps"`
	LatencyP50       int     `json:"latency_ms_p50"`
	LatencyP95       int     `json:"latency_ms_p95"`
	LatencyP99       int     `json:"latency_ms_p99"`
}

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

	out, stderr, err := TestHelper.LinkerdRun("inject", "testdata/hello_world.yaml")
	if err != nil {
		t.Fatalf("'linkerd %s' command failed with %s: %s\n", "inject", err.Error(), stderr)
	}

	out, err = TestHelper.KubectlApply(out, testNamespace)
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}

	testCase := testCase{
		sourceName: "tap",
		namespace:  testNamespace,
		spName:     "world-svc",
		deployName: "deployment/world",
	}
	sourceFlag := fmt.Sprintf("--%s", testCase.sourceName)
	cmd := []string{
		"profile",
		"--namespace",
		testCase.namespace,
		testCase.spName,
		sourceFlag,
		testCase.deployName,
	}

	out, stderr, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		t.Fatalf("'linkerd %s' command failed with %s: %s\n", cmd, err.Error(), stderr)
	}

	_, err = TestHelper.KubectlApply(out, testCase.namespace)
	if err != nil {
		t.Fatalf("kubectl apply command failed:\n%s", err)
	}

	routes, err := getRoutes("deployment/hello", testNamespace, true, []string{"--to", "deployment/world"})
	if err != nil {
		t.Fatalf("routes command failed: %s\n", err)
	}

	err = TestHelper.RetryFor(10*time.Second, func() error {
		// test that all success rates are lower than 100%
		for _, route := range routes {
			if route.EffectiveSuccess == 1 {
				return fmt.Errorf("expected route %s effective success rate [%f %%] to be less than 100 %%",
					route.Route, route.EffectiveSuccess*100)
			}
		}
		return nil
	})

	if err != nil {
		t.Fatal(err.Error())
	}

	profile := &sp.ServiceProfile{}

	// Grab the output and convert it to sp
	err = yaml.Unmarshal([]byte(out), profile)
	if err != nil {
		t.Fatalf("unable to unmarshall YAML: %s", err.Error())
	}

	for _, route := range profile.Spec.Routes {
		if route.Name == "GET /testpath" {
			route.IsRetryable = true
			break
		}

	}

	bytes, err := yaml.Marshal(profile)
	if err != nil {
		t.Fatalf("error marshalling service profile: %s", bytes)
	}

	_, err = TestHelper.KubectlApply(string(bytes), testCase.namespace)
	if err != nil {
		t.Fatalf("kubectl apply command failed:\n%s", err)
	}

	// Verify retryable
	routes, err = getRoutes("deployment/hello", testCase.namespace, true, []string{"--to", testCase.deployName})
	if err != nil {
		t.Fatalf("routes command failed: %s\n", err)
	}

	err = TestHelper.RetryFor(5*time.Second, func() error {
		for _, route := range routes {
			if route.EffectiveSuccess > 1 {
				return fmt.Errorf("expected route %s effective success rate [%f %%] to be greater than actual success rate [%f %%]",
					route.Route, route.EffectiveSuccess*100, route.ActualSuccess*100)
			}
		}
		return nil
	})

	if err != nil {
		t.Fatal(err.Error())
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
			t.Errorf("Expected route %s not found in %v", expectedRoute, actual)
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
		return nil, errors.New(fmt.Sprintf("Error: %s stderr: %s", err.Error(), stderr))
	}
	return list[deployName], nil
}
