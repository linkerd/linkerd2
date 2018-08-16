package healthcheck

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
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
