package cmd

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
)

type routesParamsExp struct {
	options *routesOptions
	routes  []string
	counts  []uint64
	file    string
}

func TestRoutes(t *testing.T) {
	options := newRoutesOptions()
	t.Run("Returns route stats", func(t *testing.T) {
		testRoutesCall(routesParamsExp{
			routes:  []string{"/a", "/b", "/c", ""},
			counts:  []uint64{90, 60, 0, 30},
			options: options,
			file:    "routes_one_output.golden",
		}, t)
	})

	options.outputFormat = "json"
	t.Run("Returns route stats (json)", func(t *testing.T) {
		testRoutesCall(routesParamsExp{
			routes:  []string{"/a", "/b", "/c", ""},
			counts:  []uint64{90, 60, 0, 30},
			options: options,
			file:    "routes_one_output_json.golden",
		}, t)
	})
}

func testRoutesCall(exp routesParamsExp, t *testing.T) {
	mockClient := &public.MockAPIClient{}

	response := public.GenTopRoutesResponse(exp.routes, exp.counts)

	mockClient.TopRoutesResponseToReturn = &response

	req, err := buildTopRoutesRequest("deploy/foobar", exp.options)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output, err := requestRouteStatsFromAPI(mockClient, req, exp.options)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	diffCompareFile(t, output, exp.file)
}
