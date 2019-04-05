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

		observedResults := make([]string, 0)
		observer := func(result *CheckResult) {
			res := fmt.Sprintf("%s %s", result.Category, result.Description)
			if result.Err != nil {
				res += fmt.Sprintf(": %s", result.Err)
			}
			observedResults = append(observedResults, res)
		}

		expectedResults := []string{
			"cat1 desc1",
			"cat2 desc2",
			"cat3 desc3: error",
			"cat4 desc4",
			"cat4 [rpc1] rpc desc1",
			"cat5 desc5",
			"cat5 [rpc2] rpc desc2: rpc error",
		}

		hc.RunChecks(observer)

		if !reflect.DeepEqual(observedResults, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, observedResults)
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

		observedResults := make([]string, 0)
		observer := func(result *CheckResult) {
			res := fmt.Sprintf("%s %s", result.Category, result.Description)
			if result.Err != nil {
				res += fmt.Sprintf(": %s", result.Err)
			}
			observedResults = append(observedResults, res)
		}

		expectedResults := []string{
			"cat1 desc1",
			"cat6 desc6: fatal",
		}

		hc.RunChecks(observer)

		if !reflect.DeepEqual(observedResults, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, observedResults)
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
	hc.clientset, _, err = k8s.NewFakeClientSets()
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
			hc.clientset, _, err = k8s.NewFakeClientSets(test.k8sConfigs...)
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
