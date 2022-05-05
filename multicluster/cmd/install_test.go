package cmd

import (
	"bytes"
	"fmt"
	"testing"

	multicluster "github.com/linkerd/linkerd2/multicluster/values"
	"github.com/linkerd/linkerd2/pkg/charts"
)

func TestRender(t *testing.T) {
	// pin values that are changed by render functions on each test run
	defaultValues := map[string]interface{}{}

	testCases := []struct {
		values             map[string]interface{}
		multiclusterValues *multicluster.Values
		goldenFileName     string
	}{
		{
			nil,
			nil,
			"install_default.golden",
		},
		{
			map[string]interface{}{
				"enablePSP": "true",
			},
			nil,
			"install_psp.golden",
		},
		{
			map[string]interface{}{
				"enablePSP": "true",
				"gateway": map[string]interface{}{
					"replicas": 3,
				},
				"enablePodAntiAffinity": true,
			},
			nil,
			"install_ha.golden",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			// Merge overrides with default
			if err := render(&buf, tc.multiclusterValues, charts.MergeMaps(defaultValues, tc.values)); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			if err := testDataDiffer.DiffTestYAML(tc.goldenFileName, buf.String()); err != nil {
				t.Error(err)
			}
		})
	}
}
