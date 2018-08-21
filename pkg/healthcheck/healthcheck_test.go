package healthcheck

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	"k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHealthChecker(t *testing.T) {
	nullObserver := func(_, _ string, _ error) {}

	passingCheck1 := &checker{
		category:    "cat1",
		description: "desc1",
		check: func() error {
			return nil
		},
	}

	passingCheck2 := &checker{
		category:    "cat2",
		description: "desc2",
		check: func() error {
			return nil
		},
	}

	failingCheck := &checker{
		category:    "cat3",
		description: "desc3",
		check: func() error {
			return fmt.Errorf("error")
		},
	}

	passingRPCClient := public.MockApiClient{
		SelfCheckResponseToReturn: &healthcheckPb.SelfCheckResponse{
			Results: []*healthcheckPb.CheckResult{
				&healthcheckPb.CheckResult{
					SubsystemName:    "rpc1",
					CheckDescription: "rpc desc1",
					Status:           healthcheckPb.CheckStatus_OK,
				},
			},
		},
	}

	passingRPCCheck := &checker{
		category:    "cat4",
		description: "desc4",
		checkRPC: func() (*healthcheckPb.SelfCheckResponse, error) {
			return passingRPCClient.SelfCheck(context.Background(),
				&healthcheckPb.SelfCheckRequest{})
		},
	}

	failingRPCClient := public.MockApiClient{
		SelfCheckResponseToReturn: &healthcheckPb.SelfCheckResponse{
			Results: []*healthcheckPb.CheckResult{
				&healthcheckPb.CheckResult{
					SubsystemName:         "rpc2",
					CheckDescription:      "rpc desc2",
					Status:                healthcheckPb.CheckStatus_FAIL,
					FriendlyMessageToUser: "rpc error",
				},
			},
		},
	}

	failingRPCCheck := &checker{
		category:    "cat5",
		description: "desc5",
		checkRPC: func() (*healthcheckPb.SelfCheckResponse, error) {
			return failingRPCClient.SelfCheck(context.Background(),
				&healthcheckPb.SelfCheckRequest{})
		},
	}

	fatalCheck := &checker{
		category:    "cat6",
		description: "desc6",
		fatal:       true,
		check: func() error {
			return fmt.Errorf("fatal")
		},
	}

	t.Run("Notifies observer of all results", func(t *testing.T) {
		hc := HealthChecker{
			checkers: []*checker{
				passingCheck1,
				passingCheck2,
				failingCheck,
				passingRPCCheck,
				failingRPCCheck,
			},
		}

		observedResults := make([]string, 0)
		observer := func(category, description string, err error) {
			res := fmt.Sprintf("%s %s", category, description)
			if err != nil {
				res += fmt.Sprintf(": %s", err)
			}
			observedResults = append(observedResults, res)
		}

		expectedResults := []string{
			"cat1 desc1",
			"cat2 desc2",
			"cat3 desc3: error",
			"cat4 desc4",
			"cat4[rpc1] rpc desc1",
			"cat5 desc5",
			"cat5[rpc2] rpc desc2: rpc error",
		}

		hc.RunChecks(observer)

		if !reflect.DeepEqual(observedResults, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, observedResults)
		}
	})

	t.Run("Is successful if all checks were successful", func(t *testing.T) {
		hc := HealthChecker{
			checkers: []*checker{
				passingCheck1,
				passingCheck2,
				passingRPCCheck,
			},
		}

		success := hc.RunChecks(nullObserver)

		if !success {
			t.Fatalf("Expecting checks to be successful, but got [%t]", success)
		}
	})

	t.Run("Is not successful if one check fails", func(t *testing.T) {
		hc := HealthChecker{
			checkers: []*checker{
				passingCheck1,
				failingCheck,
				passingCheck2,
			},
		}

		success := hc.RunChecks(nullObserver)

		if success {
			t.Fatalf("Expecting checks to not be successful, but got [%t]", success)
		}
	})

	t.Run("Is not successful if one RPC check fails", func(t *testing.T) {
		hc := HealthChecker{
			checkers: []*checker{
				passingCheck1,
				failingRPCCheck,
				passingCheck2,
			},
		}

		success := hc.RunChecks(nullObserver)

		if success {
			t.Fatalf("Expecting checks to not be successful, but got [%t]", success)
		}
	})

	t.Run("Does not run remaining check if fatal check fails", func(t *testing.T) {
		hc := HealthChecker{
			checkers: []*checker{
				passingCheck1,
				fatalCheck,
				passingCheck2,
			},
		}

		observedResults := make([]string, 0)
		observer := func(category, description string, err error) {
			res := fmt.Sprintf("%s %s", category, description)
			if err != nil {
				res += fmt.Sprintf(": %s", err)
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
}

func TestValidatePods(t *testing.T) {
	pod := func(name string, phase v1.PodPhase, ready bool) v1.Pod {
		return v1.Pod{
			ObjectMeta: meta.ObjectMeta{Name: name},
			Status: v1.PodStatus{
				Phase: phase,
				ContainerStatuses: []v1.ContainerStatus{
					v1.ContainerStatus{
						Name:  strings.Split(name, "-")[0],
						Ready: ready,
					},
				},
			},
		}
	}

	t.Run("Returns an error if not all pods are running", func(t *testing.T) {
		pods := []v1.Pod{
			pod("controller-6f78cbd47-bc557", v1.PodRunning, true),
			pod("grafana-5b7d796646-hh46d", v1.PodRunning, true),
			pod("prometheus-74d6879cd6-bbdk6", v1.PodFailed, false),
			pod("web-98c9ddbcd-7b5lh", v1.PodRunning, true),
		}

		err := validatePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "No running pods for prometheus" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if not all containers are ready", func(t *testing.T) {
		pods := []v1.Pod{
			pod("controller-6f78cbd47-bc557", v1.PodRunning, true),
			pod("grafana-5b7d796646-hh46d", v1.PodRunning, false),
			pod("prometheus-74d6879cd6-bbdk6", v1.PodRunning, true),
			pod("web-98c9ddbcd-7b5lh", v1.PodRunning, true),
		}

		err := validatePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "The grafana pod's grafana container is not ready" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns nil if all pods are running and all containers are ready", func(t *testing.T) {
		pods := []v1.Pod{
			pod("controller-6f78cbd47-bc557", v1.PodRunning, true),
			pod("grafana-5b7d796646-hh46d", v1.PodRunning, true),
			pod("prometheus-74d6879cd6-bbdk6", v1.PodRunning, true),
			pod("web-98c9ddbcd-7b5lh", v1.PodRunning, true),
		}

		err := validatePods(pods)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})
}
