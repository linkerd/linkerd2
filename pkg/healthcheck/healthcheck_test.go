package healthcheck

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type observer struct {
	results []string
}

func newObserver() *observer {
	return &observer{
		results: []string{},
	}
}
func (o *observer) resultFn(result *CheckResult) {
	res := fmt.Sprintf("%s %s", result.Category, result.Description)
	if result.Err != nil {
		res += fmt.Sprintf(": %s", result.Err)
	}
	o.results = append(o.results, res)
}

func (hc *HealthChecker) addCheckAsCategory(
	testCategoryID CategoryID,
	categoryID CategoryID,
	desc string,
) {
	testCategory := category{
		id:       testCategoryID,
		checkers: []checker{},
	}

	for _, cat := range hc.categories {
		if cat.id == categoryID {
			for _, ch := range cat.checkers {
				if ch.description == desc {
					testCategory.checkers = append(testCategory.checkers, ch)
					break
				}
			}
			break
		}
	}
	hc.addCategory(testCategory)
}

func TestHealthChecker(t *testing.T) {
	nullObserver := func(*CheckResult) {}

	passingCheck1 := category{
		id: "cat1",
		checkers: []checker{
			{
				description: "desc1",
				check: func(context.Context) error {
					return nil
				},
				retryDeadline: time.Time{},
			},
		},
	}

	passingCheck2 := category{
		id: "cat2",
		checkers: []checker{
			{
				description: "desc2",
				check: func(context.Context) error {
					return nil
				},
				retryDeadline: time.Time{},
			},
		},
	}

	failingCheck := category{
		id: "cat3",
		checkers: []checker{
			{
				description: "desc3",
				check: func(context.Context) error {
					return fmt.Errorf("error")
				},
				retryDeadline: time.Time{},
			},
		},
	}

	passingRPCClient := public.MockAPIClient{
		SelfCheckResponseToReturn: &healthcheckPb.SelfCheckResponse{
			Results: []*healthcheckPb.CheckResult{
				{
					SubsystemName:    "rpc1",
					CheckDescription: "rpc desc1",
					Status:           healthcheckPb.CheckStatus_OK,
				},
			},
		},
	}

	passingRPCCheck := category{
		id: "cat4",
		checkers: []checker{
			{
				description: "desc4",
				checkRPC: func(context.Context) (*healthcheckPb.SelfCheckResponse, error) {
					return passingRPCClient.SelfCheck(context.Background(),
						&healthcheckPb.SelfCheckRequest{})
				},
				retryDeadline: time.Time{},
			},
		},
	}

	failingRPCClient := public.MockAPIClient{
		SelfCheckResponseToReturn: &healthcheckPb.SelfCheckResponse{
			Results: []*healthcheckPb.CheckResult{
				{
					SubsystemName:         "rpc2",
					CheckDescription:      "rpc desc2",
					Status:                healthcheckPb.CheckStatus_FAIL,
					FriendlyMessageToUser: "rpc error",
				},
			},
		},
	}

	failingRPCCheck := category{
		id: "cat5",
		checkers: []checker{
			{
				description: "desc5",
				checkRPC: func(context.Context) (*healthcheckPb.SelfCheckResponse, error) {
					return failingRPCClient.SelfCheck(context.Background(),
						&healthcheckPb.SelfCheckRequest{})
				},
				retryDeadline: time.Time{},
			},
		},
	}

	fatalCheck := category{
		id: "cat6",
		checkers: []checker{
			{
				description: "desc6",
				fatal:       true,
				check: func(context.Context) error {
					return fmt.Errorf("fatal")
				},
				retryDeadline: time.Time{},
			},
		},
	}

	t.Run("Notifies observer of all results", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(passingCheck2)
		hc.addCategory(failingCheck)
		hc.addCategory(passingRPCCheck)
		hc.addCategory(failingRPCCheck)

		expectedResults := []string{
			"cat1 desc1",
			"cat2 desc2",
			"cat3 desc3: error",
			"cat4 desc4",
			"cat4 [rpc1] rpc desc1",
			"cat5 desc5",
			"cat5 [rpc2] rpc desc2: rpc error",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)

		if !reflect.DeepEqual(obs.results, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
		}
	})

	t.Run("Is successful if all checks were successful", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(passingCheck2)
		hc.addCategory(passingRPCCheck)

		success := hc.RunChecks(nullObserver)

		if !success {
			t.Fatalf("Expecting checks to be successful, but got [%t]", success)
		}
	})

	t.Run("Is not successful if one check fails", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(failingCheck)
		hc.addCategory(passingCheck2)

		success := hc.RunChecks(nullObserver)

		if success {
			t.Fatalf("Expecting checks to not be successful, but got [%t]", success)
		}
	})

	t.Run("Is not successful if one RPC check fails", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(failingRPCCheck)
		hc.addCategory(passingCheck2)

		success := hc.RunChecks(nullObserver)

		if success {
			t.Fatalf("Expecting checks to not be successful, but got [%t]", success)
		}
	})

	t.Run("Does not run remaining check if fatal check fails", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(fatalCheck)
		hc.addCategory(passingCheck2)

		expectedResults := []string{
			"cat1 desc1",
			"cat6 desc6: fatal",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)

		if !reflect.DeepEqual(obs.results, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
		}
	})

	t.Run("Retries checks if retry is specified", func(t *testing.T) {
		retryWindow = 0
		returnError := true

		retryCheck := category{
			id: "cat7",
			checkers: []checker{
				{
					description:   "desc7",
					retryDeadline: time.Now().Add(100 * time.Second),
					check: func(context.Context) error {
						if returnError {
							returnError = false
							return fmt.Errorf("retry")
						}
						return nil
					},
				},
			},
		}

		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(retryCheck)

		observedResults := make([]string, 0)
		observer := func(result *CheckResult) {
			res := fmt.Sprintf("%s %s retry=%t", result.Category, result.Description, result.Retry)
			if result.Err != nil {
				res += fmt.Sprintf(": %s", result.Err)
			}
			observedResults = append(observedResults, res)
		}

		expectedResults := []string{
			"cat1 desc1 retry=false",
			"cat7 desc7 retry=true: waiting for check to complete",
			"cat7 desc7 retry=false",
		}

		hc.RunChecks(observer)

		if !reflect.DeepEqual(observedResults, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, observedResults)
		}
	})
}

func TestCheckCanCreate(t *testing.T) {
	exp := fmt.Errorf("not authorized to access deployments.extensions")

	hc := NewHealthChecker(
		[]CategoryID{},
		&Options{},
	)
	var err error
	hc.kubeAPI, err = k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	err = hc.checkCanCreate("", "extensions", "v1beta1", "deployments")
	if err == nil ||
		err.Error() != exp.Error() {
		t.Fatalf("Unexpected error (Expected: %s, Got: %s)", exp, err)
	}
}

func TestCheckNetAdmin(t *testing.T) {
	tests := []struct {
		k8sConfigs []string
		err        error
	}{
		{
			[]string{},
			nil,
		},
		{
			[]string{`apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: restricted
spec:
  requiredDropCapabilities:
    - ALL`,
			},
			fmt.Errorf("found 1 PodSecurityPolicies, but none provide NET_ADMIN"),
		},
	}

	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: returns expected NET_ADMIN result", i), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{},
				&Options{},
			)

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			err = hc.checkNetAdmin()
			if err != nil || test.err != nil {
				if (err == nil && test.err != nil) ||
					(err != nil && test.err == nil) ||
					(err.Error() != test.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", test.err, err)
				}
			}
		})
	}
}

func TestConfigExists(t *testing.T) {
	testCases := []struct {
		k8sConfigs []string
		results    []string
	}{
		{
			[]string{},
			[]string{"linkerd-config control plane Namespace exists: The \"test-ns\" namespace does not exist"},
		},
		{
			[]string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
			},
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist: missing ClusterRoles: linkerd-test-ns-controller, linkerd-test-ns-identity, linkerd-test-ns-prometheus, linkerd-test-ns-proxy-injector, linkerd-test-ns-sp-validator",
			},
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: returns expected config result", i), func(t *testing.T) {

			hc := NewHealthChecker(
				[]CategoryID{LinkerdConfigChecks},
				&Options{
					ControlPlaneNamespace: "test-ns",
				},
			)

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(tc.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if !reflect.DeepEqual(obs.results, tc.results) {
				t.Fatalf("Expected results %v, but got %v", tc.results, obs.results)
			}
		})
	}
}

func TestCRDExists(t *testing.T) {
	k8sConfig := `
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: serviceprofiles.linkerd.io
  annotations:
    linkerd.io/created-by: linkerd/cli dev-65149d37-siggy
spec:
  group: linkerd.io
  version: v1alpha1
  scope: Namespaced
  names:
    plural: serviceprofiles
    singular: serviceprofile
    kind: ServiceProfile
    shortNames:
    - sp
`

	hc := NewHealthChecker(
		[]CategoryID{},
		&Options{},
	)
	var err error
	hc.kubeAPI, err = k8s.NewFakeAPI(k8sConfig)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	hc.addCheckAsCategory("cat1", LinkerdConfigChecks, "control plane CustomResourceDefinitions exist")

	expectedResults := []string{
		"cat1 control plane CustomResourceDefinitions exist",
	}

	obs := newObserver()
	hc.RunChecks(obs.resultFn)
	if !reflect.DeepEqual(obs.results, expectedResults) {
		t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
	}
}

func TestCheckControlPlanePodExistence(t *testing.T) {
	hc := NewHealthChecker(
		[]CategoryID{},
		&Options{
			ControlPlaneNamespace: "test-ns",
		},
	)
	k8sConfigs := []string{`
apiVersion: v1
kind: Pod
metadata:
  name: linkerd-controller-6f78cbd47-bc557
  namespace: test-ns
status:
  phase: Running
  podIP: 1.2.3.4
`,
	}
	var err error
	hc.kubeAPI, err = k8s.NewFakeAPI(k8sConfigs...)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	// validate that this check relies on the k8s api, not on hc.controlPlanePods
	hc.addCheckAsCategory("cat1", LinkerdControlPlaneExistenceChecks, "controller pod is running")

	expectedResults := []string{
		"cat1 controller pod is running",
	}

	obs := newObserver()
	hc.RunChecks(obs.resultFn)
	if !reflect.DeepEqual(obs.results, expectedResults) {
		t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
	}
}

func TestValidateControlPlanePods(t *testing.T) {
	pod := func(name string, phase corev1.PodPhase, ready bool) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{
				Phase: phase,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  strings.Split(name, "-")[1],
						Ready: ready,
					},
				},
			},
		}
	}

	t.Run("Returns an error if not all pods are running", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodFailed, false),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "No running pods for \"linkerd-prometheus\"" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if not all containers are ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, false),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodRunning, true),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "The \"linkerd-grafana\" pod's \"grafana\" container is not ready" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns nil if all pods are running and all containers are ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodRunning, true),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Returns nil if all linkerd pods are running and pod list includes non-linkerd pod", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodRunning, true),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
			pod("hello-43c25d", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err != nil {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})
}

func TestValidateDataPlaneNamespace(t *testing.T) {
	testCases := []struct {
		ns     string
		result string
	}{
		{
			"",
			"data-plane-ns-test-cat data plane namespace exists",
		},
		{
			"bad-ns",
			"data-plane-ns-test-cat data plane namespace exists: The \"bad-ns\" namespace does not exist",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d/%s", i, tc.ns), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{},
				&Options{
					DataPlaneNamespace: tc.ns,
				},
			)
			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI()
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			// create a synethic category that only includes the "data plane namespace exists" check
			hc.addCheckAsCategory("data-plane-ns-test-cat", LinkerdDataPlaneChecks, "data plane namespace exists")

			expectedResults := []string{
				tc.result,
			}
			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if !reflect.DeepEqual(obs.results, expectedResults) {
				t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
			}
		})
	}
}

func TestValidateDataPlanePods(t *testing.T) {

	t.Run("Returns an error if no inject pods were found", func(t *testing.T) {
		err := validateDataPlanePods([]*pb.Pod{}, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "No \"linkerd-proxy\" containers found in the \"emojivoto\" namespace" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if not all pods are running", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "emoji-d9c7866bb-7v74n", Status: "Running", ProxyReady: true},
			{Name: "vote-bot-644b8cb6b4-g8nlr", Status: "Running", ProxyReady: true},
			{Name: "voting-65b9fffd77-rlwsd", Status: "Failed", ProxyReady: false},
			{Name: "web-6cfbccc48-5g8px", Status: "Running", ProxyReady: true},
		}

		err := validateDataPlanePods(pods, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "The \"voting-65b9fffd77-rlwsd\" pod is not running" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if the proxy container is not ready", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "emoji-d9c7866bb-7v74n", Status: "Running", ProxyReady: true},
			{Name: "vote-bot-644b8cb6b4-g8nlr", Status: "Running", ProxyReady: false},
			{Name: "voting-65b9fffd77-rlwsd", Status: "Running", ProxyReady: true},
			{Name: "web-6cfbccc48-5g8px", Status: "Running", ProxyReady: true},
		}

		err := validateDataPlanePods(pods, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "The \"linkerd-proxy\" container in the \"vote-bot-644b8cb6b4-g8nlr\" pod is not ready" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns nil if all pods are running and all proxy containers are ready", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "emoji-d9c7866bb-7v74n", Status: "Running", ProxyReady: true},
			{Name: "vote-bot-644b8cb6b4-g8nlr", Status: "Running", ProxyReady: true},
			{Name: "voting-65b9fffd77-rlwsd", Status: "Running", ProxyReady: true},
			{Name: "web-6cfbccc48-5g8px", Status: "Running", ProxyReady: true},
		}

		err := validateDataPlanePods(pods, "emojivoto")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})
}

func TestValidateDataPlanePodReporting(t *testing.T) {
	t.Run("Returns success if no pods present", func(t *testing.T) {
		err := validateDataPlanePodReporting([]*pb.Pod{})
		if err != nil {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns success if all pods are added", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "ns1/test1", Added: true},
			{Name: "ns2/test2", Added: true},
		}

		err := validateDataPlanePodReporting(pods)
		if err != nil {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if any of the pod was not added to Prometheus", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "ns1/test1", Added: true},
			{Name: "ns2/test2", Added: false},
		}

		err := validateDataPlanePodReporting(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "Data plane metrics not found for ns2/test2." {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})
}
