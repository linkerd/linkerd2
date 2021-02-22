package cmd

import (
	"testing"

	api "github.com/linkerd/linkerd2/viz/metrics-api"
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
			routes:  []string{"/a", "/b", "/c"},
			counts:  []uint64{90, 60, 0, 30},
			options: options,
			file:    "routes_one_output.golden",
		}, t)
	})

	options.outputFormat = jsonOutput
	t.Run("Returns route stats (json)", func(t *testing.T) {
		testRoutesCall(routesParamsExp{
			routes:  []string{"/a", "/b", "/c"},
			counts:  []uint64{90, 60, 0, 30},
			options: options,
			file:    "routes_one_output_json.golden",
		}, t)
	})

	wideOptions := newRoutesOptions()
	wideOptions.toResource = "deploy/bar"
	wideOptions.outputFormat = wideOutput
	t.Run("Returns wider route stats", func(t *testing.T) {
		testRoutesCall(routesParamsExp{
			routes:  []string{"/a", "/b", "/c"},
			counts:  []uint64{90, 60, 0, 30},
			options: wideOptions,
			file:    "routes_one_output_wide.golden",
		}, t)
	})
}

func testRoutesCall(exp routesParamsExp, t *testing.T) {
	mockClient := &api.MockAPIClient{}

	response := api.GenTopRoutesResponse(exp.routes, exp.counts, exp.options.toResource != "", "foobar")

	mockClient.TopRoutesResponseToReturn = response

	req, err := buildTopRoutesRequest("deploy/foobar", exp.options)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output, err := requestRouteStatsFromAPI(mockClient, req, exp.options)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	testDataDiffer.DiffTestdata(t, exp.file, output)
}
