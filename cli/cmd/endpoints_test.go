package cmd

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/api/discovery"
	"github.com/linkerd/linkerd2/controller/api/public"
)

type endpointsExp struct {
	options    *endpointsOptions
	identities []string
	file       string
}

func TestEndpoints(t *testing.T) {
	options := newEndpointsOptions()
	t.Run("Returns endpoints", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:    options,
			identities: []string{"emoji-svc.emojivoto.svc.cluster.local:8080", "voting-svc.emojivoto.svc.cluster.local:8080", "authors.books"},
			file:       "endpoints_one_output.golden",
		}, t)
	})

	options.outputFormat = jsonOutput
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

	options.outputFormat = jsonOutput
	t.Run("Returns all namespace endpoints (json)", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:    options,
			identities: []string{"emoji-svc.emojivoto", "voting-svc.emojivoto", "authors.books"},
			file:       "endpoints_all_output_json.golden",
		}, t)
	})
}

func testEndpointsCall(exp endpointsExp, t *testing.T) {
	mockClient := &public.MockAPIClient{
		MockDiscoveryClient: &discovery.MockDiscoveryClient{
			EndpointsResponseToReturn: discovery.GenEndpointsResponse(exp.identities),
		},
	}

	endpoints, err := requestEndpointsFromAPI(mockClient)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := renderEndpoints(endpoints, exp.options)

	diffTestdata(t, exp.file, output)
}
