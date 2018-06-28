package cmd

import (
	"testing"

	"github.com/runconduit/conduit/controller/api/public"
)

func TestStat(t *testing.T) {
	t.Run("Returns namespace stats", func(t *testing.T) {
		mockClient := &public.MockConduitApiClient{}

		counts := &public.PodCounts{
			MeshedPods:  1,
			RunningPods: 2,
			FailedPods:  0,
		}

		response := public.GenStatSummaryResponse("emoji", "namespaces", "emojivoto", counts)

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

	t.Run("Returns an error for named resource queries with the --all-namespaces flag", func(t *testing.T) {
		options := newStatOptions()
		options.allNamespaces = true
		args := []string{"po", "web"}
		expectedError := "stats for a resource cannot be retrieved by name across all namespaces"

		_, err := buildStatSummaryRequest(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})
}
