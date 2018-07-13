package cmd

import (
	"errors"
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
)

func TestDashboardAvailability(t *testing.T) {
	t.Run("Returns true if api client has responds with a list of Self Checks that are OK", func(t *testing.T) {

		mockSelfCheckResponse := &healthcheckPb.SelfCheckResponse{
			Results: []*healthcheckPb.CheckResult{
				{
					SubsystemName: "TestSystem",
					Status:        healthcheckPb.CheckStatus_OK,
				},
			},
		}

		mockPublicApi := &public.MockApiClient{
			SelfCheckResponseToReturn: mockSelfCheckResponse,
		}

		dashboardAvailable, err := isDashboardAvailable(mockPublicApi)
		if err != nil {
			t.Fatalf("Expected to not receive an error but got: %+v\n", err)
		}

		if !dashboardAvailable {
			t.Fatalf("Expected dashboard available to be true but got: %t", dashboardAvailable)
		}
	})

	t.Run("Returns false if public api client returns a list of Self Checks that have failed", func(t *testing.T) {
		mockSelfCheckResponse := &healthcheckPb.SelfCheckResponse{
			Results: []*healthcheckPb.CheckResult{
				{
					SubsystemName: "TestSystem",
					Status:        healthcheckPb.CheckStatus_FAIL,
				},
			},
		}

		mockPublicApi := &public.MockApiClient{
			SelfCheckResponseToReturn: mockSelfCheckResponse,
		}

		dashboardAvailable, err := isDashboardAvailable(mockPublicApi)
		if err != nil {
			t.Fatalf("Expected to not receive an error but got: %+v\n", err)
		}

		if dashboardAvailable {
			t.Fatalf("Expected dashboard available to be false but got: %t", dashboardAvailable)
		}
	})

	t.Run("Return false when public API Self Check fails to make a request", func(t *testing.T) {
		mockPublicApi := &public.MockApiClient{
			ErrorToReturn: errors.New("expected"),
		}
		dashboardAvailable, err := isDashboardAvailable(mockPublicApi)
		if err == nil {
			t.Fatalf("Expected error to not be nil")
		}
		if dashboardAvailable {
			t.Fatalf("Expected dashboard available to return false but gotL %t", dashboardAvailable)
		}
	})
}
