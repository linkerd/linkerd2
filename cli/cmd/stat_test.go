package cmd

import (
	"io/ioutil"
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

func TestStat(t *testing.T) {
	t.Run("Returns namespace stats", func(t *testing.T) {
		mockClient := &public.MockApiClient{}

		counts := &public.PodCounts{
			MeshedPods:  1,
			RunningPods: 2,
			FailedPods:  0,
		}

		response := public.GenStatSummaryResponse("emoji", k8s.Namespace, []string{"emojivoto1"}, counts)

		mockClient.StatSummaryResponseToReturn = &response

		goldenFileBytes, err := ioutil.ReadFile("testdata/stat_one_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedOutput := string(goldenFileBytes)

		options := newStatOptions()
		args := []string{"ns"}
		reqs, err := buildStatSummaryRequests(args, options)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		output, err := requestStatsFromAPI(mockClient, reqs[0], options)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if output != expectedOutput {
			t.Fatalf("Wrong output:\n expected: \n%s\n, got: \n%s", expectedOutput, output)
		}
	})

	t.Run("Returns all namespace stats", func(t *testing.T) {
		mockClient := &public.MockApiClient{}

		counts := &public.PodCounts{
			MeshedPods:  1,
			RunningPods: 2,
			FailedPods:  0,
		}

		response := public.GenStatSummaryResponse("emoji", k8s.Namespace, []string{"emojivoto1", "emojivoto2"}, counts)

		mockClient.StatSummaryResponseToReturn = &response

		goldenFileBytes, err := ioutil.ReadFile("testdata/stat_all_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedOutput := string(goldenFileBytes)

		options := newStatOptions()
		options.allNamespaces = true
		args := []string{"ns"}
		reqs, err := buildStatSummaryRequests(args, options)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		output, err := requestStatsFromAPI(mockClient, reqs[0], options)
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

		_, err := buildStatSummaryRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})

	t.Run("Rejects commands with both --to and --from flags", func(t *testing.T) {
		options := newStatOptions()
		options.toResource = "deploy/foo"
		options.fromResource = "deploy/bar"
		args := []string{"ns", "test"}
		expectedError := "--to and --from flags are mutually exclusive"

		_, err := buildStatSummaryRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})

	t.Run("Rejects commands with both --to-namespace and --from-namespace flags", func(t *testing.T) {
		options := newStatOptions()
		options.toNamespace = "foo"
		options.fromNamespace = "bar"
		args := []string{"po"}
		expectedError := "--to-namespace and --from-namespace flags are mutually exclusive"

		_, err := buildStatSummaryRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})

	t.Run("Rejects --to-namespace flag when the target is a namespace", func(t *testing.T) {
		options := newStatOptions()
		options.toNamespace = "bar"
		args := []string{"ns", "foo"}
		expectedError := "--to-namespace flag is incompatible with namespace resource type"

		_, err := buildStatSummaryRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})

	t.Run("Rejects --from-namespace flag when the target is a namespace", func(t *testing.T) {
		options := newStatOptions()
		options.fromNamespace = "foo"
		args := []string{"ns/bar"}
		expectedError := "--from-namespace flag is incompatible with namespace resource type"

		_, err := buildStatSummaryRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})
}
