package cmd

import (
	"testing"

	"github.com/runconduit/conduit/controller/api/public"
)

func TestStat(t *testing.T) {
	t.Run("Returns namespace stats", func(t *testing.T) {
		mockClient := &public.MockConduitApiClient{}

		response := public.GenStatSummaryResponse("emoji", "namespaces", "emojivoto", 1, 2, 0)

		mockClient.StatSummaryResponseToReturn = &response

		expectedOutput := `NAME    MESHED   SUCCESS      RPS   LATENCY_P50   LATENCY_P95   LATENCY_P99   SECURED
emoji      1/2   100.00%   2.0rps         123ms         123ms         123ms      100%
`

		options := newStatOptions()
		args := []string{"ns"}
		req, err := buildStatSummaryRequest(args, options)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		output, err := requestStatsFromAPI(mockClient, req, options)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if output != expectedOutput {
			t.Fatalf("Wrong output:\n expected: \n%s\n, got: \n%s", expectedOutput, output)
		}
	})
}
