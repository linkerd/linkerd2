package cmd

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/pkg/charts"
)

func TestRender(t *testing.T) {

	// pin values that are changed by render functions on each test run
	defaultValues := map[string]interface{}{
		"webhook": map[string]interface{}{
			"externalSecret": true,
			"caBundle":       "test-webhook-ca-bundle",
		},
	}

	testCases := []struct {
		values         map[string]interface{}
		goldenFileName string
	}{
		{
			nil,
			"install_default.golden",
		},
		{
			map[string]interface{}{
				"jaeger": map[string]interface{}{"enabled": false},
			},
			"install_jaeger_disabled.golden",
		},
		{
			map[string]interface{}{
				"collector": map[string]interface{}{"enabled": false},
			},
			"install_collector_disabled.golden",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			// Merge overrides with default
			if err := render(&buf, charts.MergeMaps(defaultValues, tc.values), ""); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			if err := testDataDiffer.DiffTestYAML(tc.goldenFileName, buf.String()); err != nil {
				t.Error(err)
			}
		})
	}
}
