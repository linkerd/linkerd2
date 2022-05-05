package cmd

import (
	"bytes"
	"fmt"
	"testing"

	charts "github.com/linkerd/linkerd2/pkg/charts"
)

func TestRender(t *testing.T) {

	// pin values that are changed by render functions on each test run
	defaultValues := map[string]interface{}{
		"tap": map[string]interface{}{
			"externalSecret": true,
			"caBundle":       "test-tap-ca-bundle",
		},
		"tapInjector": map[string]interface{}{
			"externalSecret": true,
			"caBundle":       "test-tap-ca-bundle",
		},
	}

	proxyResources := map[string]interface{}{
		"proxy": map[string]interface{}{
			"resources": map[string]interface{}{
				"cpu": map[string]interface{}{
					"request": "500m",
					"limit":   "100m",
				},
				"memory": map[string]interface{}{
					"request": "20Mi",
					"limit":   "250Mi",
				},
			},
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
				"prometheus": map[string]interface{}{
					"args": map[string]interface{}{
						"log.level": "debug",
					}},
			},
			"install_prometheus_loglevel_from_args.golden",
		},
		{
			map[string]interface{}{
				"prometheus":    map[string]interface{}{"enabled": false},
				"prometheusUrl": "external-prom.com",
			},
			"install_prometheus_disabled.golden",
		},
		{
			map[string]interface{}{
				"prometheus": proxyResources,
				"tap":        proxyResources,
				"dashboard":  proxyResources,
			},
			"install_proxy_resources.golden",
		},
		{
			map[string]interface{}{
				"defaultLogLevel": "debug",
				"defaultUID":      1234,
				"defaultRegistry": "gcr.io/linkerd",
				"tap": map[string]interface{}{
					"logLevel": "info",
					"UID":      5678,
					"image": map[string]interface{}{
						"registry": "cr.l5d.io/linkerd",
						"tag":      "stable-9.2",
					},
				},
				"grafana": map[string]interface{}{"url": "grafana.grafana:3000"},
			},
			"install_default_overrides.golden",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			// Merge overrides with default
			if err := render(&buf, charts.MergeMaps(defaultValues, tc.values)); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			if err := testDataDiffer.DiffTestYAML(tc.goldenFileName, buf.String()); err != nil {
				t.Error(err)
			}
		})
	}
}
