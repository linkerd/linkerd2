package cmd

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
)

type endpointsExp struct {
	options    *endpointsOptions
	identities []string
	file       string
}

func TestEndpoints(t *testing.T) {
	options := newEndpointsOptions()
	options.namespace = "emojivoto"
	t.Run("Returns endpoints", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:    options,
			identities: []string{"emoji-svc.emojivoto", "voting-svc.emojivoto", "authors.books"},
			file:       "endpoints_one_output.golden",
		}, t)
	})

	options.outputFormat = "json"
	t.Run("Returns endpoints (json)", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:    options,
			identities: []string{"emoji-svc.emojivoto", "voting-svc.emojivoto", "authors.books"},
			file:       "endpoints_one_output_json.golden",
		}, t)
	})

	options = newEndpointsOptions()
	t.Run("Returns all namespace endpoints", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:    options,
			identities: []string{"emoji-svc.emojivoto", "voting-svc.emojivoto", "authors.books"},
			file:       "endpoints_all_output.golden",
		}, t)
	})

	options.outputFormat = "json"
	t.Run("Returns all namespace endpoints (json)", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:    options,
			identities: []string{"emoji-svc.emojivoto", "voting-svc.emojivoto", "authors.books"},
			file:       "endpoints_all_output_json.golden",
		}, t)
	})
}

func testEndpointsCall(exp endpointsExp, t *testing.T) {
	mockClient := &public.MockAPIClient{}

	response := public.GenEndpointsResponse(exp.identities)

	mockClient.EndpointsResponseToReturn = &response

	endpoints, err := requestEndpointsFromAPI(mockClient)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := renderEndpoints(endpoints, exp.options)

	testDiff(t, exp.file, output)
}
